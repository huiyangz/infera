package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// bindingsByNode 把绑定列表折叠成 node→agentID（断言用）。
func bindingsByNode(bs []PipelineBinding) map[string]string {
	out := map[string]string{}
	for _, b := range bs {
		out[b.Node] = b.AgentID
	}
	return out
}

// checkReplaceBindings 断言 ReplaceBindings 的原子替换语义（内存/pg 共用）：
// 成功 = 旧集合整体消失、新集合整体生效（含空集合=清空）；
// 失败（不存在的 agent/项目）= 报错且原状态一字不差（防半写）。
func checkReplaceBindings(t *testing.T, s Store) {
	t.Helper()
	ctx := context.Background()
	p, a1, a2 := seedAgentBindings(t, s)

	// 现状：默认 spec/test_gen → a1
	require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{Node: "spec", AgentID: a1.ID}))
	require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{Node: "test_gen", AgentID: a1.ID}))

	// 全量替换默认：test_gen 不在新集合 → 消失；code_gen 新增
	require.NoError(t, s.ReplaceBindings(ctx, "", map[string]string{"spec": a2.ID, "code_gen": a1.ID}))
	require.Equal(t, map[string]string{"spec": a2.ID, "code_gen": a1.ID}, bindingsByNode(mustList(s, ctx, "")))

	// 半写场景：test_gen 引用不存在的 agent → 报错 + 原状态完全保留。
	// （pg 实现按节点序插入：code_gen/spec 先写成功，test_gen 失败触发整体回滚）
	err := s.ReplaceBindings(ctx, "", map[string]string{
		"spec":     a1.ID,
		"code_gen": a2.ID,
		"test_gen": "00000000-0000-0000-0000-000000000000",
	})
	require.ErrorIs(t, err, ErrNotFound)
	require.Equal(t, map[string]string{"spec": a2.ID, "code_gen": a1.ID}, bindingsByNode(mustList(s, ctx, "")), "失败的替换不得留下半写状态")

	// 项目级：替换、清空、失败回滚
	require.NoError(t, s.UpsertBinding(ctx, &PipelineBinding{ProjectID: p.ID, Node: "spec", AgentID: a1.ID}))
	require.NoError(t, s.ReplaceBindings(ctx, p.ID, map[string]string{"test_gen": a2.ID, "code_gen": a1.ID}))
	require.Equal(t, map[string]string{"test_gen": a2.ID, "code_gen": a1.ID}, bindingsByNode(mustList(s, ctx, p.ID)))

	err = s.ReplaceBindings(ctx, p.ID, map[string]string{"spec": "00000000-0000-0000-0000-000000000000"})
	require.ErrorIs(t, err, ErrNotFound)
	require.Equal(t, map[string]string{"test_gen": a2.ID, "code_gen": a1.ID}, bindingsByNode(mustList(s, ctx, p.ID)), "项目级失败的替换不得留下半写状态")

	// 空集合 = 清空
	require.NoError(t, s.ReplaceBindings(ctx, p.ID, map[string]string{}))
	require.Empty(t, mustList(s, ctx, p.ID))

	// 不存在的项目 → ErrNotFound（默认绑定不受影响）
	require.ErrorIs(t, s.ReplaceBindings(ctx, "00000000-0000-0000-0000-000000000000", map[string]string{"spec": a1.ID}), ErrNotFound)
	require.Equal(t, map[string]string{"spec": a2.ID, "code_gen": a1.ID}, bindingsByNode(mustList(s, ctx, "")))
}

func TestMemoryReplaceBindings(t *testing.T) {
	checkReplaceBindings(t, NewMemory())
}

func TestPgReplaceBindings(t *testing.T) {
	checkReplaceBindings(t, testPool(t))
}
