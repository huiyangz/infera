package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

func main() {
	cfg := config.Load()
	if cfg.Password == "" {
		log.Fatal("INFERA_PASSWORD 未设置")
	}
	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	st := store.NewPg(pool)

	g := git.New()
	g.Token = cfg.GitHubToken

	ws := workspace.New(cfg.RepoWorkRoot, 30*time.Minute) // 终态后保留 30min 供排查

	var ar agent.Runner = agent.NewLocal([]string{"sh", "-c", cfg.AgentCmd + ` "$INFERA_PROMPT"`})
	var tr engine.TestRunner = &testrunner.Local{Script: cfg.TestCmd}
	if os.Getenv("AGENT_BACKEND") == "docker" {
		ar = agent.NewDocker(cfg.AgentImage, []string{cfg.AgentCmd})
		tr = &testrunner.Docker{Image: cfg.AgentImage, Cmd: []string{"sh", "-c", cfg.TestCmd}}
	}

	srv := api.NewServer(st, cfg.Password, nil)
	srv.SetGit(g)

	eng := engine.New(st, ar, ws, tr)
	eng.Notify = srv.Publish
	srv.SetEngine(eng)

	log.Printf("infera listening on %s (workdir root %s)", cfg.Addr, cfg.RepoWorkRoot)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Mux()))
}
