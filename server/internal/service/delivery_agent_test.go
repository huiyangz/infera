package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceRunsSpecAgent(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	fake.Stub(agent.RoleSpec, "spec 产出")
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), CreateInput{Title: "忘记密码"})
	_, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(fake.Calls())) // spec 被调一次
	assert.Equal(t, agent.RoleSpec, fake.Calls()[0].Role)
}
