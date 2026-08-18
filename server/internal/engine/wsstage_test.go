package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// TestEnsureWorkspaceFailureRecordsCurrentStage：workspace 获取失败的
// stage_failed 事件记交付当前阶段——拆分父在 mergeLoop 里 Acquire 失败时
// 当前阶段是 code_gen，固定记 intake 会标错位置。
func TestEnsureWorkspaceFailureRecordsCurrentStage(t *testing.T) {
	st := store.NewMemory()
	ws := &errAcquireWS{}
	e := New(st, &fakeRunner{}, ws, passTR{})
	ctx := context.Background()

	// 拆分父停在 code_gen（等子需求/合并），workspace 未就绪（子需求先完成的路径）。
	parent := seed(t, st)
	parent.SplitMode = true
	parent.CurrentStage = "code_gen"
	parent.WorkspaceReady = false
	require.NoError(t, st.UpdateDelivery(ctx, parent))

	err := e.MaybeDriveParent(ctx, parent.ID)
	require.Error(t, err)
	require.Equal(t, StatusBlocked, get(t, st, parent.ID).Status)

	evs, err := st.ListEvents(ctx, parent.ID)
	require.NoError(t, err)
	var stageFailed *store.Event
	for i := range evs {
		if evs[i].EventType == "stage_failed" {
			stageFailed = &evs[i]
		}
	}
	require.NotNil(t, stageFailed, "stage_failed event not found")
	require.Equal(t, "code_gen", stageFailed.Stage, "失败事件必须记交付当前阶段，而非固定 intake")

	// workspace_ready 事件同理记当前阶段（成功路径）。
	st2 := store.NewMemory()
	e2 := New(st2, &fakeRunner{}, &FakeWS{}, passTR{})
	p2 := seed(t, st2)
	p2.SplitMode = true
	p2.CurrentStage = "code_gen"
	require.NoError(t, st2.UpdateDelivery(ctx, p2))
	require.NoError(t, e2.MaybeDriveParent(ctx, p2.ID))

	evs2, err := st2.ListEvents(ctx, p2.ID)
	require.NoError(t, err)
	var ready *store.Event
	for i := range evs2 {
		if evs2[i].EventType == "workspace_ready" {
			ready = &evs2[i]
		}
	}
	require.NotNil(t, ready, "workspace_ready event not found")
	require.Equal(t, "code_gen", ready.Stage)
}
