package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	amfpb "github.com/5g-core/proto/amf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	defaultRequestTimeout        = 4 * time.Second
	maxRegistrationRetries       = 2
	initialRegistrationRetryWait = 100 * time.Millisecond
)

type options struct {
	address, action, ueID, amfUEID, scenario, output, runID string
	count, concurrency                                      int
	timeout                                                 time.Duration
	seed                                                    int64
}
type result struct {
	RunID, Scenario                string
	ChannelSeed                    int64
	UEID, AMFUEID, SessionID, UEIP string
	Success                        bool
	Latency                        time.Duration
	Signal, SINR                   float32
	RetryCount                     int
	Message                        string
}

func parseFlags() options {
	var o options
	flag.StringVar(&o.address, "address", "localhost:50051", "AMF gRPC地址")
	flag.StringVar(&o.action, "action", "register", "register、get、deregister或benchmark")
	flag.StringVar(&o.ueID, "id", "UE-001", "单UE ID")
	flag.StringVar(&o.amfUEID, "amf-ue-id", "", "注销时的AMF UE ID；留空会先查询")
	flag.StringVar(&o.scenario, "scenario", "stable", "stable、edge、degrading或mixed")
	flag.StringVar(&o.output, "output", "", "压测明细CSV路径（可选）")
	flag.StringVar(&o.runID, "run-id", "", "压测运行ID（可选；留空自动生成唯一值）")
	flag.IntVar(&o.count, "count", 100, "压测UE数量")
	flag.IntVar(&o.concurrency, "concurrency", 20, "压测并发数")
	flag.DurationVar(&o.timeout, "timeout", defaultRequestTimeout, "单次RPC请求超时")
	flag.Int64Var(&o.seed, "seed", 42, "信道随机种子")
	flag.Parse()
	return o
}

func main() {
	o := parseFlags()
	conn, err := grpc.NewClient(o.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("连接 AMF: %v", err)
	}
	defer conn.Close()
	client := amfpb.NewAMFServiceClient(conn)
	switch o.action {
	case "register":
		model := NewScenarioChannelModel(o.scenario)
		r := register(client, o, o.ueID, model.Sample(rand.New(rand.NewSource(o.seed))))
		if !r.Success {
			log.Fatalf("注册失败 ue_id=%s retries=%d message=%s latency=%s", r.UEID, r.RetryCount, r.Message, r.Latency)
		}
		log.Printf("注册成功 ue_id=%s amf_ue_id=%s retries=%d latency=%s message=%s", r.UEID, r.AMFUEID, r.RetryCount, r.Latency, r.Message)
	case "deregister":
		resp, err := deregister(client, o, o.ueID, o.amfUEID)
		if err != nil {
			log.Fatalf("注销失败: %v", err)
		}
		log.Printf("注销结果 success=%v message=%s", resp.Success, resp.Message)
	case "get":
		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		defer cancel()
		resp, err := client.GetUE(ctx, &amfpb.GetUERequest{UeId: o.ueID})
		if err != nil {
			log.Fatalf("查询失败: %v", err)
		}
		log.Print(formatUEStatus(o.ueID, resp))
	case "benchmark":
		if err := runBenchmark(client, o); err != nil {
			log.Fatalf("压测失败: %v", err)
		}
	default:
		log.Fatalf("不支持的action: %s", o.action)
	}
}

func formatUEStatus(requestedID string, resp *amfpb.GetUEResponse) string {
	if !resp.Found {
		return fmt.Sprintf("未找到 UE ue_id=%s", requestedID)
	}
	return fmt.Sprintf(
		"查询成功 ue_id=%s state=%s amf_ue_id=%s session_id=%s ue_ip=%s",
		resp.UeId,
		resp.State,
		resp.AmfUeId,
		resp.SessionId,
		resp.UeIp,
	)
}

func register(client amfpb.AMFServiceClient, o options, ueID string, channel ChannelSample) result {
	return registerWithRetry(client, o, ueID, channel, time.Sleep)
}

func registerWithRetry(client amfpb.AMFServiceClient, o options, ueID string, channel ChannelSample, sleep func(time.Duration)) result {
	started := time.Now()
	r := result{UEID: ueID, Signal: channel.SignalPower, SINR: channel.SINR}
	request := &amfpb.RegisterRequest{UeId: ueID, UeType: "simulator", SignalPower: channel.SignalPower, Sinr: channel.SINR}

	for attempt := 0; attempt <= maxRegistrationRetries; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		resp, err := client.Register(ctx, request)
		cancel()
		if err == nil {
			if resp == nil {
				r.Message = "AMF返回空注册响应"
			} else {
				r.Success = resp.Success
				r.Message = resp.Message
				r.AMFUEID = resp.AmfUeId
				r.SessionID = resp.SessionId
				r.UEIP = resp.UeIp
			}
			r.Latency = time.Since(started)
			return r
		}

		r.Message = err.Error()
		if attempt == maxRegistrationRetries || !isRetryableRegistrationError(err) {
			break
		}
		r.RetryCount++
		sleep(registrationRetryWait(r.RetryCount))
	}
	r.Latency = time.Since(started)
	return r
}

func isRetryableRegistrationError(err error) bool {
	switch status.Code(err) {
	case codes.Aborted, codes.DeadlineExceeded, codes.Unavailable:
		return true
	default:
		return false
	}
}

func registrationRetryWait(retryNumber int) time.Duration {
	return initialRegistrationRetryWait * time.Duration(1<<(retryNumber-1))
}

func deregister(client amfpb.AMFServiceClient, o options, ueID, amfUEID string) (*amfpb.DeregisterResponse, error) {
	if amfUEID == "" {
		ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
		current, err := client.GetUE(ctx, &amfpb.GetUERequest{UeId: ueID})
		cancel()
		if err != nil {
			return nil, fmt.Errorf("查询AMF UE ID: %w", err)
		}
		if current == nil || !current.Found {
			return &amfpb.DeregisterResponse{Success: true, Message: "UE已不存在，无需重复注销"}, nil
		}
		if current.AmfUeId == "" {
			return nil, fmt.Errorf("UE缺少AMF UE ID，无法安全注销: %s", ueID)
		}
		amfUEID = current.AmfUeId
	}

	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	resp, err := client.Deregister(ctx, &amfpb.DeregisterRequest{UeId: ueID, AmfUeId: amfUEID})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("AMF返回空注销响应")
	}
	return resp, nil
}
