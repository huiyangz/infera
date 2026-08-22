package syncsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// --- AC: 同步状态接口语义（lastSyncAt / status / error，冻结契约） ---

func TestStatusNeverSynced(t *testing.T) {
	svc, _ := newSchedSvc(&schedFetch{})
	st := svc.Status()
	require.Equal(t, StatusIdle, st.Status)
	require.Nil(t, st.LastSyncAt, "从未同步过 lastSyncAt 为 null")
	require.Empty(t, st.Error)
}

func TestStatusAfterSuccess(t *testing.T) {
	f := &schedFetch{projects: []tasksource.Project{proj("m-prj-1", "自动闭环")}}
	svc, _ := newSchedSvc(f)
	_, err := svc.SyncNow(context.Background())
	require.NoError(t, err)

	st := svc.Status()
	require.Equal(t, StatusSuccess, st.Status)
	require.NotNil(t, st.LastSyncAt, "成功后 lastSyncAt 更新")
	require.Empty(t, st.Error)
}

func TestStatusAfterFailure(t *testing.T) {
	f := &schedFetch{err: errors.New("upstream 500")}
	svc, _ := newSchedSvc(f)
	_, err := svc.SyncNow(context.Background())
	require.Error(t, err)

	st := svc.Status()
	require.Equal(t, StatusError, st.Status)
	require.NotNil(t, st.LastSyncAt)
	require.Contains(t, st.Error, "upstream 500", "失败原因进入 error 字段")
}

func TestStatusRunningShowsLastCompleted(t *testing.T) {
	f := &fakeFetch{
		projects: []tasksource.Project{{ID: "m-prj-1", Title: "自动闭环", Status: "in_progress", UpdatedAt: time.Now()}},
	}
	svc := New(f, store.NewMemory())

	// 第一轮：正常完成（channels 为 nil，拉取面不设门）。
	_, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	first := svc.Status()
	require.Equal(t, StatusSuccess, first.Status)

	// 第二轮：拉取面挂起（entered/release 门）→ running。
	f.entered = make(chan struct{})
	f.release = make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.SyncNow(context.Background())
	}()
	<-f.entered
	t.Cleanup(func() { close(f.release); <-done })

	st := svc.Status()
	require.Equal(t, StatusRunning, st.Status)
	require.NotNil(t, st.LastSyncAt, "running 期间 lastSyncAt 描述最近完成的一轮")
	require.Equal(t, *first.LastSyncAt, *st.LastSyncAt)
}
