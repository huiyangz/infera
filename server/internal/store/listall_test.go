package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListAllBindings：一次查询带回所有项目的绑定——替代逐项目 ListBindings
// 的 N+1（全局默认已删除，绑定只有项目行）。
func TestListAllBindings(t *testing.T) {
	check := func(t *testing.T, s Store) {
		t.Helper()
		ctx := context.Background()
		a1 := &Agent{Name: "a1", Runner: "local"}
		a2 := &Agent{Name: "a2", Runner: "local"}
		require.NoError(t, s.CreateAgent(ctx, a1))
		require.NoError(t, s.CreateAgent(ctx, a2))
		p1 := &Project{Name: "p1", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
		require.NoError(t, s.CreateProject(ctx, p1))
		p2 := &Project{Name: "p2", RepoURL: "https://github.com/x/z", DefaultBranch: "main"}
		require.NoError(t, s.CreateProject(ctx, p2))

		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p1.ID, Node: "spec", AgentID: a1.ID}))
		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p1.ID, Node: "code_gen", AgentID: a2.ID}))
		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p2.ID, Node: "spec", AgentID: a2.ID}))

		all, err := s.ListAllBindings(ctx)
		require.NoError(t, err)
		require.Len(t, all, 3)
		counts := map[string]int{}
		for _, b := range all {
			switch {
			case b.ProjectID == p1.ID && b.Node == "spec" && b.AgentID == a1.ID:
				counts["p1/spec"]++
			case b.ProjectID == p1.ID && b.Node == "code_gen" && b.AgentID == a2.ID:
				counts["p1/code_gen"]++
			case b.ProjectID == p2.ID && b.Node == "spec" && b.AgentID == a2.ID:
				counts["p2/spec"]++
			default:
				t.Fatalf("unexpected binding: %+v", b)
			}
		}
		require.Equal(t, map[string]int{"p1/spec": 1, "p1/code_gen": 1, "p2/spec": 1}, counts)
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemory()) })
	t.Run("pg", func(t *testing.T) { check(t, testPool(t)) })
}
