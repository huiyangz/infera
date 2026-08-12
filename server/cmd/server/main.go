package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/handler"
)

func main() {
	cfg := config.Load()

	pool, err := db.Pool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	r := handler.NewRouter(pool)
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
