// Package api 提供 infera 的 HTTP 接口：密码登录 + session cookie，
// projects / deliveries（创建触发引擎异步驱动、门禁内容、approve/reject）。
package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/deliverylock"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
)

// EngineAPI 是 api 层对引擎的最小依赖（*engine.Engine 天然满足）。
// NewServer 允许 nil（测试/分期装配），SetEngine 在 main 里后置注入。
type EngineAPI interface {
	Start(ctx context.Context, deliveryID string) error
	Continue(ctx context.Context, deliveryID string) error
	// Approve 门禁批准唯一入口：opts 按当前门校验——spec_approval 裁定 Complexity、
	// design_approval 非空 Split=「批准并拆分」（返回创建的子需求）。
	Approve(ctx context.Context, deliveryID string, opts store.ApproveOpts) ([]store.Delivery, error)
	Reject(ctx context.Context, deliveryID string, reason string) error
	// ResumeMerge 拆分父冲突恢复（fetch 人工分支 reset 后重跑合并队列）。
	ResumeMerge(ctx context.Context, deliveryID string) error
	// MaybeDriveParent 拆分父的合并/批次调度推进入口（重启恢复对停在 code_gen 的父调用）。
	MaybeDriveParent(ctx context.Context, parentID string) error
}

type Server struct {
	st   store.Store
	auth *sessionManager

	engine EngineAPI
	req    RequirementsAPI // 需求编排服务（可选：未装配 → 需求路由 503）
	g      *git.Git        // 可选：创建项目时做 LsRemote 可达性校验

	// multicaSync 同步服务（可选：未装配 → 同步路由 503）。凭据在装配方
	// （main）从 env 构造 client 后注入，Server 不持有凭据。
	multicaSync MulticaSyncAPI

	// cookieSecure session cookie 的 Secure 属性：HTTPS 终端开启（防明文泄露），
	// 本地 http 开发保持关闭（否则浏览器丢弃 cookie）。main 按 env 装配。
	cookieSecure bool

	// locks per-delivery 驱动锁（deliveryID → *sync.Mutex）：
	// 引擎无并发保护，创建的异步 driver 与 approve/reject 借此互斥。
	// 与 MCP 面共享同一份（main 经 DeliveryLocks 注入 mcp）——两条驾驶面
	// 并发进引擎会双写 UpdateDelivery / 双 advance / 事件乱序。
	locks *deliverylock.Locks

	// hub per-delivery websocket 订阅；Server.Publish 供 engine.Notify 注入。
	hub *hub
}

func NewServer(st store.Store, password string, engine EngineAPI) *Server {
	return &Server{st: st, auth: newSessionManager(password), engine: engine, hub: newHub(), locks: deliverylock.New()}
}

func (s *Server) SetEngine(e EngineAPI) { s.engine = e }
func (s *Server) SetGit(g *git.Git)     { s.g = g }

// DeliveryLocks 导出 per-delivery 锁注册表：main 把同一份注入 MCP 面
// （mcp.Server.SetLocks），两条驾驶面对引擎的操作借此互斥。
func (s *Server) DeliveryLocks() *deliverylock.Locks { return s.locks }

// SetCookieSecure 开启 session cookie 的 Secure 属性（HTTPS 部署时装配）。
func (s *Server) SetCookieSecure(v bool) *Server {
	s.cookieSecure = v
	return s
}

// Mux 装配全部路由。公开：health/login/logout/me；其余需认证。
// /ws 挂认证组 + Origin 校验 + delivery 存在性校验（事件流是登录面内容，
// 不得让未认证方订阅任意 delivery）。
func (s *Server) Mux() http.Handler {
	r := chi.NewRouter()

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/api/login", s.handleLogin)
	r.Post("/api/logout", s.handleLogout)
	r.Get("/api/me", s.handleMe)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/ws", s.handleWS)
		r.Get("/api/projects", s.listProjects)
		r.Post("/api/projects", s.createProject)
		r.Get("/api/projects/{id}", s.getProject)
		r.Patch("/api/projects/{id}", s.patchProject)
		// 项目维度只读视图（INFERA-108 T01）：需求统计 + 跨项目待决策列表。
		r.Get("/api/projects/{id}/stats", s.handleProjectStats)
		r.Get("/api/pending-decisions", s.handlePendingDecisions)
		r.Get("/api/projects/{id}/deliveries", s.handleListDeliveries)
		// 项目任务分组视图（L202608221704-1-T01 冻结契约）：父任务 + 子任务按阶段分组。
		r.Get("/api/projects/{id}/task-groups", s.handleProjectTaskGroups)
		r.Post("/api/projects/{id}/deliveries", s.handleCreateDelivery)
		r.Get("/api/deliveries/{id}", s.handleGetDelivery)
		r.Get("/api/deliveries/{id}/gate", s.handleGate)
		r.Post("/api/deliveries/{id}/approve", s.handleApprove)
		r.Post("/api/deliveries/{id}/reject", s.handleReject)
		r.Post("/api/deliveries/{id}/merge/resume", s.handleMergeResume)
		r.Get("/api/agents", s.listAgents)
		r.Post("/api/agents", s.createAgent)
		r.Patch("/api/agents/{id}", s.patchAgent)
		r.Delete("/api/agents/{id}", s.deleteAgent)
		r.Get("/api/pipeline", s.getPipeline)
		r.Put("/api/pipeline", s.putPipeline)
		r.Get("/api/projects/{id}/pipeline", s.getProjectPipeline)
		r.Put("/api/projects/{id}/pipeline", s.putProjectPipeline)
		// 需求流转面（INFERA-11 T05）
		r.Post("/api/requirements", s.handleCreateRequirement)
		r.Get("/api/requirements", s.handleListRequirements)
		r.Get("/api/requirements/{id}", s.handleGetRequirement)
		r.Post("/api/requirements/{id}/cards/{cardID}/approve", s.handleCardApprove)
		r.Post("/api/requirements/{id}/cards/{cardID}/reject", s.handleCardReject)
		r.Post("/api/requirements/{id}/cards/{cardID}/decide", s.handleCardDecide)
		r.Post("/api/requirements/{id}/cards/{cardID}/merge", s.handleCardMerge)
		r.Post("/api/requirements/{id}/cards/{cardID}/rework", s.handleCardRework)
		r.Get("/api/requirements/{id}/audit", s.handleRequirementAudit)
		// PR 评审只读面（T09 加法扩展）：行级评审评论 + diff 概要
		r.Get("/api/requirements/{id}/pr-review", s.handleRequirementPRReview)
		r.Get("/api/projects/{id}/merge-policy", s.handleGetMergePolicy)
		r.Put("/api/projects/{id}/merge-policy", s.handleSetMergePolicy)
		// Multica 同步面（INFERA-80 T03）：POST 触发全量同步 / GET 最近一次结果
		r.Post("/api/multica/sync", s.handleMulticaSyncTrigger)
		r.Get("/api/multica/sync", s.handleMulticaSyncStatus)
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError 统一错误响应：{"error": 人类可读文案, "code": 机器可读码}。
// code 由状态码派生，客户端按 code 分支而不解析文案（文案可能随措辞调整）。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": errorCode(status)})
}

// errorCode 状态码 → 稳定错误码（新增取值需全端同步）。
func errorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusBadGateway:
		return "bad_gateway"
	default:
		return "internal"
	}
}
