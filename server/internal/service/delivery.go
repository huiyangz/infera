package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/realtime"
	"github.com/tokfinity/infera/internal/stage"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryService struct {
	q           *generated.Queries
	executor    *ExecuteService        // 可为 nil（P1 模式 / 测试）
	testRunner  testrunner.Runner      // 可为 nil
	broadcaster realtime.Broadcaster   // 可为 nil
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

func (s *DeliveryService) WithExecutor(ex *ExecuteService) *DeliveryService {
	s.executor = ex
	return s
}

func (s *DeliveryService) WithTestRunner(r testrunner.Runner) *DeliveryService {
	s.testRunner = r
	return s
}

func (s *DeliveryService) WithBroadcaster(b realtime.Broadcaster) *DeliveryService {
	s.broadcaster = b
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

// Advance 推进 Delivery：执行 → 判定 → 前进 / 回退 / 升级 / 在 gate 暂停等人。
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

	// 执行（Agent stage；code_review 在 gate 块里跑，不在这）
	switch next {
	case "spec", "test_gen", "code_gen":
		if s.executor != nil {
			if _, err := s.executor.ExecuteStage(ctx, d.ID, next, buildPromptForStage(next, d)); err != nil {
				s.timeline(ctx, d.ID, next, "agent_failed", map[string]any{"error": err.Error()})
			}
		}
	}

	// unit_test：系统跑测试判定（自动 loop，不是 gate）
	if next == "unit_test" && s.testRunner != nil {
		res, err := s.testRunner.Run(ctx, "/work")
		if err == nil && !res.Pass {
			return s.retryCodeAt(ctx, d, "unit_test", res.Detail)
		}
	}

	// gate：执行前置 Agent（code_review 跑 Reviewer 预审），然后暂停等人
	if stage.IsGate(next) {
		if next == "code_review" && s.executor != nil {
			_, _ = s.executor.ExecuteStage(ctx, d.ID, next, buildPromptForStage(next, d))
		}
		d, err = s.q.SetDeliveryPendingGate(ctx, generated.SetDeliveryPendingGateParams{ID: d.ID, PendingGate: pgString(next)})
		if err != nil {
			return generated.Delivery{}, err
		}
		s.timeline(ctx, d.ID, next, "gate_waiting", map[string]any{"gate": next})
		return d, nil // 停下，等人
	}

	// deploy：等最近 PR 合并（若有 PR 且未合并，暂停）
	if next == "deploy" && s.executor != nil {
		if merged, err := s.executor.IsLatestPRMerged(ctx, d.ID); err == nil && !merged {
			s.timeline(ctx, d.ID, "deploy", "waiting_for_merge", map[string]any{})
			return d, nil
		}
	}

	return d, nil
}

// Approve 人批准当前 gate：记 gate 名、清 pending_gate、然后前进。
// 必须在 Clear 前读 PendingGate，否则 Clear 后是 nil。
func (s *DeliveryService) Approve(ctx context.Context, id pgtype.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, err
	}
	gate := ""
	if d.PendingGate != nil {
		gate = *d.PendingGate
	}
	if _, err := s.q.ClearDeliveryPendingGate(ctx, id); err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, id, gate, "gate_approved", map[string]any{})
	return s.Advance(ctx, id)
}

// Reject 人打回当前 gate：清 pending_gate，按 gate 类型回退。
// spec_approval → spec；code_review → code_gen（人打回不计 fail_count 升级）。
func (s *DeliveryService) Reject(ctx context.Context, id pgtype.UUID, reason string) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, err
	}
	gate := ""
	if d.PendingGate != nil {
		gate = *d.PendingGate
	}
	if _, err := s.q.ClearDeliveryPendingGate(ctx, id); err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, id, gate, "gate_rejected", map[string]any{"reason": reason})

	target := "code_gen"
	if gate == "spec_approval" {
		target = "spec"
	}
	d, err = s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{ID: id, CurrentStage: target})
	if err != nil {
		return generated.Delivery{}, err
	}
	s.timeline(ctx, id, target, "loop_back", map[string]any{"from": gate, "reason": reason})
	if s.executor != nil {
		_, _ = s.executor.ExecuteStage(ctx, id, target, "人打回："+reason+"\n请重做。")
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

// timeline 是写 timeline event 的简写，写完后广播（P6 实时）。
func (s *DeliveryService) timeline(ctx context.Context, deliveryID pgtype.UUID, stageName, eventType string, payload map[string]any) {
	b, _ := json.Marshal(payload)
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID, Stage: stageName, EventType: eventType, Payload: b,
	})
	s.broadcast(deliveryID, stageName, eventType)
}

// broadcast 把事件推给前端（若有 broadcaster）。pgtype.UUID → google uuid 边界转换。
func (s *DeliveryService) broadcast(deliveryID pgtype.UUID, stageName, eventType string) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(uuid.UUID(deliveryID.Bytes), realtime.Event{Type: eventType, Stage: stageName})
	}
}

// pgString 把 stage 名转 *string（sqlc 对 nullable text 生成 *string）。
func pgString(s string) *string { return &s }

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
