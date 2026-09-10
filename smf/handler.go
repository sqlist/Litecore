package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	smfpb "github.com/5g-core/proto/smf"
	upfpb "github.com/5g-core/proto/upf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	sessionCreating = "CREATING"
	sessionActive   = "ACTIVE"
	sessionDeleting = "DELETING"
)

// Session 是 SMF 保存的会话快照。Generation 只在进程内部使用，用于区分
// 同一个 UE 的多次创建，避免较早的异步调用覆盖或回滚较新的会话。
type Session struct {
	SessionID, UEID, AMFUEID, UEIP, DNN, RuleID, State string
	Generation                                         uint64
}

type SMFHandler struct {
	smfpb.UnimplementedSMFServiceServer
	mu          sync.RWMutex
	sessions    map[string]Session //key:SessionID → Session会话对象
	byUE        map[string]string //key:UEID手机ID → SessionID，手机查会话
	freeIPs     []string
	upfClient   upfpb.UPFServiceClient
	conn        *grpc.ClientConn
	callTimeout time.Duration
	nextGen     uint64
}

func defaultIPPool(size int) []string {
	pool := make([]string, 0, size)
	for i := 1; i <= size; i++ {
		pool = append(pool, fmt.Sprintf("10.0.%d.%d", (i-1)/254, (i-1)%254+1))
	}
	return pool
}

func NewSMFHandler(upfAddress string, timeout time.Duration, poolSize int) (*SMFHandler, error) {
	conn, err := grpc.NewClient(upfAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("连接 UPF: %w", err)
	}
	return newSMFHandler(upfpb.NewUPFServiceClient(conn), conn, timeout, poolSize), nil
}

func NewSMFHandlerWithClient(client upfpb.UPFServiceClient, timeout time.Duration, poolSize int) *SMFHandler {
	return newSMFHandler(client, nil, timeout, poolSize)
}

func newSMFHandler(client upfpb.UPFServiceClient, conn *grpc.ClientConn, timeout time.Duration, poolSize int) *SMFHandler {
	if poolSize < 1 {
		poolSize = 254
	}
	return &SMFHandler{sessions: make(map[string]Session), byUE: make(map[string]string), freeIPs: defaultIPPool(poolSize), upfClient: client, conn: conn, callTimeout: timeout}
}

func (h *SMFHandler) Close() error {
	if h.conn != nil {
		return h.conn.Close()
	}
	return nil
}

func sessionResponse(s Session, message string) *smfpb.CreateSessionResponse {
	return &smfpb.CreateSessionResponse{Success: true, SessionId: s.SessionID, UeIp: s.UEIP, RuleId: s.RuleID, Message: message}
}

func sameGeneration(left, right Session) bool {
	return left.SessionID == right.SessionID && left.Generation == right.Generation
}

// rollbackCreate 只回滚调用方创建的那一代会话。它返回 true 表示确实完成了
// 回滚；false 表示会话已被其他状态转换替换，调用方不能再碰当前记录或 IP 池。
func (h *SMFHandler) rollbackCreate(created Session) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.sessions[created.SessionID]
	if !ok || !sameGeneration(current, created) || current.State != sessionCreating {
		return false
	}
	delete(h.sessions, created.SessionID)
	if h.byUE[created.UEID] == created.SessionID {
		delete(h.byUE, created.UEID)
	}
	h.freeIPs = append(h.freeIPs, created.UEIP)
	return true
}

func (h *SMFHandler) restoreActive(deleting Session) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	current, ok := h.sessions[deleting.SessionID]
	if !ok || !sameGeneration(current, deleting) || current.State != sessionDeleting {
		return false
	}
	current.State = sessionActive
	h.sessions[current.SessionID] = current
	return true
}

// cleanupCreatedRule 尽力清理由一个已经失效的创建调用写入 UPF 的规则。这里使用
// 独立超时，因为原请求的 context 很可能已经取消。
func (h *SMFHandler) cleanupCreatedRule(created Session, ruleID string) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), h.callTimeout)
	defer cancel()
	resp, err := h.upfClient.DeleteRule(cleanupCtx, &upfpb.DeleteRuleRequest{RuleId: ruleID, SessionId: created.SessionID})
	if err != nil {
		log.Printf("清理过期 UPF 规则失败 session_id=%s rule_id=%s err=%v", created.SessionID, ruleID, err)
		return
	}
	if resp == nil || !resp.Success {
		message := "空响应"
		if resp != nil {
			message = resp.Message
		}
		log.Printf("清理过期 UPF 规则未成功 session_id=%s rule_id=%s message=%s", created.SessionID, ruleID, message)
	}
}

func (h *SMFHandler) CreateSession(ctx context.Context, req *smfpb.CreateSessionRequest) (*smfpb.CreateSessionResponse, error) {
	if req.GetUeId() == "" || req.GetAmfUeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "ue_id 和 amf_ue_id 不能为空")
	}
	h.mu.Lock()
	if id, ok := h.byUE[req.UeId]; ok {
		s, exists := h.sessions[id]
		h.mu.Unlock()
		if !exists {
			return nil, status.Error(codes.Internal, "UE 索引指向不存在的会话")
		}
		if s.AMFUEID != req.AmfUeId {
			return nil, status.Error(codes.AlreadyExists, "该 UE 已由另一个 AMF 上下文持有")
		}
		switch s.State {
		case sessionActive:
			return sessionResponse(s, "会话已存在（幂等返回）"), nil
		case sessionCreating:
			return nil, status.Error(codes.Aborted, "相同 UE 的会话正在创建，请稍后重试")
		case sessionDeleting:
			return nil, status.Error(codes.Aborted, "相同 UE 的会话正在删除，请稍后重试")
		default:
			return nil, status.Errorf(codes.FailedPrecondition, "已有会话状态 %q 不允许创建", s.State)
		}
	}
	if len(h.freeIPs) == 0 {
		h.mu.Unlock()
		return &smfpb.CreateSessionResponse{Success: false, Message: "IP地址池已耗尽"}, nil
	}
	ueIP := h.freeIPs[0]
	h.freeIPs = h.freeIPs[1:]
	sessionID := fmt.Sprintf("SESSION-%s", req.UeId)
	h.nextGen++
	s := Session{SessionID: sessionID, UEID: req.UeId, AMFUEID: req.AmfUeId, UEIP: ueIP, DNN: req.Dnn, State: sessionCreating, Generation: h.nextGen}
	h.sessions[sessionID] = s
	h.byUE[req.UeId] = sessionID
	h.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	upfResp, err := h.upfClient.CreateRule(callCtx, &upfpb.CreateRuleRequest{SessionId: sessionID, UeId: req.UeId, UeIp: ueIP, Dnn: req.Dnn})
	if err != nil || upfResp == nil || !upfResp.Success {
		h.rollbackCreate(s)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable, "UPF 创建规则失败: %v", err)
		}
		if upfResp == nil {
			return nil, status.Error(codes.Unavailable, "UPF 创建规则失败: 空响应")
		}
		return &smfpb.CreateSessionResponse{Success: false, Message: upfResp.Message}, nil
	}

	h.mu.Lock()
	current, exists := h.sessions[sessionID]
	if !exists || !sameGeneration(current, s) || current.State != sessionCreating || h.byUE[s.UEID] != s.SessionID {
		h.mu.Unlock()
		h.cleanupCreatedRule(s, upfResp.RuleId)
		return nil, status.Error(codes.Aborted, "会话在创建期间已变化，已清理过期 UPF 规则")
	}
	current.RuleID, current.State = upfResp.RuleId, sessionActive
	h.sessions[sessionID] = current
	h.mu.Unlock()
	log.Printf("会话建立成功 ue_id=%s session_id=%s ue_ip=%s rule_id=%s", current.UEID, current.SessionID, current.UEIP, current.RuleID)
	return sessionResponse(current, "会话和转发规则建立成功"), nil
}

func (h *SMFHandler) DeleteSession(ctx context.Context, req *smfpb.DeleteSessionRequest) (*smfpb.DeleteSessionResponse, error) {
	if req.GetSessionId() == "" || req.GetUeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id 和 ue_id 不能为空")
	}
	h.mu.Lock()
	s, exists := h.sessions[req.SessionId]
	if !exists {
		h.mu.Unlock()
		return &smfpb.DeleteSessionResponse{Success: true, Message: "会话已释放（幂等返回）"}, nil
	}
	if s.UEID != req.UeId {
		h.mu.Unlock()
		return nil, status.Error(codes.PermissionDenied, "ue_id 与会话所有者不匹配")
	}
	if s.State == sessionCreating {
		h.mu.Unlock()
		return nil, status.Error(codes.Aborted, "会话正在创建，暂时不能删除，请稍后重试")
	}
	if s.State == sessionDeleting {
		h.mu.Unlock()
		return nil, status.Error(codes.Aborted, "会话正在删除，请稍后重试")
	}
	if s.State != sessionActive {
		h.mu.Unlock()
		return nil, status.Errorf(codes.FailedPrecondition, "会话状态 %q 不允许删除", s.State)
	}
	s.State = sessionDeleting
	h.sessions[s.SessionID] = s
	h.mu.Unlock()

	callCtx, cancel := context.WithTimeout(ctx, h.callTimeout)
	defer cancel()
	resp, err := h.upfClient.DeleteRule(callCtx, &upfpb.DeleteRuleRequest{RuleId: s.RuleID, SessionId: s.SessionID})
	if err != nil {
		h.restoreActive(s)
		return nil, status.Errorf(codes.Unavailable, "UPF 删除规则失败: %v", err)
	}
	if resp == nil {
		h.restoreActive(s)
		return nil, status.Error(codes.Unavailable, "UPF 删除规则失败: 空响应")
	}
	if !resp.Success {
		h.restoreActive(s)
		return &smfpb.DeleteSessionResponse{Success: false, Message: resp.Message}, nil
	}

	h.mu.Lock()
	current, ok := h.sessions[s.SessionID]
	if !ok || !sameGeneration(current, s) || current.State != sessionDeleting || h.byUE[s.UEID] != s.SessionID {
		h.mu.Unlock()
		return nil, status.Error(codes.Aborted, "会话在删除期间已变化，未修改当前会话")
	}
	delete(h.sessions, s.SessionID)
	delete(h.byUE, s.UEID)
	h.freeIPs = append(h.freeIPs, s.UEIP)
	h.mu.Unlock()
	log.Printf("会话释放成功 session_id=%s ue_ip=%s", s.SessionID, s.UEIP)
	return &smfpb.DeleteSessionResponse{Success: true, Message: "会话、IP和UPF规则已释放"}, nil
}

func (h *SMFHandler) GetSession(_ context.Context, req *smfpb.GetSessionRequest) (*smfpb.GetSessionResponse, error) {
	if req.GetSessionId() == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id 不能为空")
	}
	h.mu.RLock()
	s, exists := h.sessions[req.SessionId]
	h.mu.RUnlock()
	if !exists {
		return &smfpb.GetSessionResponse{Found: false}, nil
	}
	return &smfpb.GetSessionResponse{Found: true, SessionId: s.SessionID, UeId: s.UEID, UeIp: s.UEIP, RuleId: s.RuleID, State: s.State}, nil
}
