package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/mcp"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/persist"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
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

	// git 实例只建一次（带 GITHUB_TOKEN）：LsRemote 校验、clone、push 共用同一 token 注入。
	g := git.New()
	g.Token = cfg.GitHubToken

	ws := workspace.New(cfg.RepoWorkRoot, g, 30*time.Minute) // 终态后保留 30min 供排查

	var ar agent.Runner = agent.NewLocal([]string{"sh", "-c", cfg.AgentCmd + ` "$INFERA_PROMPT"`})
	var tr engine.TestRunner = &testrunner.Local{Script: cfg.TestCmd}
	if os.Getenv("AGENT_BACKEND") == "docker" {
		ar = agent.NewDocker(cfg.AgentImage, []string{cfg.AgentCmd})
		tr = &testrunner.Docker{Image: cfg.AgentImage, Cmd: []string{"sh", "-c", cfg.TestCmd}}
	}

	// SetGit 必须在建第一个 delivery 前就位：createProject 的 LsRemote 可达性校验依赖它。
	srv := api.NewServer(st, cfg.Password, nil)
	srv.SetGit(g)
	// HTTPS 终端部署时开启 cookie Secure（本地 http 开发保持关闭）。
	srv.SetCookieSecure(os.Getenv("INFERA_COOKIE_SECURE") == "true")

	// 默认编排种子：无默认绑定时注册 default-cli / local-console 并绑定全部节点。
	// spec 默认走 default-cli（E2E 需要可跑通）；SEED_LOCAL_SPEC=true 切到本机交互占位。
	// 失败不致命：Resolver 未就绪时引擎回退单 runner 旧路径，启动永不因此中断。
	if err := seedDefaultOrchestration(context.Background(), st, cfg.AgentCmd); err != nil {
		log.Printf("seed orchestration: %v（引擎回退单 runner 模式）", err)
	}

	// 启动期清扫孤儿 workdir（须在 ResumeActive 前：恢复点火会重新 Acquire 在途交付）。
	sweepOrphanWorkdirs(context.Background(), st, ws)

	// 固化：code_review 门禁到达时 commit（绿地）/ push + PR（绑库，github.com），
	// 复用带 token 的 git 实例；PR 创建用同一 token。
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(g, cfg.GitHubToken))
	// 节点执行器按项目编排绑定解析（项目覆盖 ?? 全局默认）；基准节点解析失败按
	// 绑定缺失 blocked；可选节点（design/tasks，旧默认绑定不覆盖）缺绑定回退
	// 构造时的 ar 兜底（R11：真 agent 可绑定，旧配置不 blocked）。
	eng.ResolveRunner = func(ctx context.Context, projectID, node string) (agent.Runner, error) {
		agents, _, err := orchestration.Resolve(ctx, st, projectID)
		if err != nil {
			return nil, err
		}
		a, ok := agents[node]
		if !ok {
			if slices.Contains(orchestration.RequiredNodes, node) {
				return nil, &orchestration.ErrIncompleteBindings{Missing: []string{node}}
			}
			return nil, nil // 可选节点未绑定：引擎回退构造时的 ar
		}
		return orchestration.RunnerFor(a)
	}
	eng.Notify = srv.Publish
	// 拆分子需求的批次点火：engine 不能反向 import api，经闭包转接 RunDelivery
	// （拿 per-delivery 锁后驱动到稳定）。
	eng.OnStartDelivery = func(id string) { go srv.RunDelivery(id) }
	srv.SetEngine(eng)

	// 重启恢复：重启前仍 active 的交付重新点火后台驱动
	// （gate-parked 零引擎调用、中断的从 CurrentStage 继续）。
	srv.ResumeActive(context.Background())

	// MCP 服务（R3）：把驾驶面（上下文 / 本机交回 / 门操作）暴露给任意 MCP 客户端。
	// 挂在 root 路由的 /mcp（api.Mux 原样挂在 /）；未设置 INFERA_MCP_TOKEN 时端点禁用。
	// 簿记后的推进复用 api 的 per-delivery 锁驱动（RunDelivery），与 HTTP 面同一条推进路径。
	mcpSrv := mcp.New(st, eng, ws.Path, cfg.MCPToken)
	mcpSrv.SetDrive(srv.RunDelivery)
	root := chi.NewRouter()
	root.Mount("/", srv.Mux())
	root.Mount("/mcp", mcpSrv.Handler())
	if cfg.MCPToken != "" {
		log.Printf("mcp: 已启用（/mcp，Bearer token 鉴权）")
	}

	log.Printf("infera listening on %s (workdir root %s)", cfg.Addr, cfg.RepoWorkRoot)
	log.Fatal(http.ListenAndServe(cfg.Addr, root))
}

// seedDefaultOrchestration 幂等种子：无默认绑定时注册 default-cli（command 取
// AGENT_CMD）与 local-console（本机交互占位），并绑定全部可绑定节点。
// spec 默认走 default-cli（流程可跑通）；SEED_LOCAL_SPEC=true 切到 local-console。
// 已有默认绑定时不动（用户可能改过）。
func seedDefaultOrchestration(ctx context.Context, st store.Store, agentCmd string) error {
	existing, err := st.ListBindings(ctx, "")
	if err != nil || len(existing) > 0 {
		return err
	}
	cli := &store.Agent{
		Name:   "default-cli",
		Runner: "cli",
		Config: map[string]any{"command": []any{"sh", "-c", agentCmd + ` "$INFERA_PROMPT"`}},
	}
	if err := st.CreateAgent(ctx, cli); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	local := &store.Agent{Name: "local-console", Runner: "local", Config: map[string]any{}}
	if err := st.CreateAgent(ctx, local); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	// 重名（半种子状态）时回读拿 id。读失败必须上抛：吞错会继续用空
	// agent id 绑定出坏数据（误导性错误 + 半种子状态）。
	agents, err := st.ListAgents(ctx)
	if err != nil {
		return err
	}
	for _, a := range agents {
		switch a.Name {
		case "default-cli":
			cli.ID = a.ID
		case "local-console":
			local.ID = a.ID
		}
	}
	specAgent, specName := cli.ID, "default-cli"
	if os.Getenv("SEED_LOCAL_SPEC") == "true" {
		specAgent, specName = local.ID, "local-console"
	}
	for _, node := range orchestration.BindableNodes {
		id := cli.ID
		if node == "spec" {
			id = specAgent
		}
		if err := st.UpsertBinding(ctx, &store.PipelineBinding{Node: node, AgentID: id}); err != nil {
			return err
		}
	}
	log.Printf("seed: 默认编排就绪（spec→%s，test_gen/code_gen/code_review/双道审查→default-cli）", specName)
	return nil
}

// sweepOrphanWorkdirs 启动期回收孤儿 workdir：workspace 注册表只在内存，
// 上次进程的延迟清理计时器随进程消失。keep = 全部未完成交付（active/queued/
// blocked——blocked 可能是 persist 失败保留的救援现场，绝不能扫掉）。
// 读库失败只记日志不阻断启动。
func sweepOrphanWorkdirs(ctx context.Context, st store.Store, ws *workspace.Manager) {
	keep := map[string]bool{}
	keepAll := func(ds []store.Delivery) {
		for _, d := range ds {
			if d.Status != "completed" {
				keep[d.ID] = true
			}
		}
	}
	active, err := st.ListActiveDeliveries(ctx)
	if err != nil {
		log.Printf("workspace sweep: %v（跳过，不影响启动）", err)
		return
	}
	keepAll(active)
	projs, err := st.ListProjects(ctx)
	if err != nil {
		log.Printf("workspace sweep: %v（跳过，不影响启动）", err)
		return
	}
	for _, p := range projs {
		ds, err := st.ListProjectDeliveries(ctx, p.ID)
		if err != nil {
			log.Printf("workspace sweep: %v（跳过，不影响启动）", err)
			return
		}
		keepAll(ds)
	}
	ws.Sweep(func(id string) bool { return keep[id] })
}
