package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/handler"
	"github.com/tokfinity/infera/internal/service"
)

func main() {
	cfg := config.Load()

	pool, err := db.Pool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// 装配 Agent 执行层：DockerBackend（生产）。无 ANTHROPIC_API_KEY / Docker 时降级为 nil（P1 模式）。
	var executor *service.ExecuteService
	if dbBackend, err := agent.NewDockerBackend("infera-agent"); err == nil {
		executor = service.NewExecute(pool, dbBackend)
	} else {
		fmt.Println("warning: agent backend disabled:", err)
	}
	deliverySvc := service.New(pool).WithExecutor(executor)

	r := handler.NewRouter(pool, deliverySvc)
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
