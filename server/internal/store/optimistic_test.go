package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUpdateAgentOptimisticLock：UpdateAgent 按 UpdatedAt 条件更新（乐观锁）——
// 并发读-改-写后写者必须失败（ErrConflict），不得静默覆盖对方的修改。
func TestUpdateAgentOptimisticLock(t *testing.T) {
	check := func(t *testing.T, s Store) {
		t.Helper()
		ctx := context.Background()
		a := &Agent{Name: "orig", Runner: "cli", Config: map[string]any{"command": []any{"echo"}}}
		require.NoError(t, s.CreateAgent(ctx, a))

		// 两个并发读者拿到同一版本。
		c1, err := s.GetAgent(ctx, a.ID)
		require.NoError(t, err)
		c2, err := s.GetAgent(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, c1.UpdatedAt, c2.UpdatedAt)

		// 第一个写者成功。
		c1.Name = "v2"
		require.NoError(t, s.UpdateAgent(ctx, c1))

		// 第二个写者基于过期版本：必须 ErrConflict，且不覆盖 v2。
		c2.Name = "stale-write"
		require.ErrorIs(t, s.UpdateAgent(ctx, c2), ErrConflict)
		got, err := s.GetAgent(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, "v2", got.Name)

		// 刷新后重写成功。
		fresh, err := s.GetAgent(ctx, a.ID)
		require.NoError(t, err)
		fresh.Name = "v3"
		require.NoError(t, s.UpdateAgent(ctx, fresh))
	}
	t.Run("memory", func(t *testing.T) { check(t, NewMemory()) })
	t.Run("pg", func(t *testing.T) { check(t, testPool(t)) })
}
