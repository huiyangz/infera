package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/stage"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryService struct {
	q        *generated.Queries
	executor *ExecuteService // 可为 nil（P1 模式 / 测试）
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

// WithExecutor 注入 Agent 执行层。main 里调。
func (s *DeliveryService) WithExecutor(ex *ExecuteService) *DeliveryService {
	s.executor = ex
	return s
}

type CreateInput struct {
	Title       string
	Description string
	RepoURL     string
	Branch      string
}

// Create 建一条新 Delivery，初始 stage = intake。
func (s *DeliveryService) Create(ctx context.Context, in CreateInput) (generated.Delivery, error) {
	if in.Title == "" {
		return generated.Delivery{}, fmt.Errorf("title is required")
	}
	d, err := s.q.CreateDelivery(ctx, generated.CreateDeliveryParams{
		Title:       in.Title,
		Description: in.Description,
		RepoUrl:     in.RepoURL,
		Branch:      in.Branch,
	})
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("create delivery: %w", err)
	}
	return d, nil
}

// Advance 把 Delivery 推进到下一个 stage，并写一条 timeline event。
// 若当前已在 deploy，则标记 completed。推进到 Agent stage 时立即执行（P2：顺序执行，无 loop）。
func (s *DeliveryService) Advance(ctx context.Context, id pgtype.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("get delivery: %w", err)
	}

	next, ok := stage.Next(d.CurrentStage)
	if !ok {
		// 已在 deploy：完成
		updated, err := s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{
			ID:     d.ID,
			Status: generated.DeliveryStatusCompleted,
		})
		if err != nil {
			return generated.Delivery{}, err
		}
		_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
			DeliveryID: d.ID, Stage: d.CurrentStage, EventType: "delivery_completed", Payload: []byte(`{}`),
		})
		return updated, nil
	}

	updated, err := s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{
		ID: d.ID, CurrentStage: next,
	})
	if err != nil {
		return generated.Delivery{}, err
	}
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: d.ID, Stage: next, EventType: "stage_started", Payload: []byte(`{}`),
	})

	// 若该 stage 由 Agent 负责，立即执行（P2：顺序执行，无 loop；P3 加 loop）
	if s.executor != nil {
		if _, ok := agent.RoleForStage(next); ok {
			prompt := buildPromptForStage(next, d)
			if _, err := s.executor.ExecuteStage(ctx, d.ID, next, prompt); err != nil {
				// 执行失败：记一条失败事件，但不阻断状态推进（P3 会改成回环/卡住）
				payload, _ := json.Marshal(map[string]any{"error": err.Error()})
				_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
					DeliveryID: d.ID, Stage: next, EventType: "agent_failed", Payload: payload,
				})
			}
		}
	}
	return updated, nil
}

// buildPromptForStage 为不同 stage 拼给 Agent 的任务描述。
func buildPromptForStage(stageName string, d generated.Delivery) string {
	switch stageName {
	case "spec":
		return fmt.Sprintf("需求标题：%s\n需求描述：%s\n请产出 spec。", d.Title, d.Description)
	case "test_gen":
		return fmt.Sprintf("根据 spec（见 timeline）为「%s」生成测试用例与单元测试代码。", d.Title)
	case "code_gen":
		return fmt.Sprintf("为「%s」写实现代码，让单元测试通过。", d.Title)
	case "code_review":
		return fmt.Sprintf("审查「%s」的实现代码，产出审核意见。", d.Title)
	default:
		return d.Title
	}
}
