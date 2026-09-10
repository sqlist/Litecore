package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	amfpb "github.com/5g-core/proto/amf"
)

const maxRunIDLength = 64

var runSequence atomic.Uint64

func runBenchmark(client amfpb.AMFServiceClient, o options) error {
	if o.count < 1 || o.concurrency < 1 {
		return fmt.Errorf("count和concurrency必须大于0")
	}
	if o.timeout <= 0 {
		return fmt.Errorf("timeout必须大于0")
	}

	runID, err := resolveRunID(o.runID)
	if err != nil {
		return err
	}
	log.Printf("开始压测 run_id=%s count=%d concurrency=%d scenario=%s seed=%d", runID, o.count, o.concurrency, o.scenario, o.seed)

	model := NewScenarioChannelModel(o.scenario)
	started := time.Now()
	results := make([]result, o.count)
	sem := make(chan struct{}, o.concurrency)
	var wg sync.WaitGroup
	for i := 0; i < o.count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(index int) {
			defer wg.Done()
			defer func() { <-sem }()

			channelSeed := o.seed + int64(index)
			channel := model.Sample(rand.New(rand.NewSource(channelSeed)))
			r := register(client, o, benchmarkUEID(runID, index), channel)
			r.RunID = runID
			r.Scenario = o.scenario
			r.ChannelSeed = channelSeed
			results[index] = r
		}(i)
	}
	wg.Wait()
	wall := time.Since(started)

	report(results, wall)
	var outputErr error
	if o.output != "" {
		if err := writeCSV(o.output, results); err != nil {
			outputErr = fmt.Errorf("写CSV: %w", err)
		} else {
			log.Printf("注册明细已写入 %s", o.output)
		}
	}

	// Cleanup deliberately starts only after registration reporting and CSV output,
	// so its RPCs and latency cannot contaminate the registration measurement.
	cleanup := cleanupRegistrations(client, o, results)
	reportCleanup(runID, cleanup)
	return outputErr
}

func benchmarkUEID(runID string, index int) string {
	return fmt.Sprintf("UE-%s-%06d", runID, index+1)
}

func resolveRunID(requested string) (string, error) {
	if requested == "" {
		return fmt.Sprintf(
			"%s-p%d-%d",
			time.Now().UTC().Format("20060102T150405.000000000Z"),
			os.Getpid(),
			runSequence.Add(1),
		), nil
	}
	if len(requested) > maxRunIDLength {
		return "", fmt.Errorf("run-id最多%d个字符", maxRunIDLength)
	}
	for _, char := range requested {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return "", fmt.Errorf("run-id只能包含字母、数字、点、下划线和连字符")
	}
	return requested, nil
}

type benchmarkStats struct {
	RunID                                string
	Total, Success, Failure, RPCAttempts int
	Wall                                 time.Duration
	SuccessRate, TaskThroughput, Goodput float64
	HasSuccessfulLatency                 bool
	Average, P50, P95, P99, Maximum      time.Duration
}

func summarizeBenchmark(results []result, wall time.Duration) benchmarkStats {
	stats := benchmarkStats{Total: len(results), Failure: len(results), Wall: wall}
	latencies := make([]time.Duration, 0, len(results))
	for _, r := range results {
		if stats.RunID == "" {
			stats.RunID = r.RunID
		}
		stats.RPCAttempts += r.RetryCount + 1
		if !r.Success {
			continue
		}
		stats.Success++
		stats.Failure--
		latencies = append(latencies, r.Latency)
	}

	if stats.Total > 0 {
		stats.SuccessRate = float64(stats.Success) * 100 / float64(stats.Total)
	}
	if wall > 0 {
		stats.TaskThroughput = float64(stats.Total) / wall.Seconds()
		stats.Goodput = float64(stats.Success) / wall.Seconds()
	}
	if len(latencies) == 0 {
		return stats
	}

	stats.HasSuccessfulLatency = true
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var totalLatency time.Duration
	for _, latency := range latencies {
		totalLatency += latency
	}
	stats.Average = totalLatency / time.Duration(len(latencies))
	stats.P50 = durationPercentile(latencies, .50)
	stats.P95 = durationPercentile(latencies, .95)
	stats.P99 = durationPercentile(latencies, .99)
	stats.Maximum = latencies[len(latencies)-1]
	return stats
}

func durationPercentile(sorted []time.Duration, p float64) time.Duration {
	index := int(math.Ceil(p*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func report(results []result, wall time.Duration) {
	fmt.Print(formatBenchmarkReport(summarizeBenchmark(results, wall)))
}

func formatBenchmarkReport(stats benchmarkStats) string {
	var output strings.Builder
	fmt.Fprintf(&output, "\nLiteCore 压测结果\n")
	fmt.Fprintf(&output, "运行 ID: %s\n", stats.RunID)
	fmt.Fprintf(&output, "总注册任务: %d\n", stats.Total)
	fmt.Fprintf(&output, "RPC 调用尝试: %d（包含重试）\n", stats.RPCAttempts)
	fmt.Fprintf(&output, "成功注册: %d\n", stats.Success)
	fmt.Fprintf(&output, "失败注册: %d\n", stats.Failure)
	fmt.Fprintf(&output, "成功率: %.2f%%\n", stats.SuccessRate)
	fmt.Fprintf(&output, "注册测量总耗时: %s\n", stats.Wall)
	fmt.Fprintf(&output, "任务吞吐量: %.2f UE/s（所有注册任务）\n", stats.TaskThroughput)
	fmt.Fprintf(&output, "成功吞吐量: %.2f UE/s（仅成功注册）\n", stats.Goodput)
	fmt.Fprintf(&output, "延迟口径: 仅成功注册；从首次尝试到最终成功，包含重试等待\n")
	if !stats.HasSuccessfulLatency {
		fmt.Fprintf(&output, "平均成功延迟: N/A\nP50: N/A\nP95: N/A\nP99: N/A\n最大成功延迟: N/A\n")
		return output.String()
	}
	fmt.Fprintf(&output, "平均成功延迟: %s\n", stats.Average)
	fmt.Fprintf(&output, "P50: %s\nP95: %s\nP99: %s\n", stats.P50, stats.P95, stats.P99)
	fmt.Fprintf(&output, "最大成功延迟: %s\n", stats.Maximum)
	return output.String()
}

func writeCSV(path string, results []result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	if err := w.Write([]string{
		"run_id", "scenario", "channel_seed", "ue_id", "success", "retry_count", "latency_ms",
		"signal_power_dbm", "sinr_db", "amf_ue_id", "session_id", "ue_ip", "message",
	}); err != nil {
		return err
	}
	for _, r := range results {
		if err := w.Write([]string{
			r.RunID,
			r.Scenario,
			strconv.FormatInt(r.ChannelSeed, 10),
			r.UEID,
			strconv.FormatBool(r.Success),
			strconv.Itoa(r.RetryCount),
			fmt.Sprintf("%.3f", float64(r.Latency.Microseconds())/1000),
			fmt.Sprintf("%.2f", r.Signal),
			fmt.Sprintf("%.2f", r.SINR),
			r.AMFUEID,
			r.SessionID,
			r.UEIP,
			r.Message,
		}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

type cleanupSummary struct {
	Target, Attempted, Succeeded int
	Failures                     []string
}

func cleanupRegistrations(client amfpb.AMFServiceClient, o options, results []result) cleanupSummary {
	targets := make([]result, 0, len(results))
	for _, r := range results {
		if r.Success {
			targets = append(targets, r)
		}
	}
	summary := cleanupSummary{Target: len(targets)}
	if len(targets) == 0 {
		return summary
	}

	concurrency := o.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	recordFailure := func(message string) {
		mu.Lock()
		summary.Failures = append(summary.Failures, message)
		mu.Unlock()
	}

	for _, target := range targets {
		target := target
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if target.AMFUEID == "" {
				recordFailure(fmt.Sprintf("ue_id=%s: 注册响应缺少amf_ue_id", target.UEID))
				return
			}

			mu.Lock()
			summary.Attempted++
			mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
			resp, err := client.Deregister(ctx, &amfpb.DeregisterRequest{UeId: target.UEID, AmfUeId: target.AMFUEID})
			cancel()
			if err != nil {
				recordFailure(fmt.Sprintf("ue_id=%s: %v", target.UEID, err))
				return
			}
			if resp == nil || !resp.Success {
				message := "空响应"
				if resp != nil {
					message = resp.Message
				}
				recordFailure(fmt.Sprintf("ue_id=%s: %s", target.UEID, message))
				return
			}
			mu.Lock()
			summary.Succeeded++
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Strings(summary.Failures)
	return summary
}

func reportCleanup(runID string, summary cleanupSummary) {
	log.Printf(
		"压测资源清理 run_id=%s 目标=%d 已发送=%d 成功=%d 失败=%d",
		runID,
		summary.Target,
		summary.Attempted,
		summary.Succeeded,
		summary.Target-summary.Succeeded,
	)
	const maxFailureDetails = 10
	for index, failure := range summary.Failures {
		if index == maxFailureDetails {
			log.Printf("其余 %d 个清理失败未逐条显示", len(summary.Failures)-maxFailureDetails)
			break
		}
		log.Printf("清理失败: %s", failure)
	}
}
