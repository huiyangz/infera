package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
)

// TestRunUntilGateStopsAtSpecApproval：从 intake 自动跑到 spec_approval gate。
func TestRunUntilGateStopsAtSpecApproval(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "忘记密码"})
	err := svc.RunUntilGate(context.Background(), d.ID)
	assert.NoError(t, err)

	d, _ = svc.Get(context.Background(), d.ID)
	assert.Equal(t, "spec_approval", d.CurrentStage)
	assert.NotNil(t, d.PendingGate)
	assert.Equal(t, "spec_approval", *d.PendingGate)
}
