package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/stage"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryService struct {
	q          *generated.Queries
	executor   *ExecuteService    // 可为 nil（P1 模式 / 测试）
	testRunner testrunner.Runner  // 可为 nil
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

// WithExecutor 注入 Agent 执行层。
func (s *DeliveryService) WithExecutor(ex *ExecuteService) *DeliveryService {
	s.executor = ex
	return s
}

// WithTestRunner 注入测试判定器（unit_test stage 用）。
func (s *DeliveryService) WithTestRunner(r testrunner.Runner) *DeliveryService {
	s.testRunner = r
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

// Advance 推进 Delivery：执行 → 判定 → 前进 / 回退 / 升级。
// unit_test 不过或 code_review 被打回 → 回退 code_gen 重调 Coder Agent；
// 连续 3 次失败 → status=blocked。
func (s *DeliveryService) Advance(ctx context.Context, id pgtype.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("get delivery: %w", err)
	}
	if d.Status != generated.DeliveryStatusActive {
		return d, fmt.Errorf("delivery not active (status=%s)", d.Status)
	}

	next, ok := stage.Next(d.CurrentStage)
	if !ok {
		return s.completeDelivery(ctx, d)
	}

	// 推进到 next
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: d.ID, CurrentStage: next})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, next, "stage_started", map[string]any{})

	// 执行（Agent stage）
	switch next {
	case "spec", "test_gen", "code_gen", "code_review":
		if s.executor != nil {
			if _, err := s.executor.ExecuteStage(ctx, d.ID, next, buildPromptForStage(next, d)); err != nil {
				s.timeline(ctx, d.ID, next, "agent_failed", map[string]any{"error": err.Error()})
			}
		}
	}

	// unit_test：系统跑测试判定
	if next == "unit_test" && s.testRunner != nil {
		res, err := s.testRunner.Run(ctx, "/work")
		if err == nil && !res.Pass {
			return s.retryCodeAt(ctx, d, "unit_test", res.Detail)
		}
	}

	// code_review：解析 Reviewer Agent 的 decision
	if next == "code_review" && s.executor != nil {
		if decision, err := s.latestReviewDecision(ctx, d.ID); err == nil && decision.Decision == "reject" {
			return s.retryCodeAt(ctx, d, "code_review", strings.Join(decision.Reasons, "; "))
		}
	}

	// deploy：等最近 PR 合并（若有 PR 且未合并，暂停在 deploy 写 waiting_for_merge）
	if next == "deploy" && s.executor != nil {
		if merged, err := s.executor.IsLatestPRMerged(ctx, d.ID); err == nil && !merged {
			s.timeline(ctx, d.ID, "deploy", "waiting_for_merge", map[string]any{})
			return d, nil
		}
	}

	return d, nil
}

// retryCodeAt 回退到 code_gen 重做；连续 3 次失败则升级 blocked。
func (s *DeliveryService) retryCodeAt(ctx context.Context, d generated.Delivery, failedStage, reason string) (generated.Delivery, error) {
	d, err := s.q.IncrementDeliveryFailCount(ctx, d.ID)
	if err != nil {
		return generated.Delivery{}, err
	}
	if d.FailCount >= 3 {
		d, err = s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{ID: d.ID, Status: generated.DeliveryStatusBlocked})
		if err != nil {
			return generated.Delivery{}, err
		}
		s.timeline(ctx, d.ID, failedStage, "escalated", map[string]any{
			"reason": reason, "fail_count": d.FailCount,
		})
		return d, nil
	}
	// 回退到 code_gen 重做
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: d.ID, CurrentStage: "code_gen"})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, "code_gen", "loop_back", map[string]any{
		"from": failedStage, "reason": reason, "fail_count": d.FailCount,
	})
	if s.executor != nil {
		_, _ = s.executor.ExecuteStage(ctx, d.ID, "code_gen", "上一轮 "+failedStage+" 未过："+reason+"\n请修复。")
	}
	return d, nil
}

func (s *DeliveryService) completeDelivery(ctx context.Context, d generated.Delivery) (generated.Delivery, error) {
	d, err := s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{ID: d.ID, Status: generated.DeliveryStatusCompleted})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, d.ID, d.CurrentStage, "delivery_completed", map[string]any{})
	return d, nil
}

// timeline 是写 timeline event 的简写。
func (s *DeliveryService) timeline(ctx context.Context, deliveryID pgtype.UUID, stageName, eventType string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID, Stage: stageName, EventType: eventType, Payload: b,
	})
}

// latestReviewDecision 从 timeline 取最近一条 code_review 的 agent_output 并解析。
func (s *DeliveryService) latestReviewDecision(ctx context.Context, deliveryID pgtype.UUID) (agent.ReviewDecision, error) {
	events, err := s.q.ListTimelineEvents(ctx, deliveryID)
	if err != nil {
		return agent.ReviewDecision{}, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Stage == "code_review" && e.EventType == "agent_output" {
			var p struct {
				Output string `json:"output"`
			}
			if err := json.Unmarshal(e.Payload, &p); err == nil {
				return agent.ParseReview(p.Output)
			}
		}
	}
	return agent.ReviewDecision{}, fmt.Errorf("no review output found")
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
