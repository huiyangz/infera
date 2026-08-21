package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/config"
)

// fullFlowConfig 返回完整接入配置（真实值形态，非占位）：multica 三键 +
// 装配两键 + github token。单测用它派生缺项 / 误配场景。
func fullFlowConfig() config.Config {
	return config.Config{
		MulticaServerURL:       "http://localhost:8088",
		MulticaToken:           "mat_test-token",
		MulticaWorkspaceID:     "0192eeee-0000-7000-8000-000000000000",
		MulticaProjectID:       "0192eeee-0000-7000-8000-000000000001",
		MulticaTechLeadAgentID: "0192eeee-0000-7000-8000-000000000002",
		MulticaWorkspaceSlug:   "infera",
		GitHubToken:            "ghp_test-token",
		GatePollInterval:       30 * time.Second,
	}
}

// TestFlowConfigured：全部 MULTICA_* 流转键为空 = 未接入（不装配、不报错）；
// 任一键出现即视为尝试接入——半配不该静默降级，交给构造器显式报错。
func TestFlowConfigured(t *testing.T) {
	require.False(t, flowConfigured(config.Config{}), "全空 = 未接入")

	for name, mutate := range map[string]func(*config.Config){
		"ServerURL":     func(c *config.Config) { c.MulticaServerURL = "http://localhost:8088" },
		"Token":         func(c *config.Config) { c.MulticaToken = "t" },
		"WorkspaceID":   func(c *config.Config) { c.MulticaWorkspaceID = "w" },
		"ProjectID":     func(c *config.Config) { c.MulticaProjectID = "p" },
		"TechLeadAgent": func(c *config.Config) { c.MulticaTechLeadAgentID = "a" },
		"WorkspaceSlug": func(c *config.Config) { c.MulticaWorkspaceSlug = "s" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := config.Config{}
			mutate(&cfg)
			require.True(t, flowConfigured(cfg), "任一流转键出现即视为尝试接入")
		})
	}
}

// TestAssembleFlowNotConfigured：未接入时返回双 nil 无错——main 据此不注入
// reqservice（需求路由 503）也不启动轮询器，裸开发启动不受影响。
func TestAssembleFlowNotConfigured(t *testing.T) {
	reqSvc, poller, err := assembleFlow(nil, config.Config{})
	require.NoError(t, err)
	require.Nil(t, reqSvc)
	require.Nil(t, poller)
}

// TestAssembleFlowMulticaMisconfig：multica 半配必须拿到构造器的显式报错
// （哪一项缺、为什么），而不是静默不装配或运行期难排查的 401。
func TestAssembleFlowMulticaMisconfig(t *testing.T) {
	cfg := fullFlowConfig()
	cfg.MulticaToken = "" // 有 ServerURL 无 Token：multica.New 显式报错
	_, _, err := assembleFlow(nil, cfg)
	require.ErrorContains(t, err, "multica: Token 缺失")
}

// TestAssembleFlowGitHubTokenRequired：接入流转必须有 GITHUB_TOKEN
// （reqservice 合并动作与 gatepoll 自动合并共用）。
func TestAssembleFlowGitHubTokenRequired(t *testing.T) {
	cfg := fullFlowConfig()
	cfg.GitHubToken = ""
	_, _, err := assembleFlow(nil, cfg)
	require.ErrorContains(t, err, "github: Token 缺失")
}

// TestAssembleFlowPollIntervalValidated：轮询器以 cfg.GatePollInterval 构造，
// gatepoll.New 的 (0, 60s] 校验必须在装配期生效（AC-3 语义防线的最后一道）。
func TestAssembleFlowPollIntervalValidated(t *testing.T) {
	cfg := fullFlowConfig()
	cfg.GatePollInterval = 0
	_, _, err := assembleFlow(nil, cfg)
	require.ErrorContains(t, err, "interval")
}

// TestAssembleFlowReachesReqservice：全配置 + 缺连接池 → 报 reqservice 的
// 连接池错误：证明 multica / github client 与 poller 均已构造成功、装配链
// 完整走到了 reqservice（Options 必填项校验在 pool 检查之后由 reqservice
// 自测覆盖；真实 pool 的全链路由 server/test 流转 e2e 覆盖）。
func TestAssembleFlowReachesReqservice(t *testing.T) {
	_, _, err := assembleFlow(nil, fullFlowConfig())
	require.ErrorContains(t, err, "reqservice: 连接池缺失")
}
