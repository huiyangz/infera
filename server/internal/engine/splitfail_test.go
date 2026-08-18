package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// failStore 按条件注入失败的 store 包装（引擎错误路径测试用）。
type failStore struct {
	store.Store
	failUpdateTitle map[string]int // title -> 剩余失败次数（UpdateDelivery）
	failCreateAfter int            // 第 N 次 CreateDelivery 起失败（1-based；0=不失败）
	failLatestRun   bool           // LatestStageRun 一律失败（非 NotFound）
	creates         int
}

func (f *failStore) LatestStageRun(ctx context.Context, deliveryID, stage string) (*store.StageRun, error) {
	if f.failLatestRun {
		return nil, errors.New("db: latest stage run failed")
	}
	return f.Store.LatestStageRun(ctx, deliveryID, stage)
}

func (f *failStore) UpdateDelivery(ctx context.Context, d *store.Delivery) error {
	if n, ok := f.failUpdateTitle[d.Title]; ok && n > 0 {
		f.failUpdateTitle[d.Title] = n - 1
		return errors.New("db: update delivery failed")
	}
	return f.Store.UpdateDelivery(ctx, d)
}

func (f *failStore) CreateDelivery(ctx context.Context, d *store.Delivery) error {
	f.creates++
	if f.failCreateAfter > 0 && f.creates > f.failCreateAfter {
		return errors.New("db: create delivery failed")
	}
	return f.Store.CreateDelivery(ctx, d)
}

// TestStartDueWavesFailureEmitsEvent：子需求状态更新失败不得静默——
// 留 wave_start_failed 事件（子需求仍 queued，下一轮调度自动重试）。
func TestStartDueWavesFailureEmitsEvent(t *testing.T) {
	inner := store.NewMemory()
	fst := &failStore{Store: inner, failUpdateTitle: map[string]int{"子B": 1}}
	e := New(fst, &fakeRunner{}, &FakeWS{}, passTR{})
	d := seed(t, inner)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
	require.NoError(t, e.Continue(ctx, d.ID)) // → design_approval
	_, err := e.Approve(ctx, d.ID, store.ApproveOpts{Split: []store.ChildSpec{
		{Title: "子A", Wave: 1}, {Title: "子B", Wave: 1},
	}})
	require.NoError(t, err)

	// A 启动成功；B 更新失败仍 queued，但事件已留痕（不静默卡死）。
	require.Equal(t, StatusActive, get(t, inner, childByTitle(t, inner, d.ID, "子A").ID).Status)
	b := childByTitle(t, inner, d.ID, "子B")
	require.Equal(t, StatusQueued, get(t, inner, b.ID).Status)
	require.Contains(t, eventTypes(t, inner, b.ID), "wave_start_failed")

	// 下一轮调度（失败是暂态的）：重试成功，B 也启动。
	children, err := inner.ListChildDeliveries(ctx, d.ID)
	require.NoError(t, err)
	e.startDueWaves(ctx, children, map[string]bool{})
	require.Equal(t, StatusActive, get(t, inner, b.ID).Status)
	require.Contains(t, eventTypes(t, inner, b.ID), "wave_started")
}

// TestApproveSplitCreateFailureStillStartsWave1：拆分中途 CreateDelivery 失败——
// 已建子需求仍要调度（不得永不点火），错误上抛、父状态已落（split_mode）。
func TestApproveSplitCreateFailureStillStartsWave1(t *testing.T) {
	inner := store.NewMemory()
	fst := &failStore{Store: inner, failCreateAfter: 1} // 第 2 个子需求起失败
	e := New(fst, &fakeRunner{}, &FakeWS{}, passTR{})
	var started []string
	e.OnStartDelivery = func(id string) { started = append(started, id) }
	d := seed(t, inner)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, approve(ctx, e, d.ID, store.ApproveOpts{Complexity: ComplexityLarge}))
	require.NoError(t, e.Continue(ctx, d.ID)) // → design_approval

	kids, err := e.Approve(ctx, d.ID, store.ApproveOpts{Split: []store.ChildSpec{
		{Title: "子A", Wave: 1}, {Title: "子B", Wave: 1}, {Title: "子C", Wave: 1},
	}})
	require.Error(t, err, "CreateDelivery 失败必须上抛")
	require.Len(t, kids, 1, "已创建的子需求照常返回")

	// 父已进入拆分模式；子A 已被调度点火（不因半写永不调度）。
	got := get(t, inner, d.ID)
	require.True(t, got.SplitMode)
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Equal(t, StatusActive, get(t, inner, kids[0].ID).Status)
	require.Len(t, started, 1)

	// 留痕：拆分子需求创建失败事件（父时间线可排查）。
	require.Contains(t, eventTypes(t, inner, d.ID), "split_child_create_failed")
}

// TestStartStageRunPropagatesLatestRunError：LatestStageRun 的非 NotFound 错误
// 不得被吞（吞掉会 attempt 回退 1、把 DB 故障伪装成首轮运行）——必须上抛失败。
func TestStartStageRunPropagatesLatestRunError(t *testing.T) {
	inner := store.NewMemory()
	fst := &failStore{Store: inner, failLatestRun: true}
	e := New(fst, &fakeRunner{}, &FakeWS{}, passTR{})
	d := seed(t, inner)

	err := e.Start(context.Background(), d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "latest stage run failed")
}

// childByTitle 按标题取父的子需求。
func childByTitle(t *testing.T, st *store.Memory, parentID, title string) *store.Delivery {
	t.Helper()
	children, err := st.ListChildDeliveries(context.Background(), parentID)
	require.NoError(t, err)
	for _, c := range children {
		if c.Title == title {
			return &c
		}
	}
	t.Fatalf("child %q not found", title)
	return nil
}
