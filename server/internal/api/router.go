// Package api 提供 infera 的 HTTP 接口：密码登录 + session cookie，
// projects / deliveries（创建触发引擎异步驱动、门禁内容、approve/reject）。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

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
	g      *git.Git // 可选：创建项目时做 LsRemote 可达性校验

	// locks per-delivery 驱动锁（deliveryID → *sync.Mutex）：
	// 引擎无并发保护，创建的异步 driver 与 approve/reject 借此互斥。
	locks sync.Map

	// hub per-delivery websocket 订阅；Server.Publish 供 engine.Notify 注入。
	hub *hub
}

func NewServer(st store.Store, password string, engine EngineAPI) *Server {
	return &Server{st: st, auth: newSessionManager(password), engine: engine, hub: newHub()}
}

func (s *Server) SetEngine(e EngineAPI) { s.engine = e }
func (s *Server) SetGit(g *git.Git)     { s.g = g }

// Mux 装配全部路由。公开：health/login/logout/me/ws；其余需认证。
// /ws 暂挂公开组（MVP：前端带 cookie 连接），后续可加 requireAuth。
func (s *Server) Mux() http.Handler {
	r := chi.NewRouter()

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Post("/api/login", s.handleLogin)
	r.Post("/api/logout", s.handleLogout)
	r.Get("/api/me", s.handleMe)
	r.Get("/ws", s.handleWS)

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/api/projects", s.listProjects)
		r.Post("/api/projects", s.createProject)
		r.Get("/api/projects/{id}", s.getProject)
		r.Patch("/api/projects/{id}", s.patchProject)
		r.Get("/api/projects/{id}/deliveries", s.handleListDeliveries)
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
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
