package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedAgentBindings 建一个项目 + 两个 agent（内存/pg 共用断言）。
func seedAgentBindings(t *testing.T, s Store) (*Project, Agent, Agent) {
	t.Helper()
	ctx := context.Background()
	p := &Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, s.CreateProject(ctx, p))
	a1 := &Agent{Name: "default-cli", Runner: "cli", Config: map[string]any{"command": []any{"sh", "-c", "echo hi"}}}
	require.NoError(t, s.CreateAgent(ctx, a1))
	a2 := &Agent{Name: "local-console", Runner: "local"}
	require.NoError(t, s.CreateAgent(ctx, a2))
	return p, *a1, *a2
}

func checkAgentBindings(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	p, a1, a2 := seedAgentBindings(t, s)

	// name 唯一冲突
	err := s.CreateAgent(ctx, &Agent{Name: "default-cli", Runner: "cli"})
	require.ErrorIs(t, err, ErrConflict)

	// Get / List / Update
	got, err := s.GetAgent(ctx, a1.ID)
	require.NoError(t, err)
	require.Equal(t, "cli", got.Runner)
	agents, err := s.ListAgents(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 2)
	got.Runner = "http"
	got.Config = map[string]any{"url": "http://localhost:9"}
	require.NoError(t, s.UpdateAgent(ctx, got))
	got, _ = s.GetAgent(ctx, a1.ID)
	require.Equal(t, "http", got.Runner)

	// 项目绑定 upsert 幂等
	require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "spec", AgentID: a1.ID}))
	require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "spec", AgentID: a2.ID}))
	ovs, err := s.ListBindings(ctx, p.ID)
	require.NoError(t, err)
	require.Len(t, ovs, 1)
	require.Equal(t, a2.ID, ovs[0].AgentID)
	require.Equal(t, p.ID, ovs[0].ProjectID)

	// 指向不存在 agent → ErrNotFound
	require.ErrorIs(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "code_gen", AgentID: "00000000-0000-0000-0000-000000000000"}), ErrNotFound)

	// 删除仍被引用的 agent → ErrConflict
	require.ErrorIs(t, s.DeleteAgent(ctx, a2.ID), ErrConflict)
	// 解绑后可删
	require.NoError(t, s.DeleteBinding(ctx, p.ID, "spec"))
	require.NoError(t, s.DeleteAgent(ctx, a2.ID))
	// 删除不存在的绑定/agent → ErrNotFound
	require.ErrorIs(t, s.DeleteBinding(ctx, p.ID, "spec"), ErrNotFound)
	require.ErrorIs(t, s.DeleteAgent(ctx, a2.ID), ErrNotFound)

	// 更新不存在的 agent / 重名冲突
	require.ErrorIs(t, s.UpdateAgent(ctx, &Agent{ID: "00000000-0000-0000-0000-000000000000", Name: "x", Runner: "cli"}), ErrNotFound)
	a3 := &Agent{Name: "third", Runner: "docker", Config: map[string]any{"image": "infera-agent"}}
	require.NoError(t, s.CreateAgent(ctx, a3))
	a3.Name = "default-cli"
	require.ErrorIs(t, s.UpdateAgent(ctx, a3), ErrConflict)
}

func mustList(s Store, ctx context.Context, projectID string) []PipelineBinding {
	bs, err := s.ListBindings(ctx, projectID)
	if err != nil {
		panic(err)
	}
	return bs
}

func TestMemoryAgentBindings(t *testing.T) {
	checkAgentBindings(t, NewMemory())
}

func TestPgAgentBindings(t *testing.T) {
	p := testPool(t)
	checkAgentBindings(t, p)
}
