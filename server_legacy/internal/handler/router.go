package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/auth"
	"github.com/tokfinity/infera/internal/realtime"
	"github.com/tokfinity/infera/internal/service"
)

func NewRouter(pool *pgxpool.Pool, svc *service.DeliveryService, hub *realtime.Hub, authMgr *auth.Manager, projectSvc *service.ProjectService) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)
	r.Get("/ws", WS(hub))

	authH := NewAuthHandler(authMgr)
	r.Post("/api/login", authH.Login)
	r.Post("/api/logout", authH.Logout)
	r.Get("/api/me", authH.Me)

	// 受保护路由
	r.Group(func(r chi.Router) {
		r.Use(authMgr.Middleware)
		dh := NewDeliveryHandler(svc, pool)
		ph := NewProjectHandler(projectSvc, pool)
		r.Route("/api/projects", func(r chi.Router) {
			r.Post("/", ph.Create)
			r.Get("/", ph.List)
			r.Get("/{id}", ph.Get)
			r.Patch("/{id}", ph.Update)
			r.Delete("/{id}", ph.Delete)
			r.Post("/{id}/deliveries", dh.Create)
			r.Get("/{id}/deliveries", dh.ListByProject)
		})
		r.Route("/api/deliveries", func(r chi.Router) {
			r.Get("/", dh.List)
			r.Get("/{id}", dh.Get)
			r.Post("/{id}/advance", dh.Advance)
			r.Get("/{id}/gate", dh.Gate)
			r.Post("/{id}/approve", dh.Approve)
			r.Post("/{id}/reject", dh.Reject)
		})
	})
	return r
}
