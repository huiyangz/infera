package link

import "testing"

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(nil, func(string) string { return "" })
	if err != nil {
		t.Fatalf("默认加载不应报错: %v", err)
	}
	if cfg.Server != "http://localhost:8080" {
		t.Errorf("Server 默认应为 http://localhost:8080，得到 %q", cfg.Server)
	}
	if cfg.Listen != "127.0.0.1:8788" {
		t.Errorf("Listen 默认应为 127.0.0.1:8788，得到 %q", cfg.Listen)
	}
	if cfg.CLI != "claude" {
		t.Errorf("CLI 默认应为 claude，得到 %q", cfg.CLI)
	}
	if cfg.Terminal != "auto" {
		t.Errorf("Terminal 默认应为 auto，得到 %q", cfg.Terminal)
	}
	if cfg.Token != "" {
		t.Errorf("Token 默认应为空（daemon 可启动，/handle 时给可操作错误）")
	}
}

func TestLoadEnvFallback(t *testing.T) {
	env := map[string]string{
		"INFERA_URL":       "http://127.0.0.1:9090",
		"INFERA_MCP_TOKEN": "sekret",
		"INFERA_LINK_ADDR": "127.0.0.1:9000",
		"INFERA_LINK_CLI":  "codex",
		"INFERA_LINK_TERM": "none",
	}
	cfg, err := Load(nil, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("env 加载不应报错: %v", err)
	}
	if cfg.Server != "http://127.0.0.1:9090" || cfg.Token != "sekret" ||
		cfg.Listen != "127.0.0.1:9000" || cfg.CLI != "codex" || cfg.Terminal != "none" {
		t.Errorf("env 未生效: %+v", cfg)
	}
}

func TestLoadFlagOverridesEnv(t *testing.T) {
	env := map[string]string{"INFERA_URL": "http://env:8080", "INFERA_MCP_TOKEN": "envtok"}
	cfg, err := Load([]string{"--server", "http://flag:8080"}, func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("flag 加载不应报错: %v", err)
	}
	if cfg.Server != "http://flag:8080" {
		t.Errorf("flag 应覆盖 env，得到 %q", cfg.Server)
	}
	if cfg.Token != "envtok" {
		t.Errorf("未指定的项应回退 env，token=%q", cfg.Token)
	}
}

func TestLoadRejectsInvalid(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{"cli 枚举", []string{"--cli", "pi"}, nil},
		{"terminal 枚举", []string{"--terminal", "xterm"}, nil},
		{"server 非法 URL", []string{"--server", "not a url"}, nil},
		{"server 缺 scheme", []string{"--server", "localhost:8080"}, nil},
		{"listen 非法", []string{"--listen", "0.0.0.0:0:0"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Load(c.args, func(k string) string { return c.env[k] }); err == nil {
				t.Errorf("应报错却通过了")
			}
		})
	}
}

func TestMCPEndpoint(t *testing.T) {
	cfg := Config{Server: "http://localhost:8080"}
	if got := cfg.MCPEndpoint(); got != "http://localhost:8080/mcp" {
		t.Errorf("MCPEndpoint 应为 server+/mcp，得到 %q", got)
	}
	// 尾斜杠容错
	cfg.Server = "http://localhost:8080/"
	if got := cfg.MCPEndpoint(); got != "http://localhost:8080/mcp" {
		t.Errorf("尾斜杠应被容错，得到 %q", got)
	}
}
