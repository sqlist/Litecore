package main

import (
	"context"
	"sync"
	"testing"
	"time"

	amfpb "github.com/5g-core/proto/amf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type registerStep struct {
	response *amfpb.RegisterResponse
	err      error
}

type fakeAMFClient struct {
	mu sync.Mutex

	registerSteps    []registerStep
	registerCalls    int
	registerRequests []*amfpb.RegisterRequest

	deregisterRequests []*amfpb.DeregisterRequest
	deregisterResponse *amfpb.DeregisterResponse
	deregisterErr      error

	getResponse *amfpb.GetUEResponse
	getErr      error
}

func (f *fakeAMFClient) Register(_ context.Context, request *amfpb.RegisterRequest, _ ...grpc.CallOption) (*amfpb.RegisterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registerRequests = append(f.registerRequests, &amfpb.RegisterRequest{
		UeId: request.UeId, UeType: request.UeType, SignalPower: request.SignalPower, Sinr: request.Sinr,
	})
	call := f.registerCalls
	f.registerCalls++
	if call < len(f.registerSteps) {
		step := f.registerSteps[call]
		return step.response, step.err
	}
	return &amfpb.RegisterResponse{
		Success: true, AmfUeId: "AMF-" + request.UeId, SessionId: "SESSION-" + request.UeId, UeIp: "10.0.0.1", Message: "ok",
	}, nil
}

func (f *fakeAMFClient) Deregister(_ context.Context, request *amfpb.DeregisterRequest, _ ...grpc.CallOption) (*amfpb.DeregisterResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deregisterRequests = append(f.deregisterRequests, &amfpb.DeregisterRequest{UeId: request.UeId, AmfUeId: request.AmfUeId})
	if f.deregisterResponse == nil && f.deregisterErr == nil {
		return &amfpb.DeregisterResponse{Success: true, Message: "ok"}, nil
	}
	return f.deregisterResponse, f.deregisterErr
}

func (f *fakeAMFClient) GetUE(_ context.Context, _ *amfpb.GetUERequest, _ ...grpc.CallOption) (*amfpb.GetUEResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.getResponse, f.getErr
}

func TestDefaultRequestTimeout(t *testing.T) {
	if defaultRequestTimeout != 4*time.Second {
		t.Fatalf("default timeout = %s, want 4s", defaultRequestTimeout)
	}
}

func TestRegisterRetriesTransientErrorAndKeepsIdentifiers(t *testing.T) {
	client := &fakeAMFClient{registerSteps: []registerStep{
		{err: status.Error(codes.Unavailable, "AMF暂不可用")},
		{response: &amfpb.RegisterResponse{
			Success: true, Message: "registered", AmfUeId: "AMF-7", SessionId: "SESSION-7", UeIp: "10.0.0.7",
		}},
	}}
	var waits []time.Duration
	got := registerWithRetry(
		client,
		options{timeout: defaultRequestTimeout},
		"UE-7",
		ChannelSample{SignalPower: -90, SINR: 8},
		func(wait time.Duration) { waits = append(waits, wait) },
	)

	if !got.Success || got.RetryCount != 1 || client.registerCalls != 2 {
		t.Fatalf("unexpected retry result: result=%+v calls=%d", got, client.registerCalls)
	}
	if got.AMFUEID != "AMF-7" || got.SessionID != "SESSION-7" || got.UEIP != "10.0.0.7" {
		t.Fatalf("registration identifiers were not retained: %+v", got)
	}
	if len(waits) != 1 || waits[0] != initialRegistrationRetryWait {
		t.Fatalf("unexpected retry waits: %v", waits)
	}
}

func TestRegisterRetriesAtMostTwice(t *testing.T) {
	client := &fakeAMFClient{registerSteps: []registerStep{
		{err: status.Error(codes.Aborted, "并发冲突")},
		{err: status.Error(codes.Unavailable, "暂不可用")},
		{err: status.Error(codes.DeadlineExceeded, "超时")},
	}}
	var waits []time.Duration
	got := registerWithRetry(
		client,
		options{timeout: defaultRequestTimeout},
		"UE-8",
		ChannelSample{},
		func(wait time.Duration) { waits = append(waits, wait) },
	)
	if got.Success || got.RetryCount != maxRegistrationRetries || client.registerCalls != maxRegistrationRetries+1 {
		t.Fatalf("unexpected exhausted retry result: result=%+v calls=%d", got, client.registerCalls)
	}
	if len(waits) != 2 || waits[0] != 100*time.Millisecond || waits[1] != 200*time.Millisecond {
		t.Fatalf("unexpected retry waits: %v", waits)
	}
}

func TestRegisterDoesNotRetryPermanentOrBusinessFailure(t *testing.T) {
	tests := []struct {
		name string
		step registerStep
	}{
		{name: "permanent grpc error", step: registerStep{err: status.Error(codes.InvalidArgument, "bad request")}},
		{name: "business rejection", step: registerStep{response: &amfpb.RegisterResponse{Success: false, Message: "radio rejected"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeAMFClient{registerSteps: []registerStep{test.step}}
			got := registerWithRetry(client, options{timeout: defaultRequestTimeout}, "UE-X", ChannelSample{}, func(time.Duration) {
				t.Fatal("unexpected retry sleep")
			})
			if got.Success || got.RetryCount != 0 || client.registerCalls != 1 {
				t.Fatalf("unexpected result: result=%+v calls=%d", got, client.registerCalls)
			}
		})
	}
}

func TestDeregisterLooksUpAMFUEID(t *testing.T) {
	client := &fakeAMFClient{getResponse: &amfpb.GetUEResponse{Found: true, UeId: "UE-9", AmfUeId: "AMF-9"}}
	resp, err := deregister(client, options{timeout: defaultRequestTimeout}, "UE-9", "")
	if err != nil || !resp.Success {
		t.Fatalf("deregister failed: response=%+v err=%v", resp, err)
	}
	if len(client.deregisterRequests) != 1 {
		t.Fatalf("deregister calls = %d, want 1", len(client.deregisterRequests))
	}
	request := client.deregisterRequests[0]
	if request.UeId != "UE-9" || request.AmfUeId != "AMF-9" {
		t.Fatalf("unexpected deregister request: %+v", request)
	}
}

func TestDeregisterMissingUEIsIdempotent(t *testing.T) {
	client := &fakeAMFClient{getResponse: &amfpb.GetUEResponse{Found: false}}
	resp, err := deregister(client, options{timeout: defaultRequestTimeout}, "UE-missing", "")
	if err != nil || !resp.Success {
		t.Fatalf("missing UE should be an idempotent success: response=%+v err=%v", resp, err)
	}
	if len(client.deregisterRequests) != 0 {
		t.Fatalf("unexpected deregister RPC for missing UE: %d", len(client.deregisterRequests))
	}
}
