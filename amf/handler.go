package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	amfpb "github.com/5g-core/proto/amf"
	smfpb "github.com/5g-core/proto/smf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type UERecord struct {
	UEID, AMFUEID, SessionID, UEIP, State string
	Operation                             uint64
}

type AMFHandler struct {
	amfpb.UnimplementedAMFServiceServer
	mu            sync.RWMutex
	registeredUEs map[string]UERecord
	smfClient     smfpb.SMFServiceClient
	conn          *grpc.ClientConn
	callTimeout   time.Duration
	nextOperation uint64
}

func NewAMFHandler(smfAddress string, timeout time.Duration) (*AMFHandler, error) {
	conn, err := grpc.NewClient(smfAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("连接 SMF: %w", err)
	}
	return &AMFHandler{
		registeredUEs: make(map[string]UERecord),
		smfClient:     smfpb.NewSMFServiceClient(conn),
		conn:          conn,
		callTimeout:   timeout,
	}, nil
}

func NewAMFHandlerWithClient(client smfpb.SMFServiceClient, timeout time.Duration) *AMFHandler {
	return &AMFHandler{registeredUEs: make(map[string]UERecord), smfClient: client, callTimeout: timeout}
}

func (h *AMFHandler) Close() error {
	if h.conn != nil {
		return h.conn.Close()
	}
	return nil
}

func validateRegister(req *amfpb.RegisterRequest) error {
	if req.GetUeId() == "" {
		return status.Error(codes.InvalidArgument, "ue_id 不能为空")
	}
	if req.GetUeType() == "" {
		return status.Error(codes.InvalidArgument, "ue_type 不能为空")
	}
	return nil
}

func (h *AMFHandler) Register(ctx context.Context, req *amfpb.RegisterRequest) (*amfpb.RegisterResponse, error) {
	started := time.Now()
	if err := validateRegister(req); err != nil {
		return nil, err
	}

	// The channel gate is an admission-time decision. Once a UE is registered,
	// a repeated Register is an idempotent lookup and does not re-evaluate (or
	// tear down) the existing registration based on a later channel sample.
	h.mu.Lock()
	if existing, exists := h.registeredUEs[req.UeId]; exists {
		h.mu.Unlock()
		switch existing.State {
		case "REGISTERED":
			return responseForRecord(existing, "UE 已注册（幂等返回）", started), nil
		case "REGISTERING":
			return nil, status.Error(codes.Unavailable, "相同 UE 正在注册，请稍后重试")
		case "DEREGISTERING":
			return nil, status.Error(codes.Unavailable, "相同 UE 正在注销，请稍后重试")
		default:
			return nil, status.Errorf(codes.FailedPrecondition, "UE 状态异常: %s", existing.State)
		}
	}
	if req.SignalPower < -110 || req.Sinr < 0 {
		h.mu.Unlock()
		return &amfpb.RegisterResponse{Success: false, Message: fmt.Sprintf("信道质量不足(signal=%.2f dBm, SINR=%.2f dB)", req.SignalPower, req.Sinr), ProcessingMs: float64(time.Since(started).Microseconds()) / 1000}, nil
	}
	amfUEID := fmt.Sprintf("AMF-%s", req.UeId)
	h.nextOperation++
	operation := h.nextOperation
	h.registeredUEs[req.UeId] = UERecord{UEID: req.UeId, AMFUEID: amfUEID, State: "REGISTERING", Operation: operation}
	h.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	smfResp, err := h.smfClient.CreateSession(callCtx, &smfpb.CreateSessionRequest{UeId: req.UeId, AmfUeId: amfUEID, Dnn: "internet"})
	if err != nil {
		h.rollbackRegistration(req.UeId, operation)
		return nil, status.Errorf(codes.Unavailable, "SMF 建立会话失败: %v", err)
	}
	if !smfResp.Success {
		h.rollbackRegistration(req.UeId, operation)
		return &amfpb.RegisterResponse{Success: false, Message: "SMF拒绝建立会话: " + smfResp.Message, ProcessingMs: float64(time.Since(started).Microseconds()) / 1000}, nil
	}

	record := UERecord{UEID: req.UeId, AMFUEID: amfUEID, SessionID: smfResp.SessionId, UEIP: smfResp.UeIp, State: "REGISTERED", Operation: operation}
	h.mu.Lock()
	current, ok := h.registeredUEs[req.UeId]
	if !ok || current.Operation != operation || current.State != "REGISTERING" {
		h.mu.Unlock()
		return nil, status.Error(codes.Aborted, "注册状态在建立会话期间发生变化")
	}
	h.registeredUEs[req.UeId] = record
	h.mu.Unlock()
	log.Printf("注册成功 ue_id=%s session_id=%s ue_ip=%s", record.UEID, record.SessionID, record.UEIP)
	return responseForRecord(record, "注册和会话建立成功", started), nil
}

func (h *AMFHandler) rollbackRegistration(ueID string, operation uint64) {
	h.mu.Lock()
	if current, ok := h.registeredUEs[ueID]; ok && current.Operation == operation && current.State == "REGISTERING" {
		delete(h.registeredUEs, ueID)
	}
	h.mu.Unlock()
}

func responseForRecord(record UERecord, message string, started time.Time) *amfpb.RegisterResponse {
	return &amfpb.RegisterResponse{Success: true, AmfUeId: record.AMFUEID, SessionId: record.SessionID, UeIp: record.UEIP, Message: message, ProcessingMs: float64(time.Since(started).Microseconds()) / 1000}
}

func (h *AMFHandler) Deregister(ctx context.Context, req *amfpb.DeregisterRequest) (*amfpb.DeregisterResponse, error) {
	if req.GetUeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "ue_id 不能为空")
	}
	if req.GetAmfUeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "amf_ue_id 不能为空")
	}
	h.mu.Lock()
	record, exists := h.registeredUEs[req.UeId]
	if !exists {
		h.mu.Unlock()
		return &amfpb.DeregisterResponse{Success: true, Message: "UE 已注销（幂等返回）"}, nil
	}
	if record.AMFUEID != req.AmfUeId {
		h.mu.Unlock()
		return nil, status.Error(codes.FailedPrecondition, "amf_ue_id 与当前 UE 记录不匹配")
	}
	switch record.State {
	case "REGISTERING":
		h.mu.Unlock()
		return nil, status.Error(codes.Aborted, "UE 正在注册，暂不能注销")
	case "DEREGISTERING":
		h.mu.Unlock()
		return nil, status.Error(codes.Unavailable, "UE 正在注销，请稍后重试")
	case "REGISTERED":
		// Continue below after publishing the transitional state. A concurrent
		// Register will retry instead of receiving a stale success response.
	default:
		h.mu.Unlock()
		return nil, status.Errorf(codes.FailedPrecondition, "UE 状态异常: %s", record.State)
	}
	h.nextOperation++
	operation := h.nextOperation
	record.State = "DEREGISTERING"
	record.Operation = operation
	h.registeredUEs[req.UeId] = record
	h.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	resp, err := h.smfClient.DeleteSession(callCtx, &smfpb.DeleteSessionRequest{SessionId: record.SessionID, UeId: record.UEID})
	if err != nil {
		h.restoreRegistered(record, operation)
		return nil, status.Errorf(codes.Unavailable, "SMF 释放会话失败: %v", err)
	}
	if !resp.Success {
		h.restoreRegistered(record, operation)
		return &amfpb.DeregisterResponse{Success: false, Message: resp.Message}, nil
	}
	h.mu.Lock()
	current, ok := h.registeredUEs[req.UeId]
	if ok && current.Operation == operation && current.State == "DEREGISTERING" && current.AMFUEID == record.AMFUEID && current.SessionID == record.SessionID {
		delete(h.registeredUEs, req.UeId)
	}
	h.mu.Unlock()
	log.Printf("注销成功 ue_id=%s session_id=%s", record.UEID, record.SessionID)
	return &amfpb.DeregisterResponse{Success: true, Message: "UE、会话和转发规则已释放"}, nil
}

func (h *AMFHandler) restoreRegistered(record UERecord, operation uint64) {
	h.mu.Lock()
	if current, ok := h.registeredUEs[record.UEID]; ok && current.Operation == operation && current.State == "DEREGISTERING" {
		record.State = "REGISTERED"
		record.Operation = operation
		h.registeredUEs[record.UEID] = record
	}
	h.mu.Unlock()
}

func (h *AMFHandler) GetUE(_ context.Context, req *amfpb.GetUERequest) (*amfpb.GetUEResponse, error) {
	if req.GetUeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "ue_id 不能为空")
	}
	h.mu.RLock()
	record, exists := h.registeredUEs[req.UeId]
	h.mu.RUnlock()
	if !exists {
		return &amfpb.GetUEResponse{Found: false}, nil
	}
	return &amfpb.GetUEResponse{Found: true, UeId: record.UEID, AmfUeId: record.AMFUEID, SessionId: record.SessionID, UeIp: record.UEIP, State: record.State}, nil
}
