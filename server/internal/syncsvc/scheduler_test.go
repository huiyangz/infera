package syncsvc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// schedFetch 调度器测试的可脚本化拉取替身：轮次计数、可注入错误、
// 内容可中途切换（全部经互斥锁，-race 下多轮并发安全）。
type schedFetch struct {
	mu       sync.Mutex
	projects []tasksource.Project
	issues   []tasksource.Issue
	err      error
	rounds   int // ListProjects 调用次数（一轮同步恰一次）
}

func (f *schedFetch) ListProjects(_ context.Context) ([]tasksource.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rounds++
	if f.err != nil {
		return nil, f.err
	}
	return f.projects, nil
}

func (f *schedFetch) ListIssues(_ context.Context) ([]tasksource.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.issues, nil
}

// ListProjectResources 恒无绑定（repo_url 保留现值）——调度器只关心轮次与错误。
func (f *schedFetch) ListProjectResources(_ context.Context, _ string) ([]tasksource.ProjectResource, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func (f *schedFetch) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *schedFetch) roundsDone() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rounds
}

// waitFor 轮询等待条件成立（调度器测试的确定性助手）。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("条件在 %s 内未满足: %s", timeout, msg)
}

func newSchedSvc(f *schedFetch) (*Service, *store.Memory) {
	st := store.NewMemory()
	return New(f, st), st
}

// --- AC: 服务端进程启动即自动同步一次（异步，不阻塞启动） ---

func TestSchedulerStartupSyncRuns(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, st := newSchedSvc(f)

	sched := NewScheduler(svc, 0)
	startAt := time.Now()
	require.NoError(t, sched.Start(context.Background()))
	require.Less(t, time.Since(startAt), 500*time.Millisecond, "Start 必须立即返回（启动同步异步执行，不阻塞）")

	waitFor(t, 2*time.Second, func() bool { return svc.Last() != nil }, "启动同步应完成一轮")
	require.Equal(t, 1, svc.Last().ProjectsImported)
	projs, err := st.ListProjects(context.Background())
	require.NoError(t, err)
	require.Len(t, projs, 1, "启动同步应把项目落库")
	sched.Stop()
}

// --- AC: 周期轮询按配置间隔生效；间隔 0 = 关闭周期轮询（启动同步仍执行） ---

func TestSchedulerPeriodicRounds(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, _ := newSchedSvc(f)

	sched := NewScheduler(svc, 10*time.Millisecond)
	require.NoError(t, sched.Start(context.Background()))
	defer sched.Stop()
	waitFor(t, 2*time.Second, func() bool { return f.roundsDone() >= 3 }, "周期轮询应连续执行多轮")
}

func TestSchedulerZeroIntervalOnlyStartupRound(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, _ := newSchedSvc(f)

	sched := NewScheduler(svc, 0)
	require.NoError(t, sched.Start(context.Background()))
	defer sched.Stop()
	waitFor(t, 2*time.Second, func() bool { return f.roundsDone() >= 1 }, "启动同步仍应执行")
	time.Sleep(60 * time.Millisecond)
	require.Equal(t, 1, f.roundsDone(), "间隔 0 = 关闭周期轮询，只跑启动一轮")
}

// --- AC: 同步失败记录错误继续运行，不 fatal ---

func TestSchedulerFailureRecordedAndKeepsRunning(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, _ := newSchedSvc(f)

	sched := NewScheduler(svc, 10*time.Millisecond)
	require.NoError(t, sched.Start(context.Background()))
	defer sched.Stop()

	// 首轮失败：错误落在 Last()，调度器不退出。
	f.setErr(errors.New("upstream down"))
	waitFor(t, 2*time.Second, func() bool {
		last := svc.Last()
		return last != nil && last.Error != ""
	}, "失败轮的错误应记入 Last()")

	// 恢复上游：下一轮周期同步照常成功——失败不 fatal，循环仍在。
	f.setErr(nil)
	waitFor(t, 2*time.Second, func() bool {
		last := svc.Last()
		return last != nil && last.Error == "" && last.ProjectsImported == 1
	}, "错误恢复后下一轮应成功")
}

// --- 生命周期：重复启动报错；Stop 幂等且停后不再跑新轮 ---

func TestSchedulerLifecycle(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, _ := newSchedSvc(f)

	sched := NewScheduler(svc, 10*time.Millisecond)
	require.NoError(t, sched.Start(context.Background()))
	require.Error(t, sched.Start(context.Background()), "重复启动必须报错")
	waitFor(t, 2*time.Second, func() bool { return f.roundsDone() >= 1 }, "至少完成一轮")
	sched.Stop()
	sched.Stop() // 幂等
	base := f.roundsDone()
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, base, f.roundsDone(), "Stop 后不得再执行新轮")
}
