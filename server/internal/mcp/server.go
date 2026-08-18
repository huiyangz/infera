// Package mcp 提供 MCP（Model Context Protocol）服务端，把 infera 流水线的
// 驾驶面暴露给任意 MCP 客户端（claude / codex 等）：上下文获取（get_context）、
// 本机阶段交回（submit_stage_output，local 绑定节点的交回通道）、门操作
// （get_gate / approve_gate / reject_gate，门禁裁定走引擎 Approve/Reject 单入口）。
//
// 协议实现为无状态 Streamable HTTP：单 POST 端点 + JSON-RPC 2.0、application/json
// 响应、不分配会话（客户端无须回传 Mcp-Session-Id）。不支持 GET SSE 流（405）。
// 不引官方 Go SDK 的理由：本面只需要 initialize/ping/tools 三个 method，
// 仓库依赖刻意保持极小（chi/pgx/uuid），协议面小到手写完全可控且测试可直接覆盖
// 握手 / 版本协商 / 错误语义。
//
// 鉴权用专用静态 token（INFERA_MCP_TOKEN，Bearer），不复用登录 cookie 会话：
// MCP 客户端原生支持静态 Authorization 头、做不了浏览器登录流；专用 token 把
// 「交互登录」与「程序化控流水线」两种凭证解耦，可独立轮换，且未设置时整个
// 端点禁用（503）——不用的部署不暴露攻击面。与 api/auth.go 一致用常数时间比较。
package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/tokfinity/infera/internal/store"
)

// EngineAPI mcp 层对引擎的最小依赖（*engine.Engine 天然满足）。
// 门禁裁定只经 Approve/Reject——与 HTTP API 同一个单入口，无旁路。
type EngineAPI interface {
	Approve(ctx context.Context, deliveryID string, opts store.ApproveOpts) ([]store.Delivery, error)
	Reject(ctx context.Context, deliveryID, reason string) error
	// SubmitLocal 本机绑定节点的交回（写 artifact + advance；门禁审查形态只写预审产物）。
	SubmitLocal(ctx context.Context, deliveryID, output string) error
	// LocalPrompt 只读返回本机停车节点的 (角色名, 角色 prompt)；非本机停车返回空。
	LocalPrompt(ctx context.Context, deliveryID string) (string, string, error)
}

// Server MCP 服务端。挂载由 main 组合（root chi："/"→api.Mux，"/mcp"→Handler）。
type Server struct {
	st      store.Store
	eng     EngineAPI
	workdir func(deliveryID string) string // workspace.Path 注入（get_context 的仓库信息）
	token   string                         // 空串 = 禁用
	drive   func(deliveryID string)        // 簿记后的后台推进（main 注入 api 的 RunDelivery）

	// locks per-delivery 锁：串行化本 MCP 通道发起的簿记（与 api 层同款纪律；
	// 引擎自身无并发保护，跨通道冲突由引擎状态校验兜底报错）。
	locks sync.Map
}

func New(st store.Store, eng EngineAPI, workdir func(string) string, token string) *Server {
	return &Server{st: st, eng: eng, workdir: workdir, token: token}
}

// SetDrive 注入后台推进回调（api.Server.RunDelivery：拿 per-delivery 锁驱动到下一个停车点）。
func (s *Server) SetDrive(fn func(deliveryID string)) { s.drive = fn }

// JSON-RPC 2.0 错误码（协议层错误走 error 对象；工具执行错误走 isError 结果）。
const (
	codeParseError     = -32700
	codeInvalidParams  = -32602
	codeMethodNotFound = -32601
)

// supportedVersions 握手支持的协议版本（回显客户端请求；不认识则回最新支持版）。
var supportedVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

const instructions = `infera 代码交付流水线驾驶面。用法：
1. get_context 查交付全量上下文（需求 / 已有产物 / 仓库与 workdir / 本机停车节点的角色 prompt）；
2. 交付停在本机绑定节点（pending_local 非空）时，在 workdir 完成该阶段工作后用 submit_stage_output 交回产出；
3. 交付挂起人工门禁（pending_gate 非空）时，用 get_gate 查门禁详情，approve_gate / reject_gate 裁定。
门禁选项：spec_approval 可带 complexity=small|large；design_approval 可带 split=[{title,description,wave}]（批准并拆分）；tasks_approval 可带 tasks=[{title,detail}]（批准并覆盖清单）。`

// serverVersion 服务端版本标识（单二进制无版本注入，语义化占位）。
const serverVersion = "0.1.0"

// Handler 返回 /mcp 端点（含禁用态 / 鉴权 / Origin 校验 / 方法路由）。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			writeHTTPError(w, http.StatusServiceUnavailable, "MCP 未启用：未设置 INFERA_MCP_TOKEN")
			return
		}
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeHTTPError(w, http.StatusUnauthorized, "未授权：需要 Bearer INFERA_MCP_TOKEN")
			return
		}
		if !originLocal(r) {
			writeHTTPError(w, http.StatusForbidden, "Origin 不被允许（本机服务仅接受 localhost 来源）")
			return
		}
		switch r.Method {
		case http.MethodPost:
			s.servePost(w, r)
		default:
			// 无状态实现：不提供 GET SSE 流 / DELETE 会话终止。
			w.Header().Set("Allow", http.MethodPost)
			writeHTTPError(w, http.StatusMethodNotAllowed, "仅支持 POST（无状态 MCP，无 SSE 流）")
		}
	})
}

// servePost 处理单个 JSON-RPC 请求（不支持批量——2025-06-18 起协议已移除）。
func (s *Server) servePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeHTTPError(w, http.StatusBadRequest, "读取请求体失败")
		return
	}
	var req struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"` // null/缺失 = notification
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, codeParseError, "请求体不是合法 JSON-RPC 2.0")
		return
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		// 通知：无 id 无响应体（初始化完成通知等），未知通知也静默接受。
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var id any
	_ = json.Unmarshal(req.ID, &id)

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, id, req.Params)
	case "ping":
		writeRPC(w, id, map[string]any{})
	case "tools/list":
		writeRPC(w, id, map[string]any{"tools": toolDefs()})
	case "tools/call":
		s.handleToolsCall(w, id, req.Params, r.Context())
	default:
		writeRPCError(w, id, codeMethodNotFound, "未知 method: "+req.Method)
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, id any, params json.RawMessage) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &p)
	negotiated := supportedVersions[len(supportedVersions)-1]
	for _, v := range supportedVersions {
		if v == p.ProtocolVersion {
			negotiated = v
			break
		}
	}
	writeRPC(w, id, map[string]any{
		"protocolVersion": negotiated,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]string{"name": "infera", "version": serverVersion},
		"instructions":    instructions,
	})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, id any, params json.RawMessage, reqCtx context.Context) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Name == "" {
		writeRPCError(w, id, codeInvalidParams, "tools/call 参数不合法")
		return
	}
	ctx := reqCtx
	var (
		result any
		err    error
	)
	switch p.Name {
	case "get_context":
		result, err = s.toolGetContext(ctx, p.Arguments)
	case "submit_stage_output":
		result, err = s.toolSubmitStageOutput(ctx, p.Arguments)
	case "get_gate":
		result, err = s.toolGetGate(ctx, p.Arguments)
	case "approve_gate":
		result, err = s.toolApproveGate(ctx, p.Arguments)
	case "reject_gate":
		result, err = s.toolRejectGate(ctx, p.Arguments)
	default:
		writeRPCError(w, id, codeInvalidParams, "未知工具: "+p.Name)
		return
	}
	// 参数解码失败是协议错误（-32602）；其余是工具执行错误（isError 结果，文本给客户端可读原因）。
	if err != nil {
		if _, ok := err.(*argError); ok {
			writeRPCError(w, id, codeInvalidParams, err.Error())
			return
		}
		writeToolResult(w, id, err.Error(), true)
		return
	}
	text, merr := json.Marshal(result)
	if merr != nil {
		writeRPCError(w, id, -32603, "结果序列化失败")
		return
	}
	writeToolResult(w, id, string(text), false)
}

// --- 鉴权与来源校验 ---

// authorized 校验 Authorization: Bearer <token>（常数时间比较，防时序侧信道）。
func (s *Server) authorized(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, prefix)), []byte(s.token)) == 1
}

// originLocal 防 DNS rebinding：带 Origin 头的请求只接受本机来源（CLI 客户端通常不带）。
func originLocal(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// --- 簿记与推进（与 api.withGateAction 同款锁纪律） ---

func (s *Server) lockFor(deliveryID string) *sync.Mutex {
	v, _ := s.locks.LoadOrStore(deliveryID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// act 持 per-delivery 锁执行簿记；成功后把锁移交后台推进 goroutine
// （推进可能跑 agent，不能挡响应），失败立即放锁并返回错误。
func (s *Server) act(deliveryID string, bookkeep func() error) error {
	mu := s.lockFor(deliveryID)
	mu.Lock()
	if err := bookkeep(); err != nil {
		mu.Unlock()
		return err
	}
	if s.drive == nil {
		mu.Unlock()
		return nil
	}
	go func() {
		defer mu.Unlock()
		s.drive(deliveryID)
	}()
	return nil
}

// --- HTTP / JSON-RPC 写出 ---

func writeHTTPError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeRPC(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": msg},
	})
}

// writeToolResult 写 tools/call 结果：单 text content，执行错误时 isError=true。
func writeToolResult(w http.ResponseWriter, id any, text string, isErr bool) {
	writeRPC(w, id, map[string]any{
		"content": []any{map[string]string{"type": "text", "text": text}},
		"isError": isErr,
	})
}
