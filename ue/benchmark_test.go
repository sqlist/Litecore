package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummarizeBenchmarkUsesOnlySuccessfulLatencies(t *testing.T) {
	results := []result{
		{RunID: "run-1", Success: true, Latency: 10 * time.Millisecond},
		{RunID: "run-1", Success: false, Latency: 9 * time.Second, RetryCount: 2},
		{RunID: "run-1", Success: true, Latency: 30 * time.Millisecond, RetryCount: 1},
	}
	stats := summarizeBenchmark(results, time.Second)
	if stats.Success != 2 || stats.Failure != 1 || stats.RPCAttempts != 6 {
		t.Fatalf("unexpected counts: %+v", stats)
	}
	if stats.Average != 20*time.Millisecond || stats.Maximum != 30*time.Millisecond {
		t.Fatalf("failed latency contaminated successful latency stats: %+v", stats)
	}
	if stats.TaskThroughput != 3 || stats.Goodput != 2 {
		t.Fatalf("unexpected throughput: %+v", stats)
	}
}

func TestBenchmarkReportHandlesNoSuccess(t *testing.T) {
	stats := summarizeBenchmark([]result{{RunID: "run-0", Success: false, Latency: time.Second}}, time.Second)
	if stats.HasSuccessfulLatency {
		t.Fatalf("zero-success stats unexpectedly have latency: %+v", stats)
	}
	report := formatBenchmarkReport(stats)
	for _, expected := range []string{"平均成功延迟: N/A", "P50: N/A", "最大成功延迟: N/A"} {
		if !strings.Contains(report, expected) {
			t.Fatalf("report %q does not contain %q", report, expected)
		}
	}
}

func TestWriteCSVIncludesRunAndRetryMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark.csv")
	results := []result{{
		RunID: "trial-7", Scenario: "mixed", ChannelSeed: 43, UEID: "UE-trial-7-000001",
		Success: true, RetryCount: 2, Latency: 1250 * time.Microsecond, Signal: -99, SINR: 4,
		AMFUEID: "AMF-1", SessionID: "SESSION-1", UEIP: "10.0.0.1", Message: "ok",
	}}
	if err := writeCSV(path, results); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open CSV: %v", err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV record count = %d, want 2", len(records))
	}
	columns := make(map[string]int)
	for index, name := range records[0] {
		columns[name] = index
	}
	for _, name := range []string{"run_id", "scenario", "channel_seed", "retry_count", "amf_ue_id"} {
		if _, found := columns[name]; !found {
			t.Fatalf("CSV missing column %q: %v", name, records[0])
		}
	}
	if records[1][columns["run_id"]] != "trial-7" || records[1][columns["retry_count"]] != "2" {
		t.Fatalf("unexpected CSV row: %v", records[1])
	}
}

func TestCleanupRegistrationsUsesResponseAMFUEID(t *testing.T) {
	client := &fakeAMFClient{}
	results := []result{
		{UEID: "UE-1", AMFUEID: "AMF-1", Success: true},
		{UEID: "UE-2", AMFUEID: "AMF-2", Success: true},
		{UEID: "UE-rejected", Success: false},
		{UEID: "UE-no-amf-id", Success: true},
	}
	summary := cleanupRegistrations(client, options{timeout: defaultRequestTimeout, concurrency: 2}, results)
	if summary.Target != 3 || summary.Attempted != 2 || summary.Succeeded != 2 || len(summary.Failures) != 1 {
		t.Fatalf("unexpected cleanup summary: %+v", summary)
	}
	if len(client.deregisterRequests) != 2 {
		t.Fatalf("deregister request count = %d, want 2", len(client.deregisterRequests))
	}
	seen := make(map[string]string)
	for _, request := range client.deregisterRequests {
		seen[request.UeId] = request.AmfUeId
	}
	if seen["UE-1"] != "AMF-1" || seen["UE-2"] != "AMF-2" {
		t.Fatalf("cleanup did not use registration response IDs: %v", seen)
	}
}

func TestRunIDIsExplicitOrUnique(t *testing.T) {
	explicit, err := resolveRunID("repeatable_trial-1")
	if err != nil || explicit != "repeatable_trial-1" {
		t.Fatalf("explicit run ID: id=%q err=%v", explicit, err)
	}
	if got := benchmarkUEID(explicit, 0); got != "UE-repeatable_trial-1-000001" {
		t.Fatalf("unexpected benchmark UE ID: %s", got)
	}
	first, err := resolveRunID("")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveRunID("")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("generated run IDs are not unique: %q", first)
	}
	if _, err := resolveRunID("not valid/路径"); err == nil {
		t.Fatal("invalid run ID was accepted")
	}
}

func TestRunBenchmarkWritesCSVThenCleansSuccessfulUEs(t *testing.T) {
	client := &fakeAMFClient{}
	path := filepath.Join(t.TempDir(), "run.csv")
	o := options{
		count: 3, concurrency: 2, timeout: defaultRequestTimeout,
		runID: "integration", scenario: "stable", seed: 42, output: path,
	}
	if err := runBenchmark(client, o); err != nil {
		t.Fatalf("runBenchmark: %v", err)
	}
	if len(client.registerRequests) != 3 || len(client.deregisterRequests) != 3 {
		t.Fatalf("register/deregister calls = %d/%d, want 3/3", len(client.registerRequests), len(client.deregisterRequests))
	}
	for _, request := range client.deregisterRequests {
		if request.AmfUeId != "AMF-"+request.UeId {
			t.Fatalf("cleanup ID mismatch: %+v", request)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("CSV was not written: %v", err)
	}
}
