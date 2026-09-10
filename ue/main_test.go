package main

import (
	"strings"
	"testing"

	amfpb "github.com/5g-core/proto/amf"
)

func TestFormatUEStatus(t *testing.T) {
	found := formatUEStatus("UE-001", &amfpb.GetUEResponse{
		Found:     true,
		UeId:      "UE-001",
		AmfUeId:   "AMF-UE-001",
		SessionId: "SESSION-UE-001",
		UeIp:      "10.0.0.1",
		State:     "REGISTERED",
	})
	for _, expected := range []string{"UE-001", "REGISTERED", "AMF-UE-001", "SESSION-UE-001", "10.0.0.1"} {
		if !strings.Contains(found, expected) {
			t.Fatalf("status %q does not contain %q", found, expected)
		}
	}

	missing := formatUEStatus("UE-404", &amfpb.GetUEResponse{})
	if !strings.Contains(missing, "UE-404") || !strings.Contains(missing, "未找到") {
		t.Fatalf("unexpected missing status: %q", missing)
	}
}
