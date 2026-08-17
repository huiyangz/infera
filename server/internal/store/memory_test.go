package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryProjectPinnedAndStats(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p := &Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, m.CreateProject(ctx, p))
	require.NoError(t, m.PatchProjectPinned(ctx, p.ID, true))
	got, err := m.GetProject(ctx, p.ID)
	require.NoError(t, err)
	require.True(t, got.Pinned)
	require.ErrorIs(t, m.PatchProjectPinned(ctx, "nope", true), ErrNotFound)

	d := &Delivery{ProjectID: p.ID, Title: "t", Status: "active", PendingGate: "spec_approval"}
	require.NoError(t, m.CreateDelivery(ctx, d))
	s, err := m.ProjectStats(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, s.Active)
	require.Equal(t, 1, s.Pending)
}

func TestMemoryDeliveryAndArtifacts(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p := &Project{Name: "p"}
	require.NoError(t, m.CreateProject(ctx, p))
	d := &Delivery{ProjectID: p.ID, Title: "x"}
	require.NoError(t, m.CreateDelivery(ctx, d))

	require.NoError(t, m.SaveArtifact(ctx, &Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "S"}))
	arts, err := m.ListArtifacts(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, arts, 1)

	// LatestArtifact：同 kind 多条取最新；缺失 kind 报 ErrNotFound。
	require.NoError(t, m.SaveArtifact(ctx, &Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "S2"}))
	require.NoError(t, m.SaveArtifact(ctx, &Artifact{DeliveryID: d.ID, Stage: "unit_test", Kind: "test_output", Content: "FAIL"}))
	latestArt, err := m.LatestArtifact(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "S2", latestArt.Content)
	_, err = m.LatestArtifact(ctx, d.ID, "diff")
	require.ErrorIs(t, err, ErrNotFound)

	require.NoError(t, m.AppendEvent(ctx, &Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_started", Payload: []byte(`{}`)}))
	events, err := m.ListEvents(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)

	run := &StageRun{DeliveryID: d.ID, Stage: "spec", Attempt: 1}
	require.NoError(t, m.StartStageRun(ctx, run))
	require.NoError(t, m.FinishStageRun(ctx, run.ID, "done"))
	latest, err := m.LatestStageRun(ctx, d.ID, "spec")
	require.NoError(t, err)
	require.Equal(t, "done", latest.Status)
	require.NotNil(t, latest.FinishedAt)

	_, err = m.LatestStageRun(ctx, d.ID, "missing_stage")
	require.ErrorIs(t, err, ErrNotFound)

	d.Status = "completed"
	require.NoError(t, m.UpdateDelivery(ctx, d))
	got, _ := m.GetDelivery(ctx, d.ID)
	require.Equal(t, "completed", got.Status)

	// ListActiveDeliveries：只返回 active（跨项目），按创建时间升序。
	d2 := &Delivery{ProjectID: p.ID, Title: "y", Status: "active"}
	require.NoError(t, m.CreateDelivery(ctx, d2))
	active, err := m.ListActiveDeliveries(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, d2.ID, active[0].ID)

	_, err = m.GetDelivery(ctx, "nope")
	require.ErrorIs(t, err, ErrNotFound)
}
