package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// seedEnv 建项目 + 两个 agent + 默认绑定（全节点→a1），返回 store。
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
		require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{Node: n, AgentID: a1.ID}))
	}
	return st, p, *a1, *a2
}

func TestResolveDefaultsAndOverride(t *testing.T) {
	st, p, a1, a2 := seedEnv(t)
	ctx := context.Background()

	// 无覆盖：全部来自 default，指向 a1
	ags, eff, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	for _, n := range BindableNodes {
		require.Equal(t, a1.ID, ags[n].ID)
		require.Equal(t, "default", eff[n].From)
	}

	// 项目覆盖 test_gen → a2
	require.NoError(t, st.UpsertBinding(ctx, &store.PipelineBinding{ProjectID: p.ID, Node: "test_gen", AgentID: a2.ID}))
	ags, eff, err = Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	require.Equal(t, a2.ID, ags["test_gen"].ID)
	require.Equal(t, "project", eff["test_gen"].From)
	require.Equal(t, "default", eff["spec"].From)
	require.Equal(t, a1.ID, ags["spec"].ID)
}

func TestResolveIncompleteBindings(t *testing.T) {
	st, p, _, _ := seedEnv(t)
	ctx := context.Background()

	require.NoError(t, st.DeleteBinding(ctx, "", "test_gen"))
	require.NoError(t, st.DeleteBinding(ctx, "", "code_review"))

	_, _, err := Resolve(ctx, st, p.ID)
	var incomplete *ErrIncompleteBindings
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, []string{"test_gen", "code_review"}, incomplete.Missing)
	require.Contains(t, err.Error(), "test_gen")

	// ValidateComplete 跟随 BindableNodes：缺任一节点即失败，全节点通过。
	partial := map[string]Effective{}
	for i, n := range BindableNodes {
		if i < len(BindableNodes)-1 {
			partial[n] = Effective{}
		}
	}
	require.Error(t, ValidateComplete(partial))
	full := map[string]Effective{}
	for _, n := range BindableNodes {
		full[n] = Effective{}
	}
	require.NoError(t, ValidateComplete(full))
}

// TestDesignTasksBindable（R11 冒烟阻塞点）：design/tasks 属 11 阶段链路的
// agent 节点，必须可绑定（SaveBindings/Resolve 全链路），否则真 agent 无法
// 接管 large 复杂度链路。
func TestDesignTasksBindable(t *testing.T) {
	st, p, a1, a2 := seedEnv(t)
	ctx := context.Background()

	// 可绑定：项目级保存 design/tasks 绑定成功，Resolve 解析得到。
	require.NoError(t, SaveBindings(ctx, st, p.ID, map[string]string{"design": a2.ID, "tasks": a2.ID}))
	ags, eff, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err)
	require.Equal(t, a2.ID, ags["design"].ID)
	require.Equal(t, a2.ID, ags["tasks"].ID)
	require.Equal(t, "project", eff["design"].From)
	require.Equal(t, a1.ID, ags["spec"].ID, "未覆盖的节点照常来自默认")
}

// TestLegacyBindingsWithoutDesignTasksNotBlocked：默认绑定兼容——升级前的
// 默认绑定只覆盖基准节点（无 design/tasks），Resolve 不得因此
// ErrIncompleteBindings；缺绑定的可选节点不出现在 agents（引擎回退兜底
// runner，而非 blocked）。基准节点缺失仍必须阻断。
func TestLegacyBindingsWithoutDesignTasksNotBlocked(t *testing.T) {
	st, p, _, _ := seedEnv(t)
	ctx := context.Background()

	// 模拟旧默认绑定：删掉 design/tasks 的默认绑定。
	require.NoError(t, st.DeleteBinding(ctx, "", "design"))
	require.NoError(t, st.DeleteBinding(ctx, "", "tasks"))

	ags, _, err := Resolve(ctx, st, p.ID)
	require.NoError(t, err, "旧默认绑定（无 design/tasks）不得报绑定不全")
	_, hasDesign := ags["design"]
	require.False(t, hasDesign, "未绑定的可选节点不出现在 agents（引擎走兜底）")

	// 基准节点（RequiredNodes）缺失仍阻断——兼容不得放松核心校验。
	require.NoError(t, st.DeleteBinding(ctx, "", "spec"))
	_, _, err = Resolve(ctx, st, p.ID)
	var incomplete *ErrIncompleteBindings
	require.ErrorAs(t, err, &incomplete)
	require.Equal(t, []string{"spec"}, incomplete.Missing)
}

// TestValidateCompleteRequiresOnlyRequiredNodes：ValidateComplete 只强校验
// 基准节点（旧语义跟随全集会迫使所有调用方重 PUT pipeline）。
func TestValidateCompleteRequiresOnlyRequiredNodes(t *testing.T) {
	requiredOnly := map[string]Effective{}
	for _, n := range RequiredNodes {
		requiredOnly[n] = Effective{}
	}
	require.NoError(t, ValidateComplete(requiredOnly))
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
