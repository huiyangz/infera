package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// errListAgentsErr ListAgents 失败的哨兵错误（ErrorIs 匹配用）。
var errListAgentsErr = errors.New("db: list agents failed")

// errListAgentsStore ListAgents 一律失败（其余透传）。
type errListAgentsStore struct {
	*store.Memory
}

func (e *errListAgentsStore) ListAgents(context.Context) ([]store.Agent, error) {
	return nil, errListAgentsErr
}

// TestSeedPropagatesAgentListError：种子流程读 agent 列表失败必须上抛——
// 吞错会继续用空 agent id 绑定出坏数据（误导性错误 + 半种子状态）。
func TestSeedPropagatesAgentListError(t *testing.T) {
	st := &errListAgentsStore{store.NewMemory()}
	err := seedDefaultOrchestration(context.Background(), st, "claude")
	require.ErrorIs(t, err, errListAgentsErr)
}

// TestSeedIdempotentWhenBindingsExist：已有默认绑定时不动（幂等，用户可能改过）。
func TestSeedIdempotentWhenBindingsExist(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	a := &store.Agent{Name: "custom", Runner: "cli", Config: map[string]any{"command": []any{"echo"}}}
	require.NoError(t, st.CreateAgent(ctx, a))
	require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{Node: "spec", AgentID: a.ID}))

	require.NoError(t, seedDefaultOrchestration(ctx, st, "claude"))
	agents, err := st.ListAgents(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1, "已有绑定时不再补种 agent")
	defs, err := st.ListBindings(ctx, "")
	require.NoError(t, err)
	require.Len(t, defs, 1)
	require.Equal(t, a.ID, defs[0].AgentID)
}
