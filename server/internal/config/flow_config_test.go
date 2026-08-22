package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLoadGatePollInterval：闸门轮询间隔。
// 默认 30s；GATE_POLL_INTERVAL 显式配置时必须在 (0, 60s]——AC-3 要求
// 状态变化 2 分钟内反映，超过 60s 的轮询间隔直接配置期报错。
func TestLoadGatePollInterval(t *testing.T) {
	t.Run("默认 30s", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 30*time.Second, cfg.GatePollInterval)
	})

	t.Run("显式配置", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "45s")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 45*time.Second, cfg.GatePollInterval)
	})

	t.Run("60s 上限内合法", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "60s")
		_, err := Load()
		require.NoError(t, err)
	})

	t.Run("超过 60s 拒绝", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "90s")
		_, err := Load()
		require.ErrorContains(t, err, "GATE_POLL_INTERVAL")
	})

	t.Run("非正数拒绝", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "0s")
		_, err := Load()
		require.ErrorContains(t, err, "GATE_POLL_INTERVAL")
	})

	t.Run("非法 duration 拒绝", func(t *testing.T) {
		t.Setenv("GATE_POLL_INTERVAL", "soon")
		_, err := Load()
		require.ErrorContains(t, err, "GATE_POLL_INTERVAL")
	})
}

// TestLoadTaskSyncFlowAssemblyKeys：装配期（T07） reqservice.Options 需要的
// 两个定位值：Tech Lead agent id（派发指派）与 workspace slug（深链段）。
// 与其余 TASK_SYNC_* 键同语义：缺失 = 空串，必填性由构造器显式报错（不内置默认）。
func TestLoadTaskSyncFlowAssemblyKeys(t *testing.T) {
	t.Run("缺省为空", func(t *testing.T) {
		// 显式清空：e2e 环境可能带同名变量，测试必须对环境封闭。
		t.Setenv("TASK_SYNC_TECH_LEAD_AGENT_ID", "")
		t.Setenv("TASK_SYNC_WORKSPACE_SLUG", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.TaskSyncTechLeadAgentID)
		require.Empty(t, cfg.TaskSyncWorkspaceSlug)
	})
	t.Run("显式配置加载", func(t *testing.T) {
		t.Setenv("TASK_SYNC_TECH_LEAD_AGENT_ID", "9b45e9f4-0000-0000-0000-000000000000")
		t.Setenv("TASK_SYNC_WORKSPACE_SLUG", "infera")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "9b45e9f4-0000-0000-0000-000000000000", cfg.TaskSyncTechLeadAgentID)
		require.Equal(t, "infera", cfg.TaskSyncWorkspaceSlug)
	})
}
