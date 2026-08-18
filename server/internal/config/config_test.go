package config

import "testing"

func TestLoadMCPToken(t *testing.T) {
	t.Setenv("INFERA_MCP_TOKEN", "")
	if got := Load().MCPToken; got != "" {
		t.Fatalf("未设置时 MCPToken 应为空（/mcp 禁用），got %q", got)
	}
	t.Setenv("INFERA_MCP_TOKEN", "secret-token")
	if got := Load().MCPToken; got != "secret-token" {
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
			if tc.port == "" {
				t.Setenv("PORT", "")
			} else {
				t.Setenv("PORT", tc.port)
			}
			if got := Load().Addr; got != tc.want {
				t.Fatalf("Addr = %q, want %q", got, tc.want)
			}
		})
	}
}
