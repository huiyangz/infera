package handler

import "github.com/go-chi/chi/v5"

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)
	return r
}
