package main

import (
	"context"
	"testing"
	"time"

	upfpb "github.com/5g-core/proto/upf"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestRuleLifecycleAndIdempotency(t *testing.T) {
	h := NewUPFHandlerWithInterval(time.Millisecond)
	defer h.Close()
	req := &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.1", Dnn: "internet"}
	first, err := h.CreateRule(context.Background(), req)
	if err != nil || !first.Success {
		t.Fatalf("create: resp=%v err=%v", first, err)
	}
	second, err := h.CreateRule(context.Background(), req)
	if err != nil || second.RuleId != first.RuleId {
		t.Fatalf("idempotent create: resp=%v err=%v", second, err)
	}
	time.Sleep(3 * time.Millisecond)
	stats, err := h.GetStats(context.Background(), &upfpb.GetStatsRequest{SessionId: "s1"})
	if err != nil || !stats.Found || !stats.Active || stats.PacketsForwarded == 0 {
		t.Fatalf("stats: resp=%v err=%v", stats, err)
	}
	deleted, err := h.DeleteRule(context.Background(), &upfpb.DeleteRuleRequest{SessionId: "s1"})
	if err != nil || !deleted.Success {
		t.Fatalf("delete: resp=%v err=%v", deleted, err)
	}
	deleted, err = h.DeleteRule(context.Background(), &upfpb.DeleteRuleRequest{SessionId: "s1"})
	if err != nil || !deleted.Success {
		t.Fatalf("idempotent delete: resp=%v err=%v", deleted, err)
	}
}

func TestGetStatsNotFound(t *testing.T) {
	h := NewUPFHandler()
	defer h.Close()

	stats, err := h.GetStats(context.Background(), &upfpb.GetStatsRequest{SessionId: "missing"})
	if err != nil {
		t.Fatalf("get missing stats: %v", err)
	}
	if stats.Found {
		t.Fatalf("missing rule reported as found: %v", stats)
	}
}

func TestGetStatsFoundSurvivesProtoRoundTrip(t *testing.T) {
	want := &upfpb.GetStatsResponse{Found: true, RuleId: "rule-1"}
	wire, err := proto.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got upfpb.GetStatsResponse
	if err := proto.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Found || got.RuleId != want.RuleId {
		t.Fatalf("round trip: got=%v want=%v", &got, want)
	}
}

func TestCreateRuleRejectsConflictingIdempotencyKey(t *testing.T) {
	h := NewUPFHandler()
	defer h.Close()
	base := &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.1", Dnn: "internet"}
	if _, err := h.CreateRule(context.Background(), base); err != nil {
		t.Fatalf("create base rule: %v", err)
	}

	tests := []struct {
		name string
		req  *upfpb.CreateRuleRequest
	}{
		{name: "ue id", req: &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u2", UeIp: "10.0.0.1", Dnn: "internet"}},
		{name: "ue ip", req: &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.2", Dnn: "internet"}},
		{name: "dnn", req: &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.1", Dnn: "private"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := h.CreateRule(context.Background(), tt.req); status.Code(err) != codes.AlreadyExists {
				t.Fatalf("code=%v err=%v, want %v", status.Code(err), err, codes.AlreadyExists)
			}
		})
	}
}

func TestDeleteRuleRejectsMismatchedIdentifiers(t *testing.T) {
	h := NewUPFHandler()
	defer h.Close()
	first, err := h.CreateRule(context.Background(), &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.1"})
	if err != nil {
		t.Fatalf("create first rule: %v", err)
	}
	if _, err := h.CreateRule(context.Background(), &upfpb.CreateRuleRequest{SessionId: "s2", UeId: "u2", UeIp: "10.0.0.2"}); err != nil {
		t.Fatalf("create second rule: %v", err)
	}

	_, err = h.DeleteRule(context.Background(), &upfpb.DeleteRuleRequest{RuleId: first.RuleId, SessionId: "s2"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code=%v err=%v, want %v", status.Code(err), err, codes.InvalidArgument)
	}
	stats, err := h.GetStats(context.Background(), &upfpb.GetStatsRequest{RuleId: first.RuleId})
	if err != nil || !stats.Found {
		t.Fatalf("mismatched delete removed rule: stats=%v err=%v", stats, err)
	}
}

func TestCloseStopsWorkersAndRejectsNewRules(t *testing.T) {
	h := NewUPFHandlerWithInterval(time.Millisecond)
	created, err := h.CreateRule(context.Background(), &upfpb.CreateRuleRequest{SessionId: "s1", UeId: "u1", UeIp: "10.0.0.1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	time.Sleep(3 * time.Millisecond)
	h.Close()
	h.Close() // Close is idempotent.

	stats, err := h.GetStats(context.Background(), &upfpb.GetStatsRequest{RuleId: created.RuleId})
	if err != nil || !stats.Found || stats.Active {
		t.Fatalf("stats after close: resp=%v err=%v", stats, err)
	}
	count := stats.PacketsForwarded
	time.Sleep(3 * time.Millisecond)
	stats, err = h.GetStats(context.Background(), &upfpb.GetStatsRequest{RuleId: created.RuleId})
	if err != nil || stats.PacketsForwarded != count {
		t.Fatalf("worker continued after close: before=%d after=%d err=%v", count, stats.PacketsForwarded, err)
	}

	_, err = h.CreateRule(context.Background(), &upfpb.CreateRuleRequest{SessionId: "s2", UeId: "u2", UeIp: "10.0.0.2"})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("create after close: code=%v err=%v", status.Code(err), err)
	}
}
