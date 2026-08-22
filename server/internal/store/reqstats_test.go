package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// seedStatsStore 铺一个项目：任务同步来源（last_synced_at 非 nil）+ 五条需求
// （active+门、active、queued、completed、blocked），一个从未同步的空项目。
func seedStatsStore(t *testing.T, st Store) (synced, empty *Project) {
	t.Helper()
	ctx := context.Background()
	synced = &Project{Name: "同步项目", ExternalProjectID: "mp-1"}
	require.NoError(t, st.UpsertProjectByExternalID(ctx, synced))
	empty = &Project{Name: "本地项目", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, empty))

	for _, d := range []*Delivery{
		{ProjectID: synced.ID, Title: "卡在规格审批", Status: "active", PendingGate: "spec_approval", CurrentStage: "spec"},
		{ProjectID: synced.ID, Title: "执行中", Status: "active", CurrentStage: "code_gen"},
		{ProjectID: synced.ID, Title: "排队镜像", Status: "queued"},
		{ProjectID: synced.ID, Title: "已交付", Status: "completed"},
		{ProjectID: synced.ID, Title: "被阻塞", Status: "blocked"},
	} {
		require.NoError(t, st.CreateDelivery(ctx, d))
	}
	return synced, empty
}

func checkRequirementStats(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	synced, empty := seedStatsStore(t, st)

	got, err := st.RequirementStats(ctx, synced.ID)
	require.NoError(t, err)
	require.Equal(t, synced.ID, got.ProjectID)
	require.Equal(t, 5, got.RequirementTotal)
	require.Equal(t, map[string]int{"active": 2, "queued": 1, "completed": 1, "blocked": 1}, got.ByStatus)
	require.Equal(t, 1, got.PendingDecisions)
	require.Equal(t, 1, got.Delivered)
	require.NotNil(t, got.LastSyncedAt, "同步来源项目 last_synced_at 非 nil")

	// 从未同步的空项目：零计数 + last_synced_at nil（四个状态键仍在）。
	gotEmpty, err := st.RequirementStats(ctx, empty.ID)
	require.NoError(t, err)
	require.Equal(t, 0, gotEmpty.RequirementTotal)
	require.Equal(t, map[string]int{"active": 0, "queued": 0, "completed": 0, "blocked": 0}, gotEmpty.ByStatus)
	require.Equal(t, 0, gotEmpty.PendingDecisions)
	require.Equal(t, 0, gotEmpty.Delivered)
	require.Nil(t, gotEmpty.LastSyncedAt, "从未同步 → nil 而非零值时间")

	_, err = st.RequirementStats(ctx, "0b7ddc6e-0000-4000-8000-000000000000")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryRequirementStats(t *testing.T) {
	checkRequirementStats(t, NewMemory())
}

func TestPgRequirementStats(t *testing.T) {
	checkRequirementStats(t, testPool(t))
}

// seedPendingStore 两个项目三条有效待决策 + 干扰行（无门、已完结带残留门）。
// 创建间 sleep 保证 updated_at 严格递增（pg now() 微秒级、memory 同纳秒兜底）。
func seedPendingStore(t *testing.T, st Store) (newest, oldest *Delivery) {
	t.Helper()
	ctx := context.Background()
	p1 := &Project{Name: "项目一"}
	require.NoError(t, st.CreateProject(ctx, p1))
	p2 := &Project{Name: "项目二"}
	require.NoError(t, st.CreateProject(ctx, p2))

	oldest = &Delivery{ProjectID: p1.ID, Title: "规格审批中", Status: "active", PendingGate: "spec_approval", CurrentStage: "spec", ExternalIssueKey: "INFERA-1", Assignee: "agent:lead", Priority: "high"}
	oldest.ExternalIssueID = "mi-1" // 同步来源展示字段（本地行为空）
	require.NoError(t, st.UpsertDeliveryByExternalID(ctx, oldest))
	time.Sleep(5 * time.Millisecond)

	noGate := &Delivery{ProjectID: p1.ID, Title: "无门，不该出现", Status: "active"}
	require.NoError(t, st.CreateDelivery(ctx, noGate))
	stale := &Delivery{ProjectID: p1.ID, Title: "已完结带残留门", Status: "completed", PendingGate: "spec_approval"}
	require.NoError(t, st.CreateDelivery(ctx, stale))
	time.Sleep(5 * time.Millisecond)

	newest = &Delivery{ProjectID: p2.ID, Title: "任务审批中", Status: "active", PendingGate: "tasks_approval", CurrentStage: "tasks"}
	require.NoError(t, st.CreateDelivery(ctx, newest))
	return newest, oldest
}

func checkListPendingDecisions(t *testing.T, st Store) {
	t.Helper()
	newest, oldest := seedPendingStore(t, st)
	rows, err := st.ListPendingDecisions(context.Background())
	require.NoError(t, err)
	require.Len(t, rows, 2, "只含 pending_gate 非空且未完结的行")

	// updated_at DESC：新决策在前。
	require.Equal(t, newest.ID, rows[0].ID)
	require.Equal(t, oldest.ID, rows[1].ID)

	// 行字段足以支撑列表展示与跳转需求详情。
	require.Equal(t, "项目二", rows[0].ProjectName)
	require.Equal(t, "任务审批中", rows[0].Title)
	require.Equal(t, "tasks_approval", rows[0].PendingGate)
	require.Equal(t, "active", rows[0].Status)
	require.Equal(t, newest.ProjectID, rows[0].ProjectID)
	require.Empty(t, rows[0].ExternalIssueKey, "本地需求 issue_key 为空串")
	require.Equal(t, "INFERA-1", rows[1].ExternalIssueKey)
	require.Equal(t, "agent:lead", rows[1].Assignee)
	require.Equal(t, "high", rows[1].Priority)
	require.False(t, rows[1].UpdatedAt.IsZero())
	require.False(t, rows[1].CreatedAt.IsZero())
}

func TestMemoryListPendingDecisions(t *testing.T) {
	checkListPendingDecisions(t, NewMemory())
}

func TestPgListPendingDecisions(t *testing.T) {
	checkListPendingDecisions(t, testPool(t))
}
