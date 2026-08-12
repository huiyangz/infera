package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceMovesToNextStage(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(pool)
	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})

	advanced, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "spec", advanced.CurrentStage)
}

func TestAdvanceFromDeployCompletes(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(pool)
	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	// 手动把 stage 推到 deploy，模拟已经走到最后一步前
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='deploy' WHERE id=$1", d.ID)

	advanced, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "completed", string(advanced.Status))
}
