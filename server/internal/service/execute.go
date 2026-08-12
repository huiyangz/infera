package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/github"
	"github.com/tokfinity/infera/internal/realtime"
	"github.com/tokfinity/infera/pkg/db/generated"
)

// ExecuteService 按 stage 选专职 Agent、调 Backend 执行、把产出写进 timeline。
// 可选注入 GitHub 仓库上下文（P4）：code_gen 后 push 分支 + 开 PR；deploy 等合并。
type ExecuteService struct {
	pool     *pgxpool.Pool
	q        *generated.Queries
	backend  agent.Backend
	cloner   github.RepoCloner // 零值=未注入（无仓库模式）
	git      github.GitService // 同上
	pr       *github.PRService // nil=未注入
	repoRoot string            // 本地 clone 根目录

	broadcaster realtime.Broadcaster // 可为 nil（P6 实时）
}

func NewExecute(pool *pgxpool.Pool, backend agent.Backend) *ExecuteService {
	return &ExecuteService{pool: pool, q: generated.New(pool), backend: backend}
}

// WithBroadcaster 注入实时广播器。
func (s *ExecuteService) WithBroadcaster(b realtime.Broadcaster) *ExecuteService {
	s.broadcaster = b
	return s
}

func (s *ExecuteService) broadcast(deliveryID pgtype.UUID, stageName, eventType string) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(uuid.UUID(deliveryID.Bytes), realtime.Event{Type: eventType, Stage: stageName})
	}
}

// WithGitHub 注入仓库上下文（clone/commit/PR）。不调用则走"无仓库模式"（P2/P3）。
func (s *ExecuteService) WithGitHub(cloner github.RepoCloner, pr *github.PRService, repoRoot string) *ExecuteService {
	s.cloner = cloner
	s.git = github.GitService{}
	s.pr = pr
	s.repoRoot = repoRoot
	return s
}

// ExecuteStage 对给定 stage 选对应专职 Agent 执行，并把产出写进 timeline。
// code_gen 后若有仓库上下文，再 push 分支 + 开 PR。stage 无 Agent 则返回错误。
func (s *ExecuteService) ExecuteStage(ctx context.Context, deliveryID pgtype.UUID, stage, prompt string) (agent.ExecResult, error) {
	role, ok := agent.RoleForStage(stage)
	if !ok {
		return agent.ExecResult{}, fmt.Errorf("stage %q has no agent", stage)
	}

	cfgRow, err := s.q.GetAgentByRole(ctx, generated.AgentRole(role))
	if err != nil {
		return agent.ExecResult{}, fmt.Errorf("load agent %s: %w", role, err)
	}
	cfg := parseAgentConfig(cfgRow)

	res, err := s.backend.Execute(ctx, agent.ExecInput{
		Role:   role,
		Prompt: fmt.Sprintf("%s\n\n# 本次任务\n%s", cfg.SystemPrompt, prompt),
	})
	if err != nil {
		return res, fmt.Errorf("execute agent %s: %w", role, err)
	}

	payload, _ := json.Marshal(map[string]any{
		"agent": cfg.Name, "role": role, "session_id": res.SessionID, "output": res.Output,
	})
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID, Stage: stage, EventType: "agent_output", Payload: payload,
	})
	s.broadcast(deliveryID, stage, "agent_output")

	// code_gen 后：push 分支 + 开 PR（若有仓库上下文）
	// 注意（P4 已知局限）：这里 Agent 在容器空 /work 跑，产出是文本；要改真文件需把
	// clone 目录传进 ExecInput.Workdir（P5+ 再补）。当前 PR 会提交空改动。
	if stage == "code_gen" {
		if d, err := s.q.GetDelivery(ctx, deliveryID); err == nil {
			branch := "infera/" + pgUUIDString(deliveryID)
			if err := s.maybePushAndOpenPR(ctx, d, branch); err != nil {
				ep, _ := json.Marshal(map[string]any{"error": err.Error()})
				_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
					DeliveryID: deliveryID, Stage: stage, EventType: "pr_failed", Payload: ep,
				})
				s.broadcast(deliveryID, stage, "pr_failed")
			}
		}
	}
	return res, nil
}

// maybePushAndOpenPR 克隆仓库、提交改动、推分支、开 PR，记 pr_opened。
// 无仓库上下文（pr==nil）或无 repo_url 时跳过。
func (s *ExecuteService) maybePushAndOpenPR(ctx context.Context, d generated.Delivery, branch string) error {
	if s.pr == nil || d.RepoUrl == "" {
		return nil
	}
	_ = os.MkdirAll(s.repoRoot, 0o755)
	workdir := filepath.Join(s.repoRoot, pgUUIDString(d.ID))
	if _, err := os.Stat(workdir); os.IsNotExist(err) {
		if err := s.cloner.Clone(ctx, d.RepoUrl, workdir); err != nil {
			return err
		}
	}
	if err := s.git.WithWorkdir(workdir).CommitAndPush(ctx, branch, "infera: "+d.Title); err != nil {
		return err
	}
	pr, err := s.pr.Create(ctx, repoOwnerRepo(d.RepoUrl), branch, "main",
		"["+d.Title+"] by Coder Agent", "由 infera Coder Agent 自动生成")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"url": pr.GetHTMLURL(), "number": pr.GetNumber(),
	})
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: d.ID, Stage: "code_gen", EventType: "pr_opened", Payload: payload,
	})
	s.broadcast(d.ID, "code_gen", "pr_opened")
	return nil
}

// IsLatestPRMerged 取 timeline 最近一条 pr_opened 的 PR 号，查是否已合并。
func (s *ExecuteService) IsLatestPRMerged(ctx context.Context, deliveryID pgtype.UUID) (bool, error) {
	if s.pr == nil {
		return false, fmt.Errorf("no pr service")
	}
	events, err := s.q.ListTimelineEvents(ctx, deliveryID)
	if err != nil {
		return false, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.EventType == "pr_opened" {
			var p struct {
				URL    string `json:"url"`
				Number int    `json:"number"`
			}
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				return s.pr.IsMerged(ctx, repoOwnerRepo(p.URL), p.Number)
			}
		}
	}
	return false, fmt.Errorf("no pr_opened event")
}

func parseAgentConfig(row generated.AgentConfig) agent.AgentConfig {
	var cfg struct {
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
	}
	_ = json.Unmarshal(row.Config, &cfg)
	return agent.AgentConfig{
		ID:           pgUUIDString(row.ID),
		Name:         row.Name,
		Role:         agent.Role(row.Role),
		SystemPrompt: cfg.SystemPrompt,
		Model:        cfg.Model,
	}
}

// pgUUIDString 把 pgtype.UUID 转成规范字符串（Valid=false 时空串）。
func pgUUIDString(id pgtype.UUID) string {
	if id.Valid {
		return uuid.UUID(id.Bytes).String()
	}
	return ""
}

// repoOwnerRepo 从 "owner/repo" 或 GitHub URL（含 pull 路径）解析出 "owner/repo"。
func repoOwnerRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".git")
	if i := strings.Index(s, "github.com/"); i >= 0 {
		s = s[i+len("github.com/"):]
	}
	parts := strings.SplitN(s, "/", 3)
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return s
}
