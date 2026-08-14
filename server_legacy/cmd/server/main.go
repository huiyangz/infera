package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/auth"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/handler"
	"github.com/tokfinity/infera/internal/realtime"
	"github.com/tokfinity/infera/internal/service"
	"github.com/tokfinity/infera/internal/testrunner"
)

func main() {
	cfg := config.Load()

	if cfg.InferaPassword == "" {
		log.Fatal("INFERA_PASSWORD not set; required for login")
	}
	authMgr := auth.New(cfg.InferaPassword)

	pool, err := db.Pool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// 实时事件 Hub（P6）
	hub := realtime.NewHub()
	go hub.Run()

	cloner := github.NewRepoCloner(cfg.GitHubToken)
	projectSvc := service.NewProject(pool, cloner)

	// Agent 执行层 + 测试 runner：需要 Docker + ANTHROPIC_API_KEY；缺则降级 P1 模式。
	var executor *service.ExecuteService
	var testRunner testrunner.Runner
	if dbBackend, err := agent.NewDockerBackend(cfg.AgentImage); err == nil {
		ghClient := github.NewClient(cfg.GitHubToken)
		prSvc := github.NewPRService(ghClient)
		executor = service.NewExecute(pool, dbBackend).WithGitHub(cloner, prSvc, cfg.RepoWorkRoot).WithBroadcaster(hub)
		testRunner = testrunner.NewRealRunner(dbBackend, "")
		fmt.Println("agent backend: docker (full mode)")
	} else {
		fmt.Println("warning: agent backend disabled (P1 mode):", err)
	}

	deliverySvc := service.New(pool).
		WithExecutor(executor).
		WithTestRunner(testRunner).
		WithBroadcaster(hub)

	r := handler.NewRouter(pool, deliverySvc, hub, authMgr, projectSvc)
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
