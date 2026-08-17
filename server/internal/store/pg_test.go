package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/db"
)

func testPool(t *testing.T) *Pg {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects`)
	return NewPg(pool)
}

func TestPgProjectAndDelivery(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	proj := &Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, p.CreateProject(ctx, proj))
	require.NoError(t, p.PatchProjectPinned(ctx, proj.ID, true))
	got, err := p.GetProject(ctx, proj.ID)
	require.NoError(t, err)
	require.True(t, got.Pinned)

	d := &Delivery{ProjectID: proj.ID, Title: "需求A", Status: "active", CurrentStage: "spec", PendingGate: "spec_approval"}
	require.NoError(t, p.CreateDelivery(ctx, d))
	d.FailCount = 1
	require.NoError(t, p.UpdateDelivery(ctx, d))
	gotD, err := p.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, 1, gotD.FailCount)

	ev := &Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_started", Payload: []byte(`{}`)}
	require.NoError(t, p.AppendEvent(ctx, ev))
	evs, err := p.ListEvents(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, evs, 1)

	art := &Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# spec"}
	require.NoError(t, p.SaveArtifact(ctx, art))
	arts, err := p.ListArtifacts(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, arts, 1)

	// reject_reason / workspace_ready roundtrip
	d.RejectReason, d.WorkspaceReady = "验收标准缺失", true
	require.NoError(t, p.UpdateDelivery(ctx, d))
	gotD, err = p.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, "验收标准缺失", gotD.RejectReason)
	require.True(t, gotD.WorkspaceReady)

	// split 字段 roundtrip：split_mode/merge_state（parent_id 空串 ↔ NULL 见子需求）。
	d.SplitMode, d.MergeState = true, "conflict"
	require.NoError(t, p.UpdateDelivery(ctx, d))
	gotD, err = p.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.True(t, gotD.SplitMode)
	require.Equal(t, "conflict", gotD.MergeState)
	require.Empty(t, gotD.ParentID, "空 parent_id 落 NULL 回读空串")

	// 子需求：parent_id 落库回读 + ListChildDeliveries 按 wave/created_at 排序。
	child1 := &Delivery{ProjectID: proj.ID, Title: "子1", Status: "queued", CurrentStage: "intake", ParentID: d.ID, Wave: 2}
	require.NoError(t, p.CreateDelivery(ctx, child1))
	time.Sleep(2 * time.Millisecond)
	child2 := &Delivery{ProjectID: proj.ID, Title: "子2", Status: "active", CurrentStage: "intake", ParentID: d.ID, Wave: 1}
	require.NoError(t, p.CreateDelivery(ctx, child2))
	gotChild, err := p.GetDelivery(ctx, child1.ID)
	require.NoError(t, err)
	require.Equal(t, d.ID, gotChild.ParentID)
	require.Equal(t, 2, gotChild.Wave)
	children, err := p.ListChildDeliveries(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, children, 2)
	require.Equal(t, child2.ID, children[0].ID, "wave 1 在前")
	require.Equal(t, child1.ID, children[1].ID)

	// LatestArtifact：同 kind 多条取最新；缺失 kind 报 ErrNotFound。
	time.Sleep(2 * time.Millisecond) // created_at 来自 DB now()，保证严格递增
	require.NoError(t, p.SaveArtifact(ctx, &Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# spec v2"}))
	latest, err := p.LatestArtifact(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "# spec v2", latest.Content)
	_, err = p.LatestArtifact(ctx, d.ID, "diff")
	require.ErrorIs(t, err, ErrNotFound)

	s, err := p.ProjectStats(ctx, proj.ID)
	require.NoError(t, err)
	require.Equal(t, 2, s.Active, "父 + 子2（queued 不计入 active）")
	require.Equal(t, 1, s.Pending)

	// stage runs roundtrip
	run := &StageRun{DeliveryID: d.ID, Stage: "spec", Attempt: 1}
	require.NoError(t, p.StartStageRun(ctx, run))
	require.NoError(t, p.FinishStageRun(ctx, run.ID, "done"))
	lr, err := p.LatestStageRun(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "done", lr.Status)
	require.NotNil(t, lr.FinishedAt)

	// not found paths
	_, err = p.GetDelivery(ctx, "00000000-0000-0000-0000-000000000000")
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, p.PatchProjectPinned(ctx, "00000000-0000-0000-0000-000000000000", false), ErrNotFound)
}

func TestPgListPaths(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()

	// ListProjects：2 条，按 CreatedAt 升序。
	proj1 := &Project{Name: "alpha", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, p.CreateProject(ctx, proj1))
	time.Sleep(2 * time.Millisecond) // 时间戳来自 DB now()，保证严格递增
	proj2 := &Project{Name: "beta", RepoURL: "https://github.com/x/z", DefaultBranch: "main"}
	require.NoError(t, p.CreateProject(ctx, proj2))
	projects, err := p.ListProjects(ctx)
	require.NoError(t, err)
	require.Len(t, projects, 2)
	require.Equal(t, proj1.ID, projects[0].ID)
	require.Equal(t, proj2.ID, projects[1].ID)
	require.False(t, projects[0].CreatedAt.After(projects[1].CreatedAt))

	// ListProjectDeliveries：同项目 2 条，按 CreatedAt 升序。
	d1 := &Delivery{ProjectID: proj1.ID, Title: "需求A", Status: "active", CurrentStage: "spec"}
	require.NoError(t, p.CreateDelivery(ctx, d1))
	time.Sleep(2 * time.Millisecond)
	d2 := &Delivery{ProjectID: proj1.ID, Title: "需求B", Status: "active", CurrentStage: "spec"}
	require.NoError(t, p.CreateDelivery(ctx, d2))
	deliveries, err := p.ListProjectDeliveries(ctx, proj1.ID)
	require.NoError(t, err)
	require.Len(t, deliveries, 2)
	require.Equal(t, d1.ID, deliveries[0].ID)
	require.Equal(t, d2.ID, deliveries[1].ID)
	require.False(t, deliveries[0].CreatedAt.After(deliveries[1].CreatedAt))

	// ListActiveDeliveries：跨项目只取 active，按 CreatedAt 升序。
	d2.Status = "completed"
	require.NoError(t, p.UpdateDelivery(ctx, d2))
	time.Sleep(2 * time.Millisecond)
	d3 := &Delivery{ProjectID: proj2.ID, Title: "需求C", Status: "active", CurrentStage: "spec"}
	require.NoError(t, p.CreateDelivery(ctx, d3))
	active, err := p.ListActiveDeliveries(ctx)
	require.NoError(t, err)
	require.Len(t, active, 2)
	require.Equal(t, d1.ID, active[0].ID)
	require.Equal(t, d3.ID, active[1].ID)

	// 同一 stage 第二次 run 后，LatestStageRun 返回最新一次。
	run1 := &StageRun{DeliveryID: d1.ID, Stage: "spec", Attempt: 1}
	require.NoError(t, p.StartStageRun(ctx, run1))
	require.NoError(t, p.FinishStageRun(ctx, run1.ID, "done"))
	time.Sleep(2 * time.Millisecond)
	run2 := &StageRun{DeliveryID: d1.ID, Stage: "spec", Attempt: 2}
	require.NoError(t, p.StartStageRun(ctx, run2))
	require.NoError(t, p.FinishStageRun(ctx, run2.ID, "failed"))
	latest, err := p.LatestStageRun(ctx, d1.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, run2.ID, latest.ID)
	require.Equal(t, "failed", latest.Status)
	require.Equal(t, 2, latest.Attempt)
}
