package main

import (
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
)

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("infera listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
