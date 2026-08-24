package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// seedEnv 建项目 + 两个 agent + 项目级绑定（全节点→a1），返回 store。
func seedEnv(t *testing.T) (*store.Memory, *store.Project, store.Agent, store.Agent) {
	t.Helper()
	ctx := context.Background()
	st := store.NewMemory()
	p := &store.Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	a1 := &store.Agent{Name: "cli-1", Runner: "cli", Config: map[string]any{"command": []any{"echo", "hi"}}}
	a2 := &store.Agent{Name: "cli-2", Runner: "cli", Config: map[string]any{"command": []any{"echo", "yo"}}}
	require.NoError(t, st.CreateAgent(ctx, a1))
	require.NoError(t, st.CreateAgent(ctx, a2))
	for _, n := range BindableNodes {
		require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: p.ID, Node: n, AgentID: a1.ID}))
	}
	return st, p, *a1, *a2
}

func TestResolveProjectBindings(t *testing.T) {
	st, p, a1, a2 := seedEnv(t)
	ctx := context.Background()

	// 全节点项目绑定：全部解析到 a1
	ags, eff, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	for _, n := range BindableNodes {
		require.Equal(t, a1.ID, ags[n].ID)
		require.Equal(t, a1.ID, eff[n].AgentID)
		require.Equal(t, n, eff[n].Node)
	}

	// 换绑 test_gen → a2
	require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: p.ID, Node: "test_gen", AgentID: a2.ID}))
	ags, eff, err = Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	require.Equal(t, a2.ID, ags["test_gen"].ID)
	require.Equal(t, a2.ID, eff["test_gen"].AgentID)
	require.Equal(t, a1.ID, ags["spec"].ID)
}

// TestResolveWithoutBindings：项目无任何绑定时缺全部基准节点——全局默认已删除，
// 不存在任何兜底来源。
func TestResolveWithoutBindings(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	p := &store.Project{Name: "bare", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	a1 := &store.Agent{Name: "cli-1", Runner: "cli", Config: map[string]any{"command": []any{"echo"}}}
	require.NoError(t, st.CreateAgent(ctx, a1))

	_, _, err := Resolve(ctx, st, p.ID)
	var incomplete *ErrIncompleteBindings
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, RequiredNodes, incomplete.Missing)
}

// TestResolveIncompleteBindings：基准节点缺绑定即 ErrIncompleteBindings（按
// BindableNodes 序），可选节点（design/tasks）缺绑定不阻断。
func TestResolveIncompleteBindings(t *testing.T) {
	st, p, _, _ := seedEnv(t)
	ctx := context.Background()

	require.NoError(t, st.DeleteBinding(ctx, p.ID, "test_gen"))
	require.NoError(t, st.DeleteBinding(ctx, p.ID, "code_review"))
	// design/tasks 属可选节点：删掉不进 missing
	require.NoError(t, st.DeleteBinding(ctx, p.ID, "design"))
	require.NoError(t, st.DeleteBinding(ctx, p.ID, "tasks"))

	_, _, err := Resolve(ctx, st, p.ID)
	var incomplete *ErrIncompleteBindings
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, []string{"test_gen", "code_review"}, incomplete.Missing)
	require.Contains(t, err.Error(), "test_gen")
}

// TestDesignTasksBindable（R11 冒烟阻塞点）：design/tasks 属 11 阶段链路的
// agent 节点，必须可绑定（SaveBindings/Resolve 全链路），否则真 agent 无法
// 接管 large 复杂度链路。
func TestDesignTasksBindable(t *testing.T) {
	st, p, a1, a2 := seedEnv(t)
	ctx := context.Background()

	// 可绑定：项目级保存含 design/tasks 的全量绑定成功，Resolve 解析得到。
	full := map[string]string{}
	for _, n := range RequiredNodes {
		full[n] = a1.ID
	}
	full["design"], full["tasks"] = a2.ID, a2.ID
	require.NoError(t, SaveBindings(ctx, st, p.ID, full))
	ags, eff, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	require.Equal(t, a2.ID, ags["design"].ID)
	require.Equal(t, a2.ID, ags["tasks"].ID)
	require.Equal(t, a2.ID, eff["design"].AgentID)
}

// TestOptionalNodesSkippedWhenUnbound：项目只绑基准节点（无 design/tasks）时
// Resolve 不报绑定不全，可选节点不出现在 agents（引擎回退兜底 runner）。
func TestOptionalNodesSkippedWhenUnbound(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	p := &store.Project{Name: "legacy", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, p))
	a1 := &store.Agent{Name: "cli-1", Runner: "cli", Config: map[string]any{"command": []any{"echo"}}}
	require.NoError(t, st.CreateAgent(ctx, a1))
	for _, n := range RequiredNodes {
		require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: p.ID, Node: n, AgentID: a1.ID}))
	}

	ags, _, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err, "缺可选节点不得报绑定不全")
	_, hasDesign := ags["design"]
	require.False(t, hasDesign, "未绑定的可选节点不出现在 agents（引擎走兜底）")
}

func TestRunnerFor(t *testing.T) {
	// cli：command []any → argv
	r, err := RunnerFor(store.Agent{Name: "c", Runner: "cli", Config: map[string]any{"command": []any{"sh", "-c", "echo"}}})
	require.NoError(t, err)
	require.IsType(t, &agent.LocalRunner{}, r)
	// 缺 command → 错
	_, err = RunnerFor(store.Agent{Name: "c", Runner: "cli"})
	require.Error(t, err)

	// docker
	r, err = RunnerFor(store.Agent{Name: "d", Runner: "docker", Config: map[string]any{"image": "infera-agent", "command": []any{"claude"}}})
	require.NoError(t, err)
	require.IsType(t, &agent.DockerRunner{}, r)

	// http
	r, err = RunnerFor(store.Agent{Name: "h", Runner: "http", Config: map[string]any{"url": "http://localhost:9"}})
	require.NoError(t, err)
	require.IsType(t, &agent.HTTPRunner{}, r)

	// local → 哨兵
	_, err = RunnerFor(store.Agent{Name: "l", Runner: "local"})
	require.True(t, errors.Is(err, ErrLocalRunner))

	// 未知 runner
	_, err = RunnerFor(store.Agent{Name: "x", Runner: "weird"})
	require.Error(t, err)
}
