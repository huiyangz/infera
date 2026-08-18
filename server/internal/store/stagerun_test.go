package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMemoryLatestStageRunTieBreak：同一 started_at 并列时取后插入的运行
// （插入序 tie-break，稳定序）——latest 的用途是 attempt 递增与收尾定位，
// 取错旧行会让 attempt 回退。
func TestMemoryLatestStageRunTieBreak(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	now := time.Now().UTC()
	// 直接喂进内部存储：模拟同刻并列（StartStageRun 的 now() 造不出并列）。
	m.stageRuns["d1"] = []*StageRun{
		{ID: "run-1", DeliveryID: "d1", Stage: "spec", Attempt: 1, Status: "done", StartedAt: now},
		{ID: "run-2", DeliveryID: "d1", Stage: "spec", Attempt: 2, Status: "running", StartedAt: now},
	}
	got, err := m.LatestStageRun(ctx, "d1", "spec")
	require.NoError(t, err)
	require.Equal(t, "run-2", got.ID, "同刻并列应取后插入者")
}

// TestPgLatestStageRunTieBreak：pg 同 started_at 并列时按 attempt、id 稳定排序，
// 取最新一次运行。
func TestPgLatestStageRunTieBreak(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	proj := &Project{Name: "tie", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, p.CreateProject(ctx, proj))
	d := &Delivery{ProjectID: proj.ID, Title: "需求", Status: "active", CurrentStage: "spec"}
	require.NoError(t, p.CreateDelivery(ctx, d))

	// 同一 started_at 两条（绕过 StartStageRun 的 DB 默认 now()）。
	ts := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	for i, id := range []string{"aaaaaaaa-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"} {
		_, err := p.pool.Exec(ctx,
			`INSERT INTO stage_runs (id,delivery_id,stage,attempt,status,started_at) VALUES ($1,$2,'spec',$3,'done',$4)`,
			id, d.ID, i+1, ts)
		require.NoError(t, err)
	}
	got, err := p.LatestStageRun(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, 2, got.Attempt, "并列时取 attempt 更高（最新一次）的运行")
}
