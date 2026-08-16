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
	Approve(ctx context.Context, deliveryID string) error
	Reject(ctx context.Context, deliveryID string, reason string) error
}

type Server struct {
	st   store.Store
	auth *sessionManager

	engine EngineAPI
	g      *git.Git // 可选：创建项目时做 LsRemote 可达性校验

	// locks per-delivery 驱动锁（deliveryID → *sync.Mutex）：
	// 引擎无并发保护，创建的异步 driver 与 approve/reject 借此互斥。
	locks sync.Map
}

func NewServer(st store.Store, password string, engine EngineAPI) *Server {
	return &Server{st: st, auth: newSessionManager(password), engine: engine}
}

func (s *Server) SetEngine(e EngineAPI) { s.engine = e }
func (s *Server) SetGit(g *git.Git)    { s.g = g }

// Mux 装配全部路由。公开：health/login/logout/me；其余需认证。
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
