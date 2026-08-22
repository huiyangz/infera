package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBindingsRejectGlobalScope（INFERA-180）：全局默认编排已删除，绑定接口
// 只接受项目级——空 projectID 一律 ErrInvalid，全局绑定无任何读写路径。
func TestBindingsRejectGlobalScope(t *testing.T) {
	check := func(t *testing.T, s Store) {
		t.Helper()
		ctx := context.Background()
		a := &Agent{Name: "cli-1", Runner: "local"}
		require.NoError(t, s.CreateAgent(ctx, a))
		p := &Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
		require.NoError(t, s.CreateProject(ctx, p))

		// 空项目 ID 的读写路径全部拒绝
		_, err := s.ListBindings(ctx, "")
		require.ErrorIs(t, err, ErrInvalid)
		require.ErrorIs(t, s.UpsertBinding(ctx, &PipelineBinding{Node: "spec", AgentID: a.ID}), ErrInvalid)
		require.ErrorIs(t, s.DeleteBinding(ctx, "", "spec"), ErrInvalid)
		require.ErrorIs(t, s.ReplaceBindings(ctx, "", map[string]string{"spec": a.ID}), ErrInvalid)

		// 拒绝路径不落任何绑定行
		all, err := s.ListAllBindings(ctx)
		require.NoError(t, err)
		require.Empty(t, all)
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemory()) })
	t.Run("pg", func(t *testing.T) { check(t, testPool(t)) })
}

// TestBindingsProjectScoped：项目级绑定读写照常（删除全局默认不影响项目编排）。
func TestBindingsProjectScoped(t *testing.T) {
	check := func(t *testing.T, s Store) {
		t.Helper()
		ctx := context.Background()
		a1 := &Agent{Name: "a1", Runner: "local"}
		a2 := &Agent{Name: "a2", Runner: "local"}
		require.NoError(t, s.CreateAgent(ctx, a1))
		require.NoError(t, s.CreateAgent(ctx, a2))
		p := &Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
		require.NoError(t, s.CreateProject(ctx, p))

		require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "spec", AgentID: a1.ID}))
		require.NoError(t, s.ReplaceBindings(ctx, p.ID, map[string]string{"code_gen": a2.ID}))

		// ReplaceBindings 全量替换：旧 spec 行不在
		bs, err := s.ListBindings(ctx, p.ID)
		require.NoError(t, err)
		require.Len(t, bs, 1)
		require.Equal(t, "code_gen", bs[0].Node)

		require.NoError(t, s.DeleteBinding(ctx, p.ID, "code_gen"))
		bs, err = s.ListBindings(ctx, p.ID)
		require.NoError(t, err)
		require.Empty(t, bs)
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemory()) })
	t.Run("pg", func(t *testing.T) { check(t, testPool(t)) })
}
