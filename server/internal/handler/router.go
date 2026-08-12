package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/service"
)

func NewRouter(pool *pgxpool.Pool, svc *service.DeliveryService) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)

	dh := NewDeliveryHandler(svc, pool)
	r.Route("/api/deliveries", func(r chi.Router) {
		r.Post("/", dh.Create)
		r.Get("/", dh.List)
		r.Get("/{id}", dh.Get)
		r.Post("/{id}/advance", dh.Advance)
		r.Get("/{id}/gate", dh.Gate)
		r.Post("/{id}/approve", dh.Approve)
		r.Post("/{id}/reject", dh.Reject)
	})
	return r
}
