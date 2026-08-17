package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// splitEnv 带拆分点火回调的内存环境。
func splitEnv(t *testing.T) (*Engine, *store.Memory, *[]string) {
	t.Helper()
	e, st, _, _ := newEnv(t, passTR{})
	started := &[]string{}
	e.OnStartDelivery = func(id string) { *started = append(*started, id) }
	return e, st, started
}

// driveToSpecApproval 建 delivery 并驱动到 spec_approval 门禁。
func driveToSpecApproval(t *testing.T, e *Engine, st *store.Memory) *store.Delivery {
	t.Helper()
	d := seed(t, st)
	require.NoError(t, e.Start(context.Background(), d.ID))
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)
	return d
}

func TestApproveWithSplitCreatesChildren(t *testing.T) {
	e, st, started := splitEnv(t)
	d := driveToSpecApproval(t, e, st)
	ctx := context.Background()

	children, err := e.ApproveWithSplit(ctx, d.ID, []store.ChildSpec{
		{Title: "登录接口", Description: "实现 /login", Wave: 1},
		{Title: "会话存储", Description: "redis session"}, // wave 归一为 1
		{Title: "登出接口", Description: "实现 /logout", Wave: 2},
	})
	require.NoError(t, err)
	require.Len(t, children, 3)

	// 父：split_mode、跳过 test_gen 停在 code_gen、门禁清空。
	parent := get(t, st, d.ID)
	require.True(t, parent.SplitMode)
	require.Equal(t, "code_gen", parent.CurrentStage)
	require.Empty(t, parent.PendingGate)
	require.Equal(t, StatusActive, parent.Status)
	require.Contains(t, eventTypes(t, st, d.ID), "split")

	// 子需求：指向父、批次正确；wave 1 已启动（active），wave 2 仍 queued。
	gotChildren, err := st.ListChildDeliveries(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, gotChildren, 3)
	w1a, w1b, w2 := gotChildren[0], gotChildren[1], gotChildren[2]
	require.Equal(t, d.ID, w1a.ParentID)
	require.Equal(t, 1, w1a.Wave)
	require.Equal(t, 1, w1b.Wave, "空 wave 归一为 1")
	require.Equal(t, 2, w2.Wave)
	require.Equal(t, StatusActive, w1a.Status)
	require.Equal(t, StatusActive, w1b.Status)
	require.Equal(t, StatusQueued, w2.Status, "wave 2 未到批次不启动")
	require.Equal(t, "intake", w2.CurrentStage)

	// 只点火 wave 1 的两个子需求。
	require.ElementsMatch(t, []string{w1a.ID, w1b.ID}, *started)
	require.Contains(t, eventTypes(t, st, w1a.ID), "delivery_created")
	require.Contains(t, eventTypes(t, st, w1a.ID), "wave_started")
	require.NotContains(t, eventTypes(t, st, w2.ID), "wave_started")

	// queued 不进 ListActiveDeliveries（重启恢复不会误驱动未来批次）。
	active, err := st.ListActiveDeliveries(ctx)
	require.NoError(t, err)
	ids := make([]string, 0)
	for _, a := range active {
		ids = append(ids, a.ID)
	}
	require.NotContains(t, ids, w2.ID)
	require.Contains(t, ids, w1a.ID)
}

func TestApproveWithSplitInvalid(t *testing.T) {
	e, st, _ := splitEnv(t)
	d := driveToSpecApproval(t, e, st)
	ctx := context.Background()

	// 空标题报错。
	_, err := e.ApproveWithSplit(ctx, d.ID, []store.ChildSpec{{Title: " ", Description: "x"}})
	require.ErrorContains(t, err, "empty title")
	// 校验失败不消费门禁。
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)

	// 绿地项目（无仓库）不支持拆分。
	p := &store.Project{Name: "greenfield", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	gd := &store.Delivery{ProjectID: p.ID, Title: "绿地", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, gd))
	require.NoError(t, e.Start(ctx, gd.ID))
	_, err = e.ApproveWithSplit(ctx, gd.ID, []store.ChildSpec{{Title: "子"}})
	require.ErrorContains(t, err, "requires a project repo")

	// 非 spec_approval 门禁不允许拆分：驱动到 code_review 再试。
	d2 := driveToSpecApproval(t, e, st)
	require.NoError(t, e.Approve(ctx, d2.ID))
	require.NoError(t, e.Continue(ctx, d2.ID))
	require.Equal(t, "code_review", get(t, st, d2.ID).PendingGate)
	_, err = e.ApproveWithSplit(ctx, d2.ID, []store.ChildSpec{{Title: "子"}})
	require.ErrorContains(t, err, "only allowed at spec_approval")
	require.Equal(t, "code_review", get(t, st, d2.ID).PendingGate, "失败不消费门禁")
}

func TestApproveWithoutSplitUnchanged(t *testing.T) {
	e, st, started := splitEnv(t)
	d := driveToSpecApproval(t, e, st)
	ctx := context.Background()

	children, err := e.ApproveWithSplit(ctx, d.ID, nil)
	require.NoError(t, err)
	require.Empty(t, children)

	got := get(t, st, d.ID)
	require.False(t, got.SplitMode)
	require.Equal(t, "test_gen", got.CurrentStage, "普通批准照走 test_gen")
	require.Empty(t, got.PendingGate)
	require.Empty(t, *started)
}
