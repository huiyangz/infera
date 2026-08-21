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

// TestLoadMulticaProjectID：派发目标 Multica 项目（固定项目）。
// 空默认（未固定）——是否必填由 reqservice 装配期决定，配置层不虚构默认值。
func TestLoadMulticaProjectID(t *testing.T) {
	t.Run("未设置时为空", func(t *testing.T) {
		t.Setenv("MULTICA_PROJECT_ID", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.MulticaProjectID)
	})

	t.Run("显式配置透传", func(t *testing.T) {
		t.Setenv("MULTICA_PROJECT_ID", "752455fa-7d33-4cb1-bfd6-f1f65f11a25e")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "752455fa-7d33-4cb1-bfd6-f1f65f11a25e", cfg.MulticaProjectID)
	})
}
