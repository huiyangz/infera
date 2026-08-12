package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/stage"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryService struct {
	q *generated.Queries
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
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
// 若当前已在 deploy，则标记 completed。
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
	return updated, nil
}
