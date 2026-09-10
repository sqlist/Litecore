package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	smfpb "github.com/5g-core/proto/smf"
	upfpb "github.com/5g-core/proto/upf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeUPF struct {
	mu      sync.Mutex
	created int
	deleted int
}

func (f *fakeUPF) CreateRule(_ context.Context, in *upfpb.CreateRuleRequest, _ ...grpc.CallOption) (*upfpb.CreateRuleResponse, error) {
	f.mu.Lock()
	f.created++
	f.mu.Unlock()
	return &upfpb.CreateRuleResponse{Success: true, RuleId: "rule-" + in.SessionId}, nil
}

func (f *fakeUPF) DeleteRule(_ context.Context, _ *upfpb.DeleteRuleRequest, _ ...grpc.CallOption) (*upfpb.DeleteRuleResponse, error) {
	f.mu.Lock()
	f.deleted++
	f.mu.Unlock()
	return &upfpb.DeleteRuleResponse{Success: true}, nil
}

func (f *fakeUPF) GetStats(context.Context, *upfpb.GetStatsRequest, ...grpc.CallOption) (*upfpb.GetStatsResponse, error) {
	return &upfpb.GetStatsResponse{}, nil
}

func (f *fakeUPF) counts() (created, deleted int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, f.deleted
}

// controlledUPF lets a test stop an RPC at a deterministic point. A nil release
// channel means that operation returns immediately.
type controlledUPF struct {
	mu sync.Mutex

	createStarted chan struct{}
	createRelease <-chan struct{}
	createErr     error
	createSuccess bool

	deleteStarted chan struct{}
	deleteRelease <-chan struct{}
	deleteErr     error
	deleteSuccess bool

	created int
	deleted int
}

func (f *controlledUPF) CreateRule(ctx context.Context, in *upfpb.CreateRuleRequest, _ ...grpc.CallOption) (*upfpb.CreateRuleResponse, error) {
	f.mu.Lock()
	f.created++
	started, release := f.createStarted, f.createRelease
	err, success := f.createErr, f.createSuccess
	f.mu.Unlock()
	signalChannel(started)
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &upfpb.CreateRuleResponse{Success: success, RuleId: "rule-" + in.SessionId, Message: "create rejected"}, nil
}

func (f *controlledUPF) DeleteRule(ctx context.Context, _ *upfpb.DeleteRuleRequest, _ ...grpc.CallOption) (*upfpb.DeleteRuleResponse, error) {
	f.mu.Lock()
	f.deleted++
	started, release := f.deleteStarted, f.deleteRelease
	err, success := f.deleteErr, f.deleteSuccess
	f.mu.Unlock()
	signalChannel(started)
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return &upfpb.DeleteRuleResponse{Success: success, Message: "delete rejected"}, nil
}

func (f *controlledUPF) GetStats(context.Context, *upfpb.GetStatsRequest, ...grpc.CallOption) (*upfpb.GetStatsResponse, error) {
	return &upfpb.GetStatsResponse{}, nil
}

func (f *controlledUPF) counts() (created, deleted int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created, f.deleted
}

func signalChannel(ch chan struct{}) {
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked UPF call")
	}
}

func TestSessionLifecycleReusesIP(t *testing.T) {
	fake := &fakeUPF{}
	h := NewSMFHandlerWithClient(fake, time.Second, 1)
	req := createRequest("u1")
	first, err := h.CreateSession(context.Background(), req)
	if err != nil || !first.Success {
		t.Fatalf("create: %v %v", first, err)
	}
	again, err := h.CreateSession(context.Background(), req)
	created, _ := fake.counts()
	if err != nil || again.SessionId != first.SessionId || created != 1 {
		t.Fatalf("idempotency failed: %v %v calls=%d", again, err, created)
	}
	exhausted, err := h.CreateSession(context.Background(), createRequest("u2"))
	if err != nil || exhausted.Success {
		t.Fatalf("expected pool exhaustion: %v %v", exhausted, err)
	}
	deleted, err := h.DeleteSession(context.Background(), deleteRequest(first.SessionId, "u1"))
	if err != nil || !deleted.Success {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	second, err := h.CreateSession(context.Background(), createRequest("u2"))
	if err != nil || !second.Success || second.UeIp != first.UeIp {
		t.Fatalf("IP not reused: %v %v", second, err)
	}
}

func TestCreateRejectsDifferentAMFOwner(t *testing.T) {
	fake := &fakeUPF{}
	h := NewSMFHandlerWithClient(fake, time.Second, 1)
	if _, err := h.CreateSession(context.Background(), createRequest("u1")); err != nil {
		t.Fatalf("initial create: %v", err)
	}

	conflict := createRequest("u1")
	conflict.AmfUeId = "amf-other"
	if _, err := h.CreateSession(context.Background(), conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
	created, _ := fake.counts()
	if created != 1 {
		t.Fatalf("owner conflict reached UPF: create calls=%d", created)
	}
}

func TestDeleteValidatesUEOwner(t *testing.T) {
	fake := &fakeUPF{}
	h := NewSMFHandlerWithClient(fake, time.Second, 1)
	created, err := h.CreateSession(context.Background(), createRequest("u1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := h.DeleteSession(context.Background(), &smfpb.DeleteSessionRequest{SessionId: created.SessionId}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty ue_id: expected InvalidArgument, got %v", err)
	}
	if _, err := h.DeleteSession(context.Background(), deleteRequest(created.SessionId, "u2")); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong ue_id: expected PermissionDenied, got %v", err)
	}
	_, deleted := fake.counts()
	if deleted != 0 {
		t.Fatalf("invalid owner reached UPF: delete calls=%d", deleted)
	}
	got, err := h.GetSession(context.Background(), &smfpb.GetSessionRequest{SessionId: created.SessionId})
	if err != nil || !got.Found || got.State != sessionActive {
		t.Fatalf("owner mismatch changed session: %v %v", got, err)
	}
}

func TestDeleteCannotRaceCreatingSession(t *testing.T) {
	createStarted := make(chan struct{}, 1)
	createRelease := make(chan struct{})
	fake := &controlledUPF{
		createStarted: createStarted,
		createRelease: createRelease,
		createSuccess: true,
		deleteSuccess: true,
	}
	h := NewSMFHandlerWithClient(fake, time.Second, 1)

	type createResult struct {
		response *smfpb.CreateSessionResponse
		err      error
	}
	result := make(chan createResult, 1)
	go func() {
		response, err := h.CreateSession(context.Background(), createRequest("u1"))
		result <- createResult{response: response, err: err}
	}()
	waitSignal(t, createStarted)

	if _, err := h.DeleteSession(context.Background(), deleteRequest("SESSION-u1", "u1")); status.Code(err) != codes.Aborted {
		t.Fatalf("delete while creating: expected Aborted, got %v", err)
	}
	if _, err := h.CreateSession(context.Background(), createRequest("u1")); status.Code(err) != codes.Aborted {
		t.Fatalf("second create while creating: expected Aborted, got %v", err)
	}
	close(createRelease)
	created := <-result
	if created.err != nil || created.response == nil || !created.response.Success {
		t.Fatalf("original create did not commit: %v %v", created.response, created.err)
	}
	got, err := h.GetSession(context.Background(), &smfpb.GetSessionRequest{SessionId: created.response.SessionId})
	if err != nil || !got.Found || got.State != sessionActive {
		t.Fatalf("created session not active: %v %v", got, err)
	}
	_, deleted := fake.counts()
	if deleted != 0 {
		t.Fatalf("delete reached UPF while create was in flight: calls=%d", deleted)
	}
}

func TestDeleteIsSingleFlightAndReturnsIPOnce(t *testing.T) {
	deleteStarted := make(chan struct{}, 1)
	deleteRelease := make(chan struct{})
	fake := &controlledUPF{
		createSuccess: true,
		deleteStarted: deleteStarted,
		deleteRelease: deleteRelease,
		deleteSuccess: true,
	}
	h := NewSMFHandlerWithClient(fake, time.Second, 1)
	created, err := h.CreateSession(context.Background(), createRequest("u1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	type deleteResult struct {
		response *smfpb.DeleteSessionResponse
		err      error
	}
	result := make(chan deleteResult, 1)
	go func() {
		response, err := h.DeleteSession(context.Background(), deleteRequest(created.SessionId, "u1"))
		result <- deleteResult{response: response, err: err}
	}()
	waitSignal(t, deleteStarted)

	if _, err := h.DeleteSession(context.Background(), deleteRequest(created.SessionId, "u1")); status.Code(err) != codes.Aborted {
		t.Fatalf("second delete: expected Aborted, got %v", err)
	}
	if _, err := h.CreateSession(context.Background(), createRequest("u1")); status.Code(err) != codes.Aborted {
		t.Fatalf("create during delete: expected Aborted, got %v", err)
	}
	close(deleteRelease)
	deleted := <-result
	if deleted.err != nil || deleted.response == nil || !deleted.response.Success {
		t.Fatalf("original delete failed: %v %v", deleted.response, deleted.err)
	}
	_, deleteCalls := fake.counts()
	if deleteCalls != 1 {
		t.Fatalf("expected one UPF delete, got %d", deleteCalls)
	}
	h.mu.RLock()
	freeCount := len(h.freeIPs)
	_, exists := h.sessions[created.SessionId]
	h.mu.RUnlock()
	if exists || freeCount != 1 {
		t.Fatalf("delete commit mismatch: exists=%v free IPs=%d", exists, freeCount)
	}

	response, err := h.DeleteSession(context.Background(), deleteRequest(created.SessionId, "u1"))
	if err != nil || !response.Success {
		t.Fatalf("idempotent delete: %v %v", response, err)
	}
	_, deleteCalls = fake.counts()
	if deleteCalls != 1 {
		t.Fatalf("idempotent delete reached UPF: calls=%d", deleteCalls)
	}
}

func TestStaleCreateCompletionCannotOverwriteNewGeneration(t *testing.T) {
	createStarted := make(chan struct{}, 1)
	createRelease := make(chan struct{})
	fake := &controlledUPF{
		createStarted: createStarted,
		createRelease: createRelease,
		createSuccess: true,
		deleteSuccess: true,
	}
	h := NewSMFHandlerWithClient(fake, time.Second, 2)
	errCh := make(chan error, 1)
	go func() {
		_, err := h.CreateSession(context.Background(), createRequest("u1"))
		errCh <- err
	}()
	waitSignal(t, createStarted)

	replacement := replaceGenerationForTest(t, h, "SESSION-u1")
	close(createRelease)
	if err := <-errCh; status.Code(err) != codes.Aborted {
		t.Fatalf("stale create: expected Aborted, got %v", err)
	}
	assertReplacementUnchanged(t, h, replacement)
	_, deleted := fake.counts()
	if deleted != 1 {
		t.Fatalf("stale UPF rule was not cleaned up: delete calls=%d", deleted)
	}
}

func TestStaleCreateFailureCannotRollbackNewGeneration(t *testing.T) {
	createStarted := make(chan struct{}, 1)
	createRelease := make(chan struct{})
	fake := &controlledUPF{
		createStarted: createStarted,
		createRelease: createRelease,
		createErr:     errors.New("injected create failure"),
	}
	h := NewSMFHandlerWithClient(fake, time.Second, 2)
	errCh := make(chan error, 1)
	go func() {
		_, err := h.CreateSession(context.Background(), createRequest("u1"))
		errCh <- err
	}()
	waitSignal(t, createStarted)

	replacement := replaceGenerationForTest(t, h, "SESSION-u1")
	close(createRelease)
	if err := <-errCh; status.Code(err) != codes.Unavailable {
		t.Fatalf("failed create: expected Unavailable, got %v", err)
	}
	assertReplacementUnchanged(t, h, replacement)
}

func TestStaleDeleteCompletionCannotRemoveNewGeneration(t *testing.T) {
	deleteStarted := make(chan struct{}, 1)
	deleteRelease := make(chan struct{})
	fake := &controlledUPF{
		createSuccess: true,
		deleteStarted: deleteStarted,
		deleteRelease: deleteRelease,
		deleteSuccess: true,
	}
	h := NewSMFHandlerWithClient(fake, time.Second, 2)
	created, err := h.CreateSession(context.Background(), createRequest("u1"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := h.DeleteSession(context.Background(), deleteRequest(created.SessionId, "u1"))
		errCh <- err
	}()
	waitSignal(t, deleteStarted)

	replacement := replaceGenerationForTest(t, h, created.SessionId)
	close(deleteRelease)
	if err := <-errCh; status.Code(err) != codes.Aborted {
		t.Fatalf("stale delete: expected Aborted, got %v", err)
	}
	assertReplacementUnchanged(t, h, replacement)
}

func replaceGenerationForTest(t *testing.T, h *SMFHandler, sessionID string) Session {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	old, ok := h.sessions[sessionID]
	if !ok {
		t.Fatalf("missing session %s", sessionID)
	}
	if len(h.freeIPs) == 0 {
		t.Fatal("test needs a free IP for replacement generation")
	}
	h.nextGen++
	replacement := old
	replacement.Generation = h.nextGen
	replacement.UEIP = h.freeIPs[0]
	replacement.RuleID = "rule-new-generation"
	replacement.State = sessionActive
	h.freeIPs = h.freeIPs[1:]
	h.sessions[sessionID] = replacement
	return replacement
}

func assertReplacementUnchanged(t *testing.T, h *SMFHandler, want Session) {
	t.Helper()
	h.mu.RLock()
	defer h.mu.RUnlock()
	got, ok := h.sessions[want.SessionID]
	if !ok || got != want {
		t.Fatalf("replacement changed: got=%+v exists=%v want=%+v", got, ok, want)
	}
	if len(h.freeIPs) != 0 {
		t.Fatalf("stale operation returned an IP owned by another generation: free=%v", h.freeIPs)
	}
}

func createRequest(id string) *smfpb.CreateSessionRequest {
	return &smfpb.CreateSessionRequest{UeId: id, AmfUeId: "amf-" + id, Dnn: "internet"}
}

func deleteRequest(session, id string) *smfpb.DeleteSessionRequest {
	return &smfpb.DeleteSessionRequest{SessionId: session, UeId: id}
}
