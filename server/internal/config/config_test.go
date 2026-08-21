package config

import (
	"testing"

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

// TestLoadMultica：multica 接入三项全走环境变量、完全挂在现有 Load 机制上。
// ServerURL 绝不内置默认值（坑4：本机默认 profile 指向云端 api.multica.ai，
// 误配必须可检出——空值交给 multica.New 显式报错，而不是回落到一个可能错误的地址）。
func TestLoadMultica(t *testing.T) {
	t.Run("未设置时全空不回落默认", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "")
		t.Setenv("MULTICA_TOKEN", "")
		t.Setenv("MULTICA_WORKSPACE_ID", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.MulticaServerURL)
		require.Empty(t, cfg.MulticaToken)
		require.Empty(t, cfg.MulticaWorkspaceID)
	})

	t.Run("设置后原样透传", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "http://localhost:8088")
		t.Setenv("MULTICA_TOKEN", "mul_test-token")
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-uuid-1")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "http://localhost:8088", cfg.MulticaServerURL)
		require.Equal(t, "mul_test-token", cfg.MulticaToken)
		require.Equal(t, "ws-uuid-1", cfg.MulticaWorkspaceID)
	})
}

// TestLoadMulticaProjectID：固定 project（FR-2 派发时 Multica 父 issue 归属的
// 固定项目）。同 multica 接入面其余键：不设置为空（未启用固定项目语义），
// 设置后原样透传，不内置默认值。
func TestLoadMulticaProjectID(t *testing.T) {
	t.Run("未设置时为空", func(t *testing.T) {
		t.Setenv("MULTICA_PROJECT_ID", "")
		cfg, err := Load()
		require.NoError(t, err)
		require.Empty(t, cfg.MulticaProjectID)
	})

	t.Run("设置后原样透传", func(t *testing.T) {
		t.Setenv("MULTICA_PROJECT_ID", "proj-uuid-1")
		cfg, err := Load()
		require.NoError(t, err)
		require.Equal(t, "proj-uuid-1", cfg.MulticaProjectID)
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
