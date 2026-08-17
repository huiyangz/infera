package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// markRunner 按角色回显标记（区分不同节点的执行器）。
type markRunner struct {
	mark  string
	calls []agent.Request
}

func (m *markRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	m.calls = append(m.calls, req)
	return agent.Result{Output: m.mark + ":" + req.Role}, nil
}

// TestResolveRunnerPerNode：ResolveRunner 按节点换执行器，未覆盖的节点回退构造时的 ar。
func TestResolveRunnerPerNode(t *testing.T) {
	st := store_Memory(t)
	fallback := &markRunner{mark: "fallback"}
	e := New(st, fallback, &FakeWS{}, passTR{})
	special := &markRunner{mark: "special"}
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "test_gen" {
			return special, nil
		}
		return nil, nil // 回退
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Approve(ctx, d.ID))
	require.NoError(t, e.Continue(ctx, d.ID))

	// spec/code_gen/code_review 走 fallback；test_gen 走 special。
	require.Equal(t, []string{"spec", "code_gen", "code_review"}, rolesOf(fallback))
	require.Equal(t, []string{"test_gen"}, rolesOf(special))
}

// TestLocalRunnerParksAtStage：节点绑定 local runner → 不跑 agent、
// 交付停在当前节点、发 local_stage_pending 事件，仍 active。
func TestLocalRunnerParksAtStage(t *testing.T) {
	st := store_Memory(t)
	ar := &markRunner{mark: "x"}
	e := New(st, ar, &FakeWS{}, passTR{})
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "spec" {
			return nil, orchestration.ErrLocalRunner
		}
		return nil, nil
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	got := get(t, st, d.ID)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, "spec", got.CurrentStage)
	require.Empty(t, got.PendingGate)
	require.Empty(t, rolesOf(ar), "local 绑定节点不应调用 agent")

	types := eventTypes(t, st, d.ID)
	require.Contains(t, types, "local_stage_pending")

	// 重驱动：仍停在 spec（每次驱动会重复发事件——已知可接受行为）。
	require.NoError(t, e.Continue(ctx, d.ID))
	got = get(t, st, d.ID)
	require.Equal(t, "spec", got.CurrentStage)
	require.Equal(t, StatusActive, got.Status)
}

// TestResolveErrorBlocks：绑定缺失等解析错误 → stage_failed（带缺哪个节点）+ blocked。
func TestResolveErrorBlocks(t *testing.T) {
	st := store_Memory(t)
	ar := &markRunner{mark: "x"}
	e := New(st, ar, &FakeWS{}, passTR{})
	resolveErr := &orchestration.ErrIncompleteBindings{Missing: []string{"test_gen"}}
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "test_gen" {
			return nil, resolveErr
		}
		return nil, nil
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Approve(ctx, d.ID)) // → test_gen
	err := e.Continue(ctx, d.ID)
	require.Error(t, err)
	var incomplete *orchestration.ErrIncompleteBindings
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, []string{"test_gen"}, incomplete.Missing)

	got := get(t, st, d.ID)
	require.Equal(t, StatusBlocked, got.Status)
	types := eventTypes(t, st, d.ID)
	require.Contains(t, types, "stage_failed")
	require.Contains(t, types, "delivery_blocked")
}

// TestLocalRunnerGateReviewSkipsPreview：code_review（门禁预审角色）绑定为 local →
// 跳过预审但门禁照常挂起（人工审查仍是停车点，预审产物留给批 B 的本机通道）。
func TestLocalRunnerGateReviewSkipsPreview(t *testing.T) {
	st := store_Memory(t)
	ar := &markRunner{mark: "x"}
	e := New(st, ar, &FakeWS{}, passTR{})
	e.ResolveRunner = func(_ context.Context, _, node string) (agent.Runner, error) {
		if node == "code_review" {
			return nil, orchestration.ErrLocalRunner
		}
		return nil, nil
	}
	d := seedEngine(t, st)
	ctx := context.Background()

	require.NoError(t, e.Start(ctx, d.ID))
	require.NoError(t, e.Approve(ctx, d.ID))
	require.NoError(t, e.Continue(ctx, d.ID))

	got := get(t, st, d.ID)
	require.Equal(t, "code_review", got.PendingGate)
	require.Equal(t, StatusActive, got.Status)
	require.Equal(t, []string{"spec", "test_gen", "code_gen"}, rolesOf(ar), "预审角色绑定 local 时不跑预审")
	require.Contains(t, eventTypes(t, st, d.ID), "local_stage_pending")
}

// --- 小工具（避免与 engine_test.go 现有 helper 命名冲突） ---

func store_Memory(t *testing.T) *store.Memory {
	t.Helper()
	return store.NewMemory()
}

func seedEngine(t *testing.T, st *store.Memory) *store.Delivery {
	t.Helper()
	return seed(t, st)
}

func rolesOf(r *markRunner) []string {
	out := make([]string, 0, len(r.calls))
	for _, c := range r.calls {
		out = append(out, c.Role)
	}
	return out
}
