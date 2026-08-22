package syncsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// fakeFetch 是 T01 拉取面的测试替身：内容可变（幂等测试两轮喂不同数据），
// 可注入致命错误，可阻塞（运行互斥测试）。
type fakeFetch struct {
	projects []tasksource.Project
	issues   []tasksource.Issue
	projErr  error
	issErr   error

	// entered/release 非空时 ListProjects 先发信号再等放行（并发守卫测试）。
	entered chan struct{}
	release chan struct{}
}

func (f *fakeFetch) ListProjects(ctx context.Context) ([]tasksource.Project, error) {
	if f.entered != nil {
		f.entered <- struct{}{}
		<-f.release
	}
	if f.projErr != nil {
		return nil, f.projErr
	}
	return f.projects, nil
}

func (f *fakeFetch) ListIssues(ctx context.Context) ([]tasksource.Issue, error) {
	if f.issErr != nil {
		return nil, f.issErr
	}
	return f.issues, nil
}

func ptr[T any](v T) *T { return &v }

func proj(id, title string) tasksource.Project {
	return tasksource.Project{ID: id, Title: title, Status: "in_progress", Priority: "high", UpdatedAt: time.Now()}
}

func iss(id, key, title, projectID string) tasksource.Issue {
	return tasksource.Issue{
		ID: id, Identifier: key, Title: title, Status: "todo",
		Priority: "medium", ProjectID: &projectID, UpdatedAt: time.Now(),
	}
}

// findByExternalIssueID 在已落库的 deliveries 里按外部 issue ID 找行。
func findByExternalIssueID(t *testing.T, st *store.Memory, extID string) *store.Delivery {
	t.Helper()
	ctx := context.Background()
	projs, err := st.ListProjects(ctx)
	require.NoError(t, err)
	for _, p := range projs {
		for _, d := range func() []store.Delivery {
			ds, err := st.ListProjectDeliveries(ctx, p.ID)
			require.NoError(t, err)
			return ds
		}() {
			if d.ExternalIssueID == extID {
				return &d
			}
		}
	}
	return nil
}

// --- AC: POST 触发同步导入当前 workspace 全部项目与 issue（含父子关系） ---

func TestSyncHappyPathImportsProjectsAndIssues(t *testing.T) {
	st := store.NewMemory()
	agentID := "7bc775bc-db05-47bc-8f45-5c3baecc3fe3"
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues: []tasksource.Issue{
			{
				ID: "m-iss-1", Identifier: "INFERA-1", Title: "父需求", Status: "in_progress",
				Priority: "urgent", Description: ptr("父描述"), ProjectID: ptr("m-prj-1"),
				AssigneeType: ptr("agent"), AssigneeID: ptr(agentID), UpdatedAt: time.Now(),
			},
			{
				ID: "m-iss-2", Identifier: "INFERA-2", Title: "子需求", Status: "todo",
				Priority: "low", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-iss-1"),
				UpdatedAt: time.Now(),
			},
			{
				ID: "m-iss-3", Identifier: "INFERA-3", Title: "独立需求", Status: "done",
				Priority: "none", ProjectID: ptr("m-prj-1"), UpdatedAt: time.Now(),
			},
		},
	}
	svc := New(f, st)
	res, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Empty(t, res.Error)
	require.Equal(t, 1, res.ProjectsImported)
	require.Equal(t, 3, res.IssuesImported)
	require.Equal(t, 0, res.IssuesSkipped)
	require.False(t, res.StartedAt.IsZero())
	require.False(t, res.FinishedAt.IsZero())

	// 项目：Name 落上游标题，外部 ID 与同步时间回填。
	projs, err := st.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projs, 1)
	require.Equal(t, "自动闭环", projs[0].Name)
	require.Equal(t, "m-prj-1", projs[0].ExternalProjectID)
	require.NotNil(t, projs[0].ExternalSyncedAt)

	// 父需求：标题/描述/负责人/优先级/展示键落库；状态翻译为 queued（非 active）。
	parent := findByExternalIssueID(t, st, "m-iss-1")
	require.NotNil(t, parent)
	require.Equal(t, "父需求", parent.Title)
	require.Equal(t, "父描述", parent.Description)
	require.Equal(t, "INFERA-1", parent.ExternalIssueKey)
	require.Equal(t, "agent:"+agentID, parent.Assignee)
	require.Equal(t, "urgent", parent.Priority)
	require.Equal(t, "queued", parent.Status)
	require.Empty(t, parent.ParentID)
	require.Zero(t, parent.Wave, "顶层需求 wave=0")

	// 子需求：ParentID 解析为父的 infera 内部 ID；未带 stage → wave 0（无阶段）。
	child := findByExternalIssueID(t, st, "m-iss-2")
	require.NotNil(t, child)
	require.Equal(t, parent.ID, child.ParentID)
	require.Zero(t, child.Wave, "无 stage 子任务 wave=0（无阶段），不兜底 1")

	// 终态翻译：done → completed。
	done := findByExternalIssueID(t, st, "m-iss-3")
	require.NotNil(t, done)
	require.Equal(t, "completed", done.Status)

	// 同步后 Last() 给出本轮结果。
	last := svc.Last()
	require.NotNil(t, last)
	require.Equal(t, res.IssuesImported, last.IssuesImported)
}

// --- AC: 幂等——重复触发不产生重复行，只更新 ---

func TestSyncIdempotentReimport(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues: []tasksource.Issue{
			iss("m-iss-1", "INFERA-1", "旧标题", "m-prj-1"),
		},
	}
	svc := New(f, st)
	res1, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res1.ProjectsImported)
	require.Equal(t, 1, res1.IssuesImported)

	first := findByExternalIssueID(t, st, "m-iss-1")
	require.NotNil(t, first)

	// infera 侧推进引擎字段（模拟本地状态）。
	first.CurrentStage = "code_gen"
	first.PendingGate = "code_review"
	require.NoError(t, st.UpdateDelivery(context.Background(), first))

	// 第二轮：标题更新、加一个新 issue。
	f.projects = []tasksource.Project{proj("m-prj-1", "自动闭环（改名）")}
	f.issues = []tasksource.Issue{
		iss("m-iss-1", "INFERA-1", "新标题", "m-prj-1"),
		iss("m-iss-2", "INFERA-2", "后到的需求", "m-prj-1"),
	}
	res2, err := svc.SyncNow(context.Background())
	require.NoError(t, err)

	// 行数不翻倍：1 项目 2 需求。
	projs, err := st.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projs, 1)
	ds, err := st.ListProjectDeliveries(context.Background(), projs[0].ID)
	require.NoError(t, err)
	require.Len(t, ds, 2)
	require.Equal(t, 2, res2.IssuesImported)

	// 同外部 ID 稳定命中同一行：ID 不变、标题更新、引擎字段保留。
	got := findByExternalIssueID(t, st, "m-iss-1")
	require.Equal(t, first.ID, got.ID)
	require.Equal(t, "新标题", got.Title)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Equal(t, "code_review", got.PendingGate)

	// 项目名更新生效，行数不变。
	require.Equal(t, "自动闭环（改名）", projs[0].Name)
	require.Equal(t, first.ProjectID, projs[0].ID)
}

// --- AC: 标题含 [infera-e2e] 的冒烟单跳过 ---

func TestSyncSkipsSmokeAndProjectlessIssues(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues: []tasksource.Issue{
			iss("m-smoke", "INFERA-90", "[infera-e2e] 冒烟单", "m-prj-1"),
			{ID: "m-noproj", Identifier: "INFERA-91", Title: "无项目 issue", Status: "todo", UpdatedAt: time.Now()},
			iss("m-normal", "INFERA-92", "正常单", "m-prj-1"),
			// 冒烟单的子单：父未导入 → 按顶层导入（父缺失折叠规则）。
			{
				ID: "m-child-of-smoke", Identifier: "INFERA-93", Title: "冒烟单之子",
				Status: "todo", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-smoke"),
				UpdatedAt: time.Now(),
			},
			// 无项目单的子单：父被跳过 → 子单同样折叠为顶层，不得误判成环。
			{
				ID: "m-child-of-noproj", Identifier: "INFERA-94", Title: "无项目单之子",
				Status: "todo", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-noproj"),
				UpdatedAt: time.Now(),
			},
		},
	}
	svc := New(f, st)
	res, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.ProjectsImported)
	require.Equal(t, 3, res.IssuesImported)
	require.Equal(t, 2, res.IssuesSkipped)

	byReason := map[string]string{}
	for _, sk := range res.Skips {
		byReason[sk.Reason] = sk.ExternalIssueID
	}
	require.Equal(t, "m-smoke", byReason["smoke"])
	require.Equal(t, "m-noproj", byReason["no_project"])

	require.Nil(t, findByExternalIssueID(t, st, "m-smoke"), "冒烟单不落库")
	require.Nil(t, findByExternalIssueID(t, st, "m-noproj"), "无项目 issue 不落库")
	require.NotNil(t, findByExternalIssueID(t, st, "m-normal"))
	orphan := findByExternalIssueID(t, st, "m-child-of-smoke")
	require.NotNil(t, orphan)
	require.Empty(t, orphan.ParentID, "父未导入 → 顶层导入")
	require.Zero(t, orphan.Wave)
	// 父被跳过（no_project）的子单：折叠为顶层，不得误判成环跳过。
	folded := findByExternalIssueID(t, st, "m-child-of-noproj")
	require.NotNil(t, folded)
	require.Empty(t, folded.ParentID)
	require.Zero(t, folded.Wave)
}

// --- AC: 状态翻译——镜像永远不能是 active（重启恢复会点火引擎） ---

func TestTranslateStatus(t *testing.T) {
	cases := map[string]string{
		"todo":        "queued",
		"backlog":     "queued",
		"in_progress": "queued",
		"in_review":   "queued",
		"done":        "completed",
		"cancelled":   "completed",
		"blocked":     "blocked",
		"":            "queued",
		"future-word": "queued",
	}
	for in, want := range cases {
		require.Equal(t, want, translateStatus(in), "上游状态 %q", in)
	}
	// 全词表穷举：任何输入都不得翻出 active。
	for _, in := range []string{"todo", "backlog", "in_progress", "in_review", "done", "blocked", "cancelled", "", "active", "xxx"} {
		require.NotEqual(t, "active", translateStatus(in), "上游状态 %q 不得映射为 active", in)
	}
}

// --- AC: 同步保留子任务的真实阶段（上游 stage → wave，不再全部落阶段 1） ---

// TestSyncChildStagePreserved：含多阶段子任务的项目同步后，子任务在 infera
// 侧的 stage 与 上游侧一致——原生表示即拆分批次 wave（编号阶段>=1），
// 同步镜像沿用同一字段，不发明平行入口。无 stage 子任务 = 「无阶段」，wave 0
// 原样落库（不兜底 1，否则显示层会把它混进「阶段 1」）；顶层的 stage
// 不上行（顶层恒 wave=0）。
func TestSyncChildStagePreserved(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues: []tasksource.Issue{
			{
				ID: "m-parent", Identifier: "INFERA-1", Title: "父需求", Status: "in_progress",
				Priority: "urgent", ProjectID: ptr("m-prj-1"), Stage: 1, UpdatedAt: time.Now(),
			},
			{
				ID: "m-c1", Identifier: "INFERA-2", Title: "阶段 2 子任务", Status: "todo",
				Priority: "low", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-parent"), Stage: 2,
				UpdatedAt: time.Now(),
			},
			{
				ID: "m-c2", Identifier: "INFERA-3", Title: "阶段 3 子任务", Status: "todo",
				Priority: "low", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-parent"), Stage: 3,
				UpdatedAt: time.Now(),
			},
			{
				ID: "m-c3", Identifier: "INFERA-4", Title: "无阶段子任务", Status: "todo",
				Priority: "low", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-parent"),
				UpdatedAt: time.Now(),
			},
		},
	}
	svc := New(f, st)
	res, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, res.IssuesImported)

	require.Zero(t, findByExternalIssueID(t, st, "m-parent").Wave, "顶层恒 wave=0：上游 stage 不上行到顶层")
	require.Equal(t, 2, findByExternalIssueID(t, st, "m-c1").Wave, "子任务 wave = 上游 stage")
	require.Equal(t, 3, findByExternalIssueID(t, st, "m-c2").Wave, "子任务 wave = 上游 stage")
	require.Zero(t, findByExternalIssueID(t, st, "m-c3").Wave, "无 stage 子任务 wave 0 原样落库（0=无阶段，不兜底 1）")
}

// TestSyncChildStageUpdatedOnResync：上游侧改阶段后再同步 → 镜像行 wave
// 跟进（upsert 冲突分支更新 wave，幂等重放不卡在旧阶段）。
func TestSyncChildStageUpdatedOnResync(t *testing.T) {
	st := store.NewMemory()
	child := tasksource.Issue{
		ID: "m-c1", Identifier: "INFERA-2", Title: "子任务", Status: "todo",
		Priority: "low", ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-parent"), Stage: 2,
		UpdatedAt: time.Now(),
	}
	parent := tasksource.Issue{
		ID: "m-parent", Identifier: "INFERA-1", Title: "父需求", Status: "in_progress",
		Priority: "urgent", ProjectID: ptr("m-prj-1"), UpdatedAt: time.Now(),
	}
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues:   []tasksource.Issue{parent, child},
	}
	svc := New(f, st)
	_, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, findByExternalIssueID(t, st, "m-c1").Wave)

	// 上游侧把子任务挪到阶段 3，再同步。
	child.Stage = 3
	f.issues = []tasksource.Issue{parent, child}
	_, err = svc.SyncNow(context.Background())
	require.NoError(t, err)
	got := findByExternalIssueID(t, st, "m-c1")
	require.Equal(t, 3, got.Wave, "重同步跟进了新阶段，且不产生重复行")
}

// --- 父子成环：环上成员跳过，环外正常导入 ---

func TestSyncParentCycleSkipped(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		issues: []tasksource.Issue{
			{
				ID: "m-a", Identifier: "INFERA-A", Title: "A", Status: "todo",
				ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-b"), UpdatedAt: time.Now(),
			},
			{
				ID: "m-b", Identifier: "INFERA-B", Title: "B", Status: "todo",
				ProjectID: ptr("m-prj-1"), ParentIssueID: ptr("m-a"), UpdatedAt: time.Now(),
			},
			iss("m-c", "INFERA-C", "环外正常单", "m-prj-1"),
		},
	}
	svc := New(f, st)
	res, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, res.IssuesImported)
	require.Equal(t, 2, res.IssuesSkipped)
	for _, sk := range res.Skips {
		require.Equal(t, "parent_cycle", sk.Reason)
	}
	require.Nil(t, findByExternalIssueID(t, st, "m-a"))
	require.Nil(t, findByExternalIssueID(t, st, "m-b"))
	require.NotNil(t, findByExternalIssueID(t, st, "m-c"))
}

// --- 并发守卫：同一时刻只允许一轮同步 ---

func TestSyncRunningGuard(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{
		projects: []tasksource.Project{proj("m-prj-1", "自动闭环")},
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	svc := New(f, st)
	require.False(t, svc.Running())
	require.Nil(t, svc.Last(), "首轮前 Last 为 nil")

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := svc.SyncNow(context.Background())
		require.NoError(t, err)
	}()
	<-f.entered // 第一轮已进入拉取
	require.True(t, svc.Running())

	_, err := svc.SyncNow(context.Background())
	require.ErrorIs(t, err, ErrSyncRunning)

	close(f.release)
	<-done
	require.False(t, svc.Running())
	require.NotNil(t, svc.Last())
}

// --- 拉取失败：整轮失败，结果记录错误，不落任何行 ---

func TestSyncFatalPullError(t *testing.T) {
	st := store.NewMemory()
	f := &fakeFetch{projErr: errors.New("tasksource: HTTP 401")}
	svc := New(f, st)
	res, err := svc.SyncNow(context.Background())
	require.Error(t, err)
	require.NotEmpty(t, res.Error, "Result 带回失败原因")

	last := svc.Last()
	require.NotNil(t, last, "失败轮也记录为最近结果")
	require.NotEmpty(t, last.Error)

	projs, err2 := st.ListProjects(context.Background())
	require.NoError(t, err2)
	require.Empty(t, projs)
}
