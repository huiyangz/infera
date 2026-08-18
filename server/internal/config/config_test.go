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
