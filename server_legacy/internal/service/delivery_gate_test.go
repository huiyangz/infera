package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceToCodeReviewPauses(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Reviewer Agent','reviewer','{"system_prompt":"x"}')`)
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='unit_test' WHERE id=$1", d.ID)

	// unit_test → code_review（gate）：应暂停，pending_gate=code_review，不进 deploy
	d, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "code_review", d.CurrentStage)
	assert.NotNil(t, d.PendingGate)
	assert.Equal(t, "code_review", *d.PendingGate)
}

func TestApproveAdvances(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='code_review', pending_gate='code_review' WHERE id=$1", d.ID)

	d, err := svc.Approve(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Nil(t, d.PendingGate)
	assert.Equal(t, "completed", string(d.Status)) // code_review 是终点，批准即完成
}

func TestRejectCodeReviewBacksToCodeGen(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='code_review', pending_gate='code_review' WHERE id=$1", d.ID)

	d, err := svc.Reject(context.Background(), d.ID, "边界 case 没覆盖")
	assert.NoError(t, err)
	assert.Equal(t, "code_gen", d.CurrentStage)
	assert.Nil(t, d.PendingGate)
}

func TestRejectSpecApprovalBacksToSpec(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(),
		"UPDATE deliveries SET current_stage='spec_approval', pending_gate='spec_approval' WHERE id=$1", d.ID)

	d, _ = svc.Reject(context.Background(), d.ID, "验收标准不清")
	assert.Equal(t, "spec", d.CurrentStage)
}
