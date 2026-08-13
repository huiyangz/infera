package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/pkg/db/generated"
)

func TestExecuteRunsAgentAndWritesTimeline(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")

	// 手动 seed 一个 spec agent（覆盖 migration 之外的隔离测试）
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	fake.Stub(agent.RoleSpec, "## spec\n忘记密码流程…")
	svc := NewExecute(pool, fake)

	d, err := New(pool).Create(context.Background(), pid, CreateInput{Title: "忘记密码"})
	assert.NoError(t, err)

	res, err := svc.ExecuteStage(context.Background(), d.ID, "spec", "需求：忘记密码")
	assert.NoError(t, err)
	assert.Equal(t, "## spec\n忘记密码流程…", res.Output)

	// timeline 应有一条 agent_output 事件
	q := generated.New(pool)
	events, err := q.ListTimelineEvents(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.True(t, len(events) >= 1)
	assert.Equal(t, "agent_output", events[len(events)-1].EventType)
}

func TestExecuteSkipsForHumanSystemStage(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p1")

	fake := agent.NewFakeBackend()
	svc := NewExecute(pool, fake)

	d, _ := New(pool).Create(context.Background(), pid, CreateInput{Title: "t"})

	_, err := svc.ExecuteStage(context.Background(), d.ID, "intake", "")
	assert.Error(t, err) // intake 不该调 agent
}
