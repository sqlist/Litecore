package main

import (
	"context"
	"sync"
	"testing"
	"time"

	amfpb "github.com/5g-core/proto/amf"
	smfpb "github.com/5g-core/proto/smf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSMF struct{ created, deleted int }

func (f *fakeSMF) CreateSession(_ context.Context, in *smfpb.CreateSessionRequest, _ ...grpc.CallOption) (*smfpb.CreateSessionResponse, error) {
	f.created++
	return &smfpb.CreateSessionResponse{Success: true, SessionId: "session-" + in.UeId, UeIp: "10.0.0.1", RuleId: "rule-1"}, nil
}
func (f *fakeSMF) DeleteSession(context.Context, *smfpb.DeleteSessionRequest, ...grpc.CallOption) (*smfpb.DeleteSessionResponse, error) {
	f.deleted++
	return &smfpb.DeleteSessionResponse{Success: true}, nil
}
func (f *fakeSMF) GetSession(context.Context, *smfpb.GetSessionRequest, ...grpc.CallOption) (*smfpb.GetSessionResponse, error) {
	return &smfpb.GetSessionResponse{}, nil
}

func TestRegisterChannelGateAndLifecycle(t *testing.T) {
	fake := &fakeSMF{}
	h := NewAMFHandlerWithClient(fake, time.Second)
	bad, err := h.Register(context.Background(), &amfpb.RegisterRequest{UeId: "bad", UeType: "sim", SignalPower: -120, Sinr: 10})
	if err != nil || bad.Success || fake.created != 0 {
		t.Fatalf("bad channel accepted: %v %v", bad, err)
	}
	req := &amfpb.RegisterRequest{UeId: "u1", UeType: "sim", SignalPower: -80, Sinr: 20}
	first, err := h.Register(context.Background(), req)
	if err != nil || !first.Success {
		t.Fatalf("register: %v %v", first, err)
	}
	// Admission quality is checked only for the initial registration. A later
	// channel sample cannot evict an already registered UE.
	again, err := h.Register(context.Background(), &amfpb.RegisterRequest{UeId: "u1", UeType: "sim", SignalPower: -120, Sinr: -10})
	if err != nil || again.SessionId != first.SessionId || fake.created != 1 {
		t.Fatalf("idempotency failed: %v %v calls=%d", again, err, fake.created)
	}
	found, err := h.GetUE(context.Background(), &amfpb.GetUERequest{UeId: "u1"})
	if err != nil || !found.Found {
		t.Fatalf("get: %v %v", found, err)
	}
	if _, err := h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing amf_ue_id code=%s err=%v", status.Code(err), err)
	}
	if _, err := h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1", AmfUeId: "wrong"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mismatched amf_ue_id code=%s err=%v", status.Code(err), err)
	}
	deleted, err := h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1", AmfUeId: first.AmfUeId})
	if err != nil || !deleted.Success || fake.deleted != 1 {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	deleted, err = h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1", AmfUeId: first.AmfUeId})
	if err != nil || !deleted.Success || fake.deleted != 1 {
		t.Fatalf("idempotent delete: %v %v", deleted, err)
	}
}

type controlledSMF struct {
	createStarted, allowCreate chan struct{}
	deleteStarted, allowDelete chan struct{}
	createOnce, deleteOnce     sync.Once
}

func (f *controlledSMF) CreateSession(_ context.Context, in *smfpb.CreateSessionRequest, _ ...grpc.CallOption) (*smfpb.CreateSessionResponse, error) {
	f.createOnce.Do(func() {
		if f.createStarted != nil {
			close(f.createStarted)
		}
	})
	if f.allowCreate != nil {
		<-f.allowCreate
	}
	return &smfpb.CreateSessionResponse{Success: true, SessionId: "session-" + in.UeId, UeIp: "10.0.0.1", RuleId: "rule-1"}, nil
}

func (f *controlledSMF) DeleteSession(context.Context, *smfpb.DeleteSessionRequest, ...grpc.CallOption) (*smfpb.DeleteSessionResponse, error) {
	f.deleteOnce.Do(func() {
		if f.deleteStarted != nil {
			close(f.deleteStarted)
		}
	})
	if f.allowDelete != nil {
		<-f.allowDelete
	}
	return &smfpb.DeleteSessionResponse{Success: true}, nil
}

func (f *controlledSMF) GetSession(context.Context, *smfpb.GetSessionRequest, ...grpc.CallOption) (*smfpb.GetSessionResponse, error) {
	return &smfpb.GetSessionResponse{}, nil
}

func goodRequest(id string) *amfpb.RegisterRequest {
	return &amfpb.RegisterRequest{UeId: id, UeType: "sim", SignalPower: -80, Sinr: 20}
}

func TestConcurrentRegisterRetriesWhileFirstIsInFlight(t *testing.T) {
	fake := &controlledSMF{createStarted: make(chan struct{}), allowCreate: make(chan struct{})}
	h := NewAMFHandlerWithClient(fake, time.Second)
	result := make(chan *amfpb.RegisterResponse, 1)
	errs := make(chan error, 1)
	go func() {
		resp, err := h.Register(context.Background(), goodRequest("u1"))
		result <- resp
		errs <- err
	}()
	<-fake.createStarted

	if _, err := h.Register(context.Background(), goodRequest("u1")); status.Code(err) != codes.Unavailable {
		t.Fatalf("second register code=%s err=%v", status.Code(err), err)
	}
	if _, err := h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1", AmfUeId: "AMF-u1"}); status.Code(err) != codes.Aborted {
		t.Fatalf("deregister during register code=%s err=%v", status.Code(err), err)
	}
	close(fake.allowCreate)
	if resp, err := <-result, <-errs; err != nil || !resp.Success {
		t.Fatalf("first register resp=%v err=%v", resp, err)
	}
}

func TestRegisterDuringDeregisterDoesNotReturnStaleSuccess(t *testing.T) {
	fake := &controlledSMF{deleteStarted: make(chan struct{}), allowDelete: make(chan struct{})}
	h := NewAMFHandlerWithClient(fake, time.Second)
	registered, err := h.Register(context.Background(), goodRequest("u1"))
	if err != nil || !registered.Success {
		t.Fatalf("register resp=%v err=%v", registered, err)
	}

	result := make(chan *amfpb.DeregisterResponse, 1)
	errs := make(chan error, 1)
	go func() {
		resp, err := h.Deregister(context.Background(), &amfpb.DeregisterRequest{UeId: "u1", AmfUeId: registered.AmfUeId})
		result <- resp
		errs <- err
	}()
	<-fake.deleteStarted
	if _, err := h.Register(context.Background(), goodRequest("u1")); status.Code(err) != codes.Unavailable {
		t.Fatalf("register during deregister code=%s err=%v", status.Code(err), err)
	}
	close(fake.allowDelete)
	if resp, err := <-result, <-errs; err != nil || !resp.Success {
		t.Fatalf("deregister resp=%v err=%v", resp, err)
	}
	if resp, err := h.Register(context.Background(), goodRequest("u1")); err != nil || !resp.Success {
		t.Fatalf("retry after deregister resp=%v err=%v", resp, err)
	}
}
