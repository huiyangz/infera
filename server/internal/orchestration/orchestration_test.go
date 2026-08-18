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
