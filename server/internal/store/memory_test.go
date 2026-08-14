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
	require.NoError(t, m.AppendEvent(ctx, &Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_started", Payload: []byte(`{}`)}))
	require.NoError(t, m.StartStageRun(ctx, &StageRun{DeliveryID: d.ID, Stage: "spec", Attempt: 1}))
	runs, err := m.ListEvents(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	d.Status = "completed"
	require.NoError(t, m.UpdateDelivery(ctx, d))
	got, _ := m.GetDelivery(ctx, d.ID)
	require.Equal(t, "completed", got.Status)

	_, err = m.GetDelivery(ctx, "nope")
	require.ErrorIs(t, err, ErrNotFound)
}
