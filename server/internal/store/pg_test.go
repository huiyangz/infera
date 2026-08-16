package store

import (
	"context"
	"os"
	"testing"

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

	s, err := p.ProjectStats(ctx, proj.ID)
	require.NoError(t, err)
	require.Equal(t, 1, s.Active)
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
