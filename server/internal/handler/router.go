package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(pool *pgxpool.Pool) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)

	dh := NewDeliveryHandler(pool)
	r.Route("/api/deliveries", func(r chi.Router) {
		r.Post("/", dh.Create)
		r.Get("/", dh.List)
		r.Get("/{id}", dh.Get)
		r.Post("/{id}/advance", dh.Advance) // 在 Task 11 实现
	})
	return r
}
