package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoadMCPToken(t *testing.T) {
	t.Setenv("INFERA_MCP_TOKEN", "")
	cfg, err := Load()
	require.NoError(t, err)
	if got := cfg.MCPToken; got != "" {
		t.Fatalf("未设置时 MCPToken 应为空（/mcp 禁用），got %q", got)
	}
	t.Setenv("INFERA_MCP_TOKEN", "secret-token")
	cfg, err = Load()
	require.NoError(t, err)
	if got := cfg.MCPToken; got != "secret-token" {
		t.Fatalf("MCPToken = %q, want %q", got, "secret-token")
	}
}

func TestLoadAddrPortFootgun(t *testing.T) {
	cases := []struct {
		name string
		port string // t.Setenv 的值；"" 表示 unset
		want string
	}{
		{"无前导冒号自动补", "8080", ":8080"},
		{"带冒号原样保留", ":9000", ":9000"},
		{"未设置用默认", "", ":8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PORT", tc.port)
			cfg, err := Load()
			require.NoError(t, err)
			if got := cfg.Addr; got != tc.want {
				t.Fatalf("Addr = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoadTaskSync：任务源接入三项全走环境变量、完全挂在现有 Load 机制上。
// ServerURL 绝不内置默认值（坑4），
// 空值交给 tasksource.New 显式报错，而不是回落到一个可能错误的地址）。
func TestLoadTaskSync(t *testing.T) {
	t.Run("未设置时全空不回落默认", func(t *testing.T) {
		t.Setenv("TASK_SYNC_SERVER_URL", "")
		t.Setenv("TASK_SYNC_TOKEN", "")
		t.Setenv("TASK_SYNC_WORKSPACE_ID", "")
		// 旧键也一并清空：运行环境可能带历史值（回退语义见 LegacyFallback 测试）。
		t.Setenv(legacyEnvPrefix+"SERVER_URL", "")
		t.Setenv(legacyEnvPrefix+"TOKEN", "")
		t.Setenv(legacyEnvPrefix+"WORKSPACE_ID", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.TaskSyncServerURL)
		require.Empty(t, cfg.TaskSyncToken)
		require.Empty(t, cfg.TaskSyncWorkspaceID)
	})

	t.Run("设置后原样透传", func(t *testing.T) {
		t.Setenv("TASK_SYNC_SERVER_URL", "http://localhost:8088")
		t.Setenv("TASK_SYNC_TOKEN", "mul_test-token")
		t.Setenv("TASK_SYNC_WORKSPACE_ID", "ws-uuid-1")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "http://localhost:8088", cfg.TaskSyncServerURL)
		require.Equal(t, "mul_test-token", cfg.TaskSyncToken)
		require.Equal(t, "ws-uuid-1", cfg.TaskSyncWorkspaceID)
	})
}

// TestLoadTaskSyncLegacyFallback：旧键兼容回退——新键未设置时按同后缀读旧键
// （现有 .env 不中断）。旧前缀字面量只在 config.go 的回退函数一出现，
// 测试经 legacyEnvPrefix 常量引用，不在测试里重复旧键名。
func TestLoadTaskSyncLegacyFallback(t *testing.T) {
	clearAll := func(t *testing.T) {
		t.Helper()
		for _, suffix := range []string{"SERVER_URL", "TOKEN", "WORKSPACE_ID", "PROJECT_ID", "TECH_LEAD_AGENT_ID", "WORKSPACE_SLUG"} {
			t.Setenv(taskSyncEnvPrefix+suffix, "")
			t.Setenv(legacyEnvPrefix+suffix, "")
		}
	}
	t.Run("仅旧键时回退读取", func(t *testing.T) {
		clearAll(t)
		t.Setenv(legacyEnvPrefix+"SERVER_URL", "http://localhost:8088")
		t.Setenv(legacyEnvPrefix+"TOKEN", "mul_legacy")
		t.Setenv(legacyEnvPrefix+"WORKSPACE_ID", "ws-old")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "http://localhost:8088", cfg.TaskSyncServerURL)
		require.Equal(t, "mul_legacy", cfg.TaskSyncToken)
		require.Equal(t, "ws-old", cfg.TaskSyncWorkspaceID)
	})

	t.Run("新键优先于旧键", func(t *testing.T) {
		clearAll(t)
		t.Setenv(legacyEnvPrefix+"SERVER_URL", "http://old:1")
		t.Setenv(taskSyncEnvPrefix+"SERVER_URL", "http://new:2")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "http://new:2", cfg.TaskSyncServerURL, "新键出现即以新键为准")
	})
}

// TestLoadTaskSyncInterval：周期轮询间隔键——默认 60s；"0"/"0s" = 关闭周期
// 轮询（启动同步仍执行）；非法 duration 与负值在配置期直接报错。
func TestLoadTaskSyncInterval(t *testing.T) {
	t.Run("未设置默认 60s", func(t *testing.T) {
		t.Setenv(taskSyncEnvPrefix+"INTERVAL", "")
		t.Setenv(legacyEnvPrefix+"INTERVAL", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 60*time.Second, cfg.TaskSyncInterval)
	})

	t.Run("显式间隔透传", func(t *testing.T) {
		t.Setenv(taskSyncEnvPrefix+"INTERVAL", "45s")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 45*time.Second, cfg.TaskSyncInterval)
	})

	t.Run("0 与 0s 均为关闭周期轮询", func(t *testing.T) {
		for _, v := range []string{"0", "0s"} {
			t.Setenv(taskSyncEnvPrefix+"INTERVAL", v)
			cfg, err := Load()
			require.NoError(t, err)
			require.Zero(t, cfg.TaskSyncInterval, "%q 应解析为 0（关闭）", v)
		}
	})

	t.Run("非法值与负值报错", func(t *testing.T) {
		for _, v := range []string{"abc", "-5s"} {
			t.Setenv(taskSyncEnvPrefix+"INTERVAL", v)
			_, err := Load()
			require.ErrorContains(t, err, "TASK_SYNC_INTERVAL", "%q 应配置期报错", v)
		}
	})

	t.Run("旧键回退同样生效", func(t *testing.T) {
		t.Setenv(taskSyncEnvPrefix+"INTERVAL", "")
		t.Setenv(legacyEnvPrefix+"INTERVAL", "2m")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, 2*time.Minute, cfg.TaskSyncInterval)
	})
}

// TestLoadTaskSyncProjectID：固定 project（FR-2 派发时 上游父 issue 归属的
// 固定项目）。同任务源接入面其余键：不设置为空（未启用固定项目语义），
// 设置后原样透传，不内置默认值。
func TestLoadTaskSyncProjectID(t *testing.T) {
	t.Run("未设置时为空", func(t *testing.T) {
		t.Setenv("TASK_SYNC_PROJECT_ID", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.TaskSyncProjectID)
	})

	t.Run("设置后原样透传", func(t *testing.T) {
		t.Setenv("TASK_SYNC_PROJECT_ID", "proj-uuid-1")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "proj-uuid-1", cfg.TaskSyncProjectID)
	})
}

// TestLoadDatabaseURL：开发模式缺省回落内置连接串；生产模式（INFERA_ENV=production）
// 必须显式设置——内置默认串连上错误的库比启动失败更危险。
func TestLoadDatabaseURL(t *testing.T) {
	t.Run("dev 默认回落", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("INFERA_ENV", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.NotEmpty(t, cfg.DatabaseURL)
	})

	t.Run("显式设置优先", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://u:p@h:5432/db")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "postgres://u:p@h:5432/db", cfg.DatabaseURL)
	})

	t.Run("生产模式强制显式", func(t *testing.T) {
		t.Setenv("DATABASE_URL", "")
		t.Setenv("INFERA_ENV", "production")
		_, err := Load()
		require.ErrorContains(t, err, "DATABASE_URL")

		t.Setenv("DATABASE_URL", "postgres://u:p@h:5432/prod")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "postgres://u:p@h:5432/prod", cfg.DatabaseURL)
	})
}

// TestLoadGitHub：token 走既有 GITHUB_TOKEN；API 入口可选经 GITHUB_API_URL 覆盖
// （GitHub Enterprise 等），默认空 → github.New 用 api.github.com 官方默认。
func TestLoadGitHub(t *testing.T) {
	t.Run("GITHUB_TOKEN 透传", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "ghp_test_token")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "ghp_test_token", cfg.GitHubToken)
	})

	t.Run("未设置 GITHUB_API_URL 时为空不回落", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.GitHubAPIURL, "空 = github.New 官方默认，不内置值")
	})

	t.Run("GITHUB_API_URL 显式覆盖", func(t *testing.T) {
		t.Setenv("GITHUB_API_URL", "https://github.example.com/api/v3")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "https://github.example.com/api/v3", cfg.GitHubAPIURL)
	})
}
