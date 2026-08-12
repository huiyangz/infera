package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/pkg/db/generated"
)

// ExecuteService 按 stage 选专职 Agent、调 Backend 执行、把产出写进 timeline。
type ExecuteService struct {
	pool    *pgxpool.Pool
	q       *generated.Queries
	backend agent.Backend
}

func NewExecute(pool *pgxpool.Pool, backend agent.Backend) *ExecuteService {
	return &ExecuteService{pool: pool, q: generated.New(pool), backend: backend}
}

// ExecuteStage 对给定 stage 选对应专职 Agent 执行，并把产出写进 timeline。
// 若该 stage 不需要 Agent（人/系统），返回错误。
func (s *ExecuteService) ExecuteStage(ctx context.Context, deliveryID pgtype.UUID, stage, prompt string) (agent.ExecResult, error) {
	role, ok := agent.RoleForStage(stage)
	if !ok {
		return agent.ExecResult{}, fmt.Errorf("stage %q has no agent", stage)
	}

	// 取 Agent 配置
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

	// 写 timeline
	payload, _ := json.Marshal(map[string]any{
		"agent":      cfg.Name,
		"role":       role,
		"session_id": res.SessionID,
		"output":     res.Output,
	})
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID,
		Stage:      stage,
		EventType:  "agent_output",
		Payload:    payload,
	})
	return res, nil
}

func parseAgentConfig(row generated.AgentConfig) agent.AgentConfig {
	var cfg struct {
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
	}
	_ = json.Unmarshal(row.Config, &cfg)
	idStr := ""
	if row.ID.Valid {
		idStr = uuid.UUID(row.ID.Bytes).String()
	}
	return agent.AgentConfig{
		ID:           idStr,
		Name:         row.Name,
		Role:         agent.Role(row.Role),
		SystemPrompt: cfg.SystemPrompt,
		Model:        cfg.Model,
	}
}
