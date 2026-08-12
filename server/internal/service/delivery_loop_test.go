package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/internal/testrunner"
)

func seedAgentsForLoop(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, _ = pool.Exec(context.Background(), `
		INSERT INTO agent_configs (name, role, config) VALUES
		  ('Spec Agent','spec','{"system_prompt":"x"}'),
		  ('Coder Agent','coder','{"system_prompt":"x"}'),
		  ('Reviewer Agent','reviewer','{"system_prompt":"x"}')
		ON CONFLICT (name) DO NOTHING`)
}

func TestLoopRetriesThenBlocksAtThree(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	seedAgentsForLoop(t, pool)

	// unit_test 永远失败 → 每次回退 code_gen，3 次后 blocked
	runner := testrunner.NewFakeRunner(false)
	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake)).WithTestRunner(runner)

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	// 直接把 stage 设到 code_gen，下一步即 unit_test，聚焦 loop
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='code_gen' WHERE id=$1", d.ID)

	// 第 1 次：code_gen → unit_test(失败) → 回 code_gen, fail_count=1
	d, _ = svc.Advance(context.Background(), d.ID)
	assert.Equal(t, "code_gen", d.CurrentStage)
	assert.Equal(t, int32(1), d.FailCount)
	assert.Equal(t, "active", string(d.Status))

	d, _ = svc.Advance(context.Background(), d.ID) // fail_count=2
	assert.Equal(t, int32(2), d.FailCount)

	d, _ = svc.Advance(context.Background(), d.ID) // 第 3 次 → blocked
	assert.Equal(t, "blocked", string(d.Status))
}

func TestLoopPassesWhenTestsGreen(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	seedAgentsForLoop(t, pool)

	runner := testrunner.NewFakeRunner(true) // 测试通过
	fake := agent.NewFakeBackend()
	fake.Stub(agent.RoleReviewer, `{"decision":"approve","reasons":[]}`)
	svc := New(pool).WithExecutor(NewExecute(pool, fake)).WithTestRunner(runner)

	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='code_gen' WHERE id=$1", d.ID)

	// code_gen → unit_test(pass) → code_review（gate 暂停等人）
	for d.Status == "active" && d.PendingGate == nil {
		var err error
		d, err = svc.Advance(context.Background(), d.ID)
		assert.NoError(t, err)
	}
	// 到 code_review gate：人批准 → 前进到 deploy
	assert.NotNil(t, d.PendingGate)
	assert.Equal(t, "code_review", *d.PendingGate)
	var err error
	d, err = svc.Approve(context.Background(), d.ID)
	assert.NoError(t, err)
	// deploy → completed
	for d.Status == "active" {
		d, err = svc.Advance(context.Background(), d.ID)
		assert.NoError(t, err)
	}
	assert.Equal(t, "completed", string(d.Status))
	assert.Equal(t, int32(0), d.FailCount)
}
