package orchestration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		runner  string
		config  map[string]any
		wantErr string // 空 = 应通过
	}{
		{"cli 合法", "cli", map[string]any{"command": []any{"sh", "-c", "echo"}}, ""},
		{"cli 缺 command", "cli", nil, "config.command"},
		{"cli command 空数组", "cli", map[string]any{"command": []any{}}, "config.command"},
		{"cli command 类型错", "cli", map[string]any{"command": "echo"}, "config.command"},
		{"cli command 含非字符串", "cli", map[string]any{"command": []any{"sh", 1}}, "config.command"},
		{"http 合法", "http", map[string]any{"url": "http://localhost:9"}, ""},
		{"http 缺 url", "http", nil, "config.url"},
		{"http url 空串", "http", map[string]any{"url": ""}, "config.url"},
		{"docker 合法", "docker", map[string]any{"image": "infera-agent", "command": []any{"claude"}}, ""},
		{"docker 缺 image", "docker", map[string]any{"command": []any{"claude"}}, "config.image"},
		{"docker image 空串", "docker", map[string]any{"image": ""}, "config.image"},
		{"docker command 可选但类型要合法", "docker", map[string]any{"image": "x", "command": "claude"}, "config.command"},
		{"local 无额外要求", "local", nil, ""},
		{"local 空 config", "local", map[string]any{}, ""},
		{"未知 runner", "weird", nil, "runner"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfig(tc.runner, tc.config)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSaveBindings(t *testing.T) {
	st, p, a1, a2 := seedEnv(t)
	ctx := context.Background()
	seeded := bindingsSnapshot(t, st, "")

	// 未知节点 → 校验失败，不写库（design/tasks 现已是可绑定节点，取真不存在的名字）
	err := SaveBindings(ctx, st, "", map[string]string{"nonexistent_stage": a1.ID})
	var invalid *ErrInvalidBinding
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, err.Error(), "nonexistent_stage")
	require.Equal(t, seeded, bindingsSnapshot(t, st, ""), "校验失败不得写入")

	// 不存在的 agent → 校验失败，不写库
	err = SaveBindings(ctx, st, "", map[string]string{"spec": "00000000-0000-0000-0000-000000000000"})
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, err.Error(), "不存在")
	require.Equal(t, seeded, bindingsSnapshot(t, st, ""))

	// agent 存在但配置不合法（存量脏数据绕过了 API 预校验）→ 保存即报错
	bad := &store.Agent{Name: "legacy-bad", Runner: "cli"}
	require.NoError(t, st.CreateAgent(ctx, bad))
	err = SaveBindings(ctx, st, "", map[string]string{"spec": bad.ID})
	require.ErrorAs(t, err, &invalid)
	require.Contains(t, err.Error(), "config.command", "错误应写明缺的字段")
	require.Contains(t, err.Error(), "legacy-bad", "错误应写明哪个 agent 配置不合法")
	require.Equal(t, seeded, bindingsSnapshot(t, st, ""))

	// 合法保存：全量替换默认（spec→a2，其余→a1，含 R10 双道审查节点）
	full := map[string]string{"spec": a2.ID, "test_gen": a1.ID, "code_gen": a1.ID, "code_review": a1.ID,
		"spec_conformance": a1.ID, "code_quality": a1.ID}
	require.NoError(t, SaveBindings(ctx, st, "", full))
	require.Equal(t, full, bindingsSnapshot(t, st, ""))

	// 项目级：覆盖 + 清空
	require.NoError(t, SaveBindings(ctx, st, p.ID, map[string]string{"test_gen": a2.ID}))
	require.Equal(t, map[string]string{"test_gen": a2.ID}, bindingsSnapshot(t, st, p.ID))
	require.NoError(t, SaveBindings(ctx, st, p.ID, map[string]string{}))
	require.Empty(t, bindingsSnapshot(t, st, p.ID))

	// 项目级引用非法 agent 同样整体拒绝
	err = SaveBindings(ctx, st, p.ID, map[string]string{"spec": bad.ID})
	require.ErrorAs(t, err, &invalid)
	require.Empty(t, bindingsSnapshot(t, st, p.ID))
}

// bindingsSnapshot 把某项目的绑定折叠成 node→agentID。
func bindingsSnapshot(t *testing.T, st store.Store, projectID string) map[string]string {
	t.Helper()
	bs, err := st.ListBindings(context.Background(), projectID)
	require.NoError(t, err)
	out := map[string]string{}
	for _, b := range bs {
		out[b.Node] = b.AgentID
	}
	return out
}
