package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// resumeEngine 记录并发驱动数的引擎替身：Continue 模拟一次有耗时的推进
// （并把交付置 completed 终止 driveLocked 循环），Start 同 Continue。
type resumeEngine struct {
	st   store.Store
	mu   sync.Mutex
	cur  int
	maxI int
}

func (e *resumeEngine) enter() {
	e.mu.Lock()
	e.cur++
	if e.cur > e.maxI {
		e.maxI = e.cur
	}
	e.mu.Unlock()
	time.Sleep(25 * time.Millisecond)
}

func (e *resumeEngine) exit() {
	e.mu.Lock()
	e.cur--
	e.mu.Unlock()
}

func (e *resumeEngine) drive(ctx context.Context, id string) error {
	e.enter()
	defer e.exit()
	d, err := e.st.GetDelivery(ctx, id)
	if err != nil {
		return err
	}
	d.Status = "completed"
	return e.st.UpdateDelivery(ctx, d)
}

func (e *resumeEngine) Start(ctx context.Context, id string) error       { return e.drive(ctx, id) }
func (e *resumeEngine) Continue(ctx context.Context, id string) error    { return e.drive(ctx, id) }
func (e *resumeEngine) ResumeMerge(ctx context.Context, id string) error { return nil }
func (e *resumeEngine) MaybeDriveParent(ctx context.Context, id string) error {
	return e.drive(ctx, id)
}

func (e *resumeEngine) Approve(ctx context.Context, id string, opts store.ApproveOpts) ([]store.Delivery, error) {
	return nil, nil
}
func (e *resumeEngine) Reject(ctx context.Context, id string, reason string) error { return nil }

// TestResumeActiveConcurrencyCapped：重启恢复的点火并发有上限——
// 上百 active 交付同时驱动会打爆 agent 后端/DB；超上限的排队放行。
func TestResumeActiveConcurrencyCapped(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	p := &store.Project{Name: "p", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	const n = 20
	for i := 0; i < n; i++ {
		require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{
			ProjectID: p.ID, Title: "d", Status: "active", CurrentStage: "spec",
		}))
	}
	fe := &resumeEngine{st: st}
	srv := NewServer(st, "pw", fe)

	srv.ResumeActive(ctx)

	// 全部恢复完成（每个交付都被驱动且置 completed）。
	require.Eventually(t, func() bool {
		ds, err := st.ListActiveDeliveries(ctx)
		return err == nil && len(ds) == 0
	}, 10*time.Second, 50*time.Millisecond, "全部 active 交付应被恢复驱动")

	fe.mu.Lock()
	maxI := fe.maxI
	fe.mu.Unlock()
	require.LessOrEqual(t, maxI, maxResumeConcurrency, "恢复驱动并发不得超过上限")
	require.GreaterOrEqual(t, maxI, 2, "并发确实发生（多交付同时驱动）")
}
