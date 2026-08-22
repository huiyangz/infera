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

// driveToDesignApproval 按大需求驱动到 design_approval 门禁
// （spec 门裁定 large → design agent 产设计文档 → 停设计门）。
func driveToDesignApproval(t *testing.T, e *Engine, st *store.Memory) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	d := driveToSpecApproval(t, e, st)
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
	require.NoError(t, e.Continue(ctx, d.ID))
	got := get(t, st, d.ID)
	require.Equal(t, "design_approval", got.CurrentStage)
	require.Equal(t, "design_approval", got.PendingGate)
	require.Equal(t, "# 设计正文", artifactByKind(t, st, d.ID, "design").Content)
	return d
}

// driveToTasksApproval 大需求全门推进到 tasks_approval（设计门不拆分）。
func driveToTasksApproval(t *testing.T, e *Engine, st *store.Memory) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	d := driveToDesignApproval(t, e, st)
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{})) // → tasks
	require.NoError(t, e.Continue(ctx, d.ID))
	got := get(t, st, d.ID)
	require.Equal(t, "tasks_approval", got.CurrentStage)
	require.Equal(t, "tasks_approval", got.PendingGate)
	require.NotNil(t, artifactByKind(t, st, d.ID, "tasks"))
	return d
}

// TestApproveSplitAtDesignApproval：设计门「批准并拆分」——父跳过
// tasks/tasks_approval/test_gen 停 code_gen（不执行这三个阶段），建子需求并点火 wave 1。
func TestApproveSplitAtDesignApproval(t *testing.T) {
	e, st, started := splitEnv(t)
	d := driveToDesignApproval(t, e, st)
	ctx := context.Background()

	children, err := e.Approve(ctx, d.ID, store.ApproveOpts{Split: []store.ChildSpec{
		{Title: "登录接口", Description: "实现 /login", Wave: 1},
		{Title: "会话存储", Description: "redis session"}, // wave 归一为 1
		{Title: "登出接口", Description: "实现 /logout", Wave: 2},
	}})
	require.NoError(t, err)
	require.Len(t, children, 3)

	// 父：split_mode、跳过 tasks/tasks_approval/test_gen 停在 code_gen、门禁清空。
	parent := get(t, st, d.ID)
	require.True(t, parent.SplitMode)
	require.Equal(t, "code_gen", parent.CurrentStage)
	require.Empty(t, parent.PendingGate)
	require.Equal(t, StatusActive, parent.Status)
	require.Contains(t, eventTypes(t, st, d.ID), "split")
	// 跳过三阶段的证据：无对应 StageRun（拆分父停 code_gen 走合并语义，也不会有 code_gen StageRun）。
	for _, stage := range []string{"tasks", "tasks_approval", "test_gen", "code_gen"} {
		_, err := st.LatestStageRun(ctx, d.ID, stage)
		require.Error(t, err, "拆分父不应执行阶段 %s", stage)
	}

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

// TestApproveSplitInvalid：拆分只认 design_approval——空标题 / 绿地项目 /
// spec 门与其它门带 split 都报错，且失败不消费门禁。
func TestApproveSplitInvalid(t *testing.T) {
	e, st, _ := splitEnv(t)
	ctx := context.Background()

	// spec_approval 门带 split：拆分已迁设计门，报错且不消费门禁。
	d := driveToSpecApproval(t, e, st)
	_, err := e.Approve(ctx, d.ID, store.ApproveOpts{Split: []store.ChildSpec{{Title: "子A", Wave: 1}}})
	require.ErrorContains(t, err, "only allowed at design_approval")
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)

	// design 门：空标题报错（校验失败不消费门禁）。
	d2 := driveToDesignApproval(t, e, st)
	_, err = e.Approve(ctx, d2.ID, store.ApproveOpts{Split: []store.ChildSpec{{Title: " ", Description: "x"}}})
	require.ErrorContains(t, err, "empty title")
	require.Equal(t, "design_approval", get(t, st, d2.ID).PendingGate)

	// 绿地项目（无仓库）不支持拆分。
	p := &store.Project{Name: "greenfield", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	gd := &store.Delivery{ProjectID: p.ID, Title: "绿地", Status: StatusActive, CurrentStage: "intake"}
	require.NoError(t, st.CreateDelivery(ctx, gd))
	require.NoError(t, e.Start(ctx, gd.ID))
	require.NoError(t, approve(ctx, e, gd.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
	require.NoError(t, e.Continue(ctx, gd.ID))
	_, err = e.Approve(ctx, gd.ID, store.ApproveOpts{Split: []store.ChildSpec{{Title: "子"}}})
	require.ErrorContains(t, err, "requires a project repo")

	// 其它门禁（code_review）带 split 同样拒绝。
	d3 := driveToTasksApproval(t, e, st)
	require.NoError(t, approve(ctx, e, d3.ID, store.ApproveOpts{})) // → test_gen
	require.NoError(t, e.Continue(ctx, d3.ID))                      // → code_review
	require.Equal(t, "code_review", get(t, st, d3.ID).PendingGate)
	_, err = e.Approve(ctx, d3.ID, store.ApproveOpts{Split: []store.ChildSpec{{Title: "子"}}})
	require.ErrorContains(t, err, "only allowed at design_approval")
	require.Equal(t, "code_review", get(t, st, d3.ID).PendingGate, "失败不消费门禁")
}

// TestApproveDesignGateWithoutSplit：设计门普通批准 → tasks（不拆分照走任务门）。
func TestApproveDesignGateWithoutSplit(t *testing.T) {
	e, st, started := splitEnv(t)
	d := driveToDesignApproval(t, e, st)

	children, err := e.Approve(context.Background(), d.ID, store.ApproveOpts{})
	require.NoError(t, err)
	require.Empty(t, children)

	got := get(t, st, d.ID)
	require.False(t, got.SplitMode)
	require.Equal(t, "tasks", got.CurrentStage, "设计门普通批准 → tasks")
	require.Empty(t, got.PendingGate)
	require.Empty(t, *started)
}

// TestContinueOnParkedSplitParentRoutesToMerge gap 守卫：拆分父停在 code_gen 时，
// Continue/run 必须走合并推进（MaybeDriveParent），绝不能执行 code_gen AGENT 节点
// （否则重启恢复/approve 后重点火会让父自己"实现"，覆盖子需求合并语义）。
func TestContinueOnParkedSplitParentRoutesToMerge(t *testing.T) {
	e, st, proj := newRealEnv(t)
	ctx := context.Background()

	parent := &store.Delivery{ProjectID: proj.ID, Title: "父", Status: StatusActive,
		CurrentStage: "code_gen", SplitMode: true}
	require.NoError(t, st.CreateDelivery(ctx, parent))
	child := &store.Delivery{ProjectID: proj.ID, Title: "子", Status: StatusCompleted,
		CurrentStage: "code_review", ParentID: parent.ID, Wave: 1}
	require.NoError(t, st.CreateDelivery(ctx, child))
	// 子需求分支已推（真 git）：Continue 应把它合进父并收尾推进。
	pushChildBranch(t, proj, child.ID, "a.txt", "aaa")

	require.NoError(t, e.Continue(ctx, parent.ID))

	got := get(t, st, parent.ID)
	require.Equal(t, "code_review", got.PendingGate, "应经合并收尾推进到 code_review 门")
	require.Equal(t, 1, mergedChildCount(t, st, parent.ID))
	// 证据：父从未执行过 code_gen 阶段（AGENT 路径会留下 code_gen StageRun）。
	if _, err := st.LatestStageRun(ctx, parent.ID, "code_gen"); err == nil {
		t.Fatal("split parent must never run the code_gen agent step")
	}
}

// --- INFERA-146 回归：无阶段（wave 0）子任务不得干扰批次调度 ---
//
// wave 0 = 任务同步镜像无阶段子任务（见 syncsvc 字段约定）。startDueWaves
// 以 nextWave==0 作「无 queued 批次」哨兵，且前序检查按 Wave<nextWave 判定；
// wave 0 的 queued 镜像若混入扫描会（a）误触哨兵静默禁用调度、（b）被当成
// 永不完成的「前序批次」卡死后续批次。引擎自身拆分的孩子恒 wave>=1
// （normalizeSplit 归一），混入只可能来自同步镜像挂到拆分父，扫描侧跳过
// Wave<=0 防御。

// seedWaveChildren 建拆分父 + 指定 (wave, status) 的子任务并从库里读回
// （ListChildDeliveries 按 wave 升序返回新鲜副本，UpdateDelivery 乐观锁可过）。
func seedWaveChildren(t *testing.T, st *store.Memory, parentID, projectID string, spec []struct {
	id     string
	wave   int
	status string
}) []store.Delivery {
	t.Helper()
	ctx := context.Background()
	for _, s := range spec {
		require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{
			ID: s.id, ProjectID: projectID, ParentID: parentID,
			Wave: s.wave, Title: s.id, Status: s.status, CurrentStage: "intake",
		}))
	}
	kids, err := st.ListChildDeliveries(ctx, parentID)
	require.NoError(t, err)
	return kids
}

// TestStartDueWavesSkipsNoStageChildren：queued 的 wave 0 镜像子任务在场时，
// 最小 queued 批次照常点火——哨兵不被误触，wave 0 自身保持原状不被点火。
func TestStartDueWavesSkipsNoStageChildren(t *testing.T) {
	e, st, started := splitEnv(t)
	ctx := context.Background()
	parent := seed(t, st)
	parent.SplitMode = true
	parent.CurrentStage = "code_gen"
	parent.Status = StatusActive
	require.NoError(t, st.UpdateDelivery(ctx, parent))

	kids := seedWaveChildren(t, st, parent.ID, parent.ProjectID, []struct {
		id     string
		wave   int
		status string
	}{
		{"k-w0", 0, StatusQueued},  // 同步镜像：无阶段
		{"k-w1a", 1, StatusQueued}, // 引擎批次 1
		{"k-w1b", 1, StatusQueued},
		{"k-w2", 2, StatusQueued},
	})
	e.startDueWaves(ctx, kids, map[string]bool{})

	require.Equal(t, StatusActive, get(t, st, "k-w1a").Status, "wave 1 照常点火")
	require.Equal(t, StatusActive, get(t, st, "k-w1b").Status)
	require.Equal(t, StatusQueued, get(t, st, "k-w2").Status, "wave 2 未到批次")
	require.Equal(t, StatusQueued, get(t, st, "k-w0").Status, "wave 0 镜像不被点火")
	require.ElementsMatch(t, []string{"k-w1a", "k-w1b"}, *started)
	require.Contains(t, eventTypes(t, st, "k-w1a"), "wave_started")
	require.NotContains(t, eventTypes(t, st, "k-w0"), "wave_started")
}

// TestStartDueWavesNoStageChildDoesNotBlockLaterWave：wave 1 全部完成并合并后，
// queued 的 wave 0 镜像不构成「前序批次」，wave 2 照常启动。
func TestStartDueWavesNoStageChildDoesNotBlockLaterWave(t *testing.T) {
	e, st, started := splitEnv(t)
	ctx := context.Background()
	parent := seed(t, st)
	parent.SplitMode = true
	parent.CurrentStage = "code_gen"
	parent.Status = StatusActive
	require.NoError(t, st.UpdateDelivery(ctx, parent))

	kids := seedWaveChildren(t, st, parent.ID, parent.ProjectID, []struct {
		id     string
		wave   int
		status string
	}{
		{"k-w0", 0, StatusQueued},
		{"k-w1", 1, StatusCompleted},
		{"k-w2", 2, StatusQueued},
	})
	e.startDueWaves(ctx, kids, map[string]bool{"k-w1": true})

	require.Equal(t, StatusActive, get(t, st, "k-w2").Status, "wave 2 不被 wave 0 卡死")
	require.Equal(t, []string{"k-w2"}, *started)
	require.Equal(t, StatusQueued, get(t, st, "k-w0").Status)
}
