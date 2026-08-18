package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// hangRunner 一直阻塞到 ctx 取消（模拟卡死的 CLI / 挂起的远端 agent）。
type hangRunner struct{}

func (hangRunner) Run(ctx context.Context, _ agent.Request) (agent.Result, error) {
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}

// TestAgentRunTimeoutBlocks：agent 调用超过总时限 → 按 agent 失败约定收场
// （stage_failed 事件带超时说明 + blocked 终态 + 释放 workspace），驱动错误上抛。
func TestAgentRunTimeoutBlocks(t *testing.T) {
	old := agentRunTimeout
	agentRunTimeout = 30 * time.Millisecond
	t.Cleanup(func() { agentRunTimeout = old })

	st := store.NewMemory()
	e := New(st, hangRunner{}, &FakeWS{}, passTR{})
	d := seed(t, st)

	err := e.Start(context.Background(), d.ID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout")

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	require.Contains(t, eventTypes(t, st, d.ID), "stage_failed")

	// 超时事件写明是超时（区分于 agent 自身报错）。
	evs, _ := st.ListEvents(context.Background(), d.ID)
	var failed *store.Event
	for i := range evs {
		if evs[i].EventType == "stage_failed" {
			failed = &evs[i]
		}
	}
	require.NotNil(t, failed)
	require.Contains(t, string(failed.Payload), "timeout")
}

// TestAgentRunWithinTimeoutPasses：时限内完成的调用不受影响。
func TestAgentRunWithinTimeoutPasses(t *testing.T) {
	old := agentRunTimeout
	agentRunTimeout = 5 * time.Second
	t.Cleanup(func() { agentRunTimeout = old })

	e, st, _, _ := newEnv(t, passTR{})
	d := seed(t, st)
	require.NoError(t, e.Start(context.Background(), d.ID))
	require.Equal(t, "spec_approval", get(t, st, d.ID).PendingGate)
	require.NotContains(t, eventTypes(t, st, d.ID), "stage_failed")
}
