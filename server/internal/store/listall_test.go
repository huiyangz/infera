package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestListAllBindings：一次查询带回全部绑定（全局默认 + 所有项目覆盖），
// 全局默认行 ProjectID 为空串——替代逐项目 ListBindings 的 N+1。
func TestListAllBindings(t *testing.T) {
	check := func(t *testing.T, s Store) {
		t.Helper()
		ctx := context.Background()
		a1 := &Agent{Name: "a1", Runner: "local"}
		a2 := &Agent{Name: "a2", Runner: "local"}
		require.NoError(t, s.CreateAgent(ctx, a1))
		require.NoError(t, s.CreateAgent(ctx, a2))
		p := &Project{Name: "p", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
		require.NoError(t, s.CreateProject(ctx, p))

		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{Node: "spec", AgentID: a1.ID}))                  // 默认
		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "spec", AgentID: a2.ID})) // 项目覆盖
		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{Node: "code_gen", AgentID: a2.ID}))              // 默认

		all, err := s.ListAllBindings(ctx)
		require.NoError(t, err)
		require.Len(t, all, 3)
		def, ov := 0, 0
		for _, b := range all {
			switch {
			case b.ProjectID == "" && b.Node == "spec" && b.AgentID == a1.ID:
				def++
			case b.ProjectID == p.ID && b.Node == "spec" && b.AgentID == a2.ID:
				ov++
			case b.ProjectID == "" && b.Node == "code_gen" && b.AgentID == a2.ID:
				def++
			default:
				t.Fatalf("unexpected binding: %+v", b)
			}
		}
		require.Equal(t, 2, def)
		require.Equal(t, 1, ov)
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemory()) })
	t.Run("pg", func(t *testing.T) { check(t, testPool(t)) })
}
