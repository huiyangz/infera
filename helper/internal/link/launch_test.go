package link

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ctxLocal 构造 pending_local 停车的 get_context 结果。
func ctxLocal() *Context {
	var c Context
	if err := json.Unmarshal([]byte(sampleContext), &c); err != nil {
		panic(err)
	}
	return &c
}

// ctxGate 构造挂起 code_review 门禁、无本机停车节点的结果。
func ctxGate() *Context {
	c := ctxLocal()
	c.Delivery.PendingGate = "code_review"
	c.PendingLocal = nil
	return c
}

func TestMCPConfigClaude(t *testing.T) {
	got, err := MCPConfigFor("claude", "http://localhost:8080", "sekret")
	if err != nil {
		t.Fatalf("claude 配置不应报错: %v", err)
	}
	// 与 docs/mcp.md 的客户端配置同构（键序由 encoding/json 排序，确定性输出）
	want := `{"mcpServers":{"infera":{"headers":{"Authorization":"Bearer sekret"},"type":"http","url":"http://localhost:8080/mcp"}}}`
	if got != want {
		t.Errorf("claude MCP 配置不符:\n得到 %s\n期望 %s", got, want)
	}
	if err := json.Unmarshal([]byte(got), &map[string]any{}); err != nil {
		t.Errorf("应为合法 JSON: %v", err)
	}
}

func TestMCPConfigCodex(t *testing.T) {
	got, err := MCPConfigFor("codex", "http://localhost:8080", "sekret")
	if err != nil {
		t.Fatalf("codex 配置不应报错: %v", err)
	}
	for _, want := range []string{
		"[mcp_servers.infera]",
		`url = "http://localhost:8080/mcp"`,
		"[mcp_servers.infera.http_headers]",
		`Authorization = "Bearer sekret"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("codex TOML 缺少 %q:\n%s", want, got)
		}
	}
}

func TestMCPConfigInvalidCLI(t *testing.T) {
	if _, err := MCPConfigFor("pi", "http://localhost:8080", "t"); err == nil {
		t.Errorf("未知 CLI 应报错")
	}
}

func TestBuildPromptStageParking(t *testing.T) {
	p, err := BuildPrompt(ctxLocal())
	if err != nil {
		t.Fatalf("本机停车不应报错: %v", err)
	}
	for _, want := range []string{
		"d-1",                         // delivery_id 显式给出
		"你是资深工程师…",                    // 角色 prompt（与引擎同源）嵌入
		"/tmp/wd",                     // workdir
		"submit_stage_output",         // 交回通道
		"```infera-complexity",        // spec 节点产出契约
		"approve_gate", "reject_gate", // 后续门禁驾驶
	} {
		if !strings.Contains(p, want) {
			t.Errorf("阶段提示缺少 %q", want)
		}
	}
}

func TestBuildPromptCodeGenContract(t *testing.T) {
	c := ctxLocal()
	c.PendingLocal.Node = "code_gen"
	p, err := BuildPrompt(c)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !strings.Contains(p, "改动摘要") {
		t.Errorf("code_gen 契约应说明 output=改动摘要、代码改动直接做在 workdir")
	}
}

func TestBuildPromptGateReview(t *testing.T) {
	p, err := BuildPrompt(ctxGate())
	if err != nil {
		t.Fatalf("门禁形态不应报错: %v", err)
	}
	for _, want := range []string{"get_gate", "approve_gate", "reject_gate", "code_review"} {
		if !strings.Contains(p, want) {
			t.Errorf("门禁提示缺少 %q", want)
		}
	}
}

func TestBuildPromptNotParked(t *testing.T) {
	c := ctxLocal()
	c.PendingLocal = nil
	c.Delivery.PendingGate = ""
	if _, err := BuildPrompt(c); err == nil {
		t.Errorf("非本机停车且无门禁应报错（拉起无意义的会话）")
	}
}

func TestBuildCommandClaude(t *testing.T) {
	got := BuildCommand("claude", CommandInputs{
		Workdir: "/tmp/wd", MCPConfigPath: "/stage/mcp.json", PromptPath: "/stage/prompt.txt",
		Endpoint: "http://localhost:8080/mcp", Token: "sekret",
	})
	want := `cd '/tmp/wd' && claude --mcp-config '/stage/mcp.json' --allowed-tools 'mcp__infera__*' "$(cat '/stage/prompt.txt')"`
	if got != want {
		t.Errorf("claude 命令不符:\n得到 %s\n期望 %s", got, want)
	}
}

func TestBuildCommandCodex(t *testing.T) {
	got := BuildCommand("codex", CommandInputs{
		Workdir: "/tmp/wd", PromptPath: "/stage/prompt.txt",
		Endpoint: "http://localhost:8080/mcp", Token: "sekret",
	})
	for _, want := range []string{
		"cd '/tmp/wd' && codex",
		`-c mcp_servers.infera.url='http://localhost:8080/mcp'`,
		`-c mcp_servers.infera.http_headers.Authorization='Bearer sekret'`,
		`"$(cat '/stage/prompt.txt')"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("codex 命令缺少 %q:\n%s", want, got)
		}
	}
}

func TestBuildCommandQuotesPaths(t *testing.T) {
	got := BuildCommand("claude", CommandInputs{
		Workdir: "/tmp/w'd", MCPConfigPath: "/s/mcp.json", PromptPath: "/s/p.txt",
		Endpoint: "http://localhost:8080/mcp", Token: "t",
	})
	if !strings.Contains(got, `'/tmp/w'\''d'`) {
		t.Errorf("路径中的单引号应转义:\\n%s", got)
	}
}

func TestOsascriptSource(t *testing.T) {
	src := osascriptSource(`cd '/tmp/wd' && claude 'x'`)
	for _, want := range []string{"tell application \"Terminal\"", `do script "cd '/tmp/wd' && claude 'x'"`} {
		if !strings.Contains(src, want) {
			t.Errorf("osascript 源缺少 %q: %s", want, src)
		}
	}
	if strings.Count(src, "\"")%2 != 0 {
		t.Errorf("AppleScript 字符串引数应配对: %s", src)
	}
	// 命令含双引号/反斜杠时做 AppleScript 转义，不破坏外层字符串
	escaped := osascriptSource(`echo "hi\n"`)
	if !strings.Contains(escaped, `do script "echo \"hi\\n\""`) {
		t.Errorf("双引号与反斜杠应转义: %s", escaped)
	}
}

func TestPlanStagesFiles(t *testing.T) {
	cfg := Config{Server: "http://localhost:8080", Token: "sekret", CLI: "claude"}
	plan, err := Plan(ctxLocal(), cfg, "/stage")
	if err != nil {
		t.Fatalf("Plan 不应报错: %v", err)
	}
	if plan.Node != "spec" {
		t.Errorf("Node 应取 pending_local.node，得到 %q", plan.Node)
	}
	if plan.Workdir != "/tmp/wd" {
		t.Errorf("Workdir 应取 repo.workdir，得到 %q", plan.Workdir)
	}
	if !strings.Contains(plan.Command, "--mcp-config '/stage/mcp.json'") {
		t.Errorf("命令应引用落盘的 MCP 配置: %s", plan.Command)
	}
	// 两个文件都应有内容且配置合法
	if !strings.Contains(plan.MCPConfig, `"url":"http://localhost:8080/mcp"`) {
		t.Errorf("MCP 配置内容不符: %s", plan.MCPConfig)
	}
	if !strings.Contains(plan.Prompt, "submit_stage_output") {
		t.Errorf("提示应含交回指引")
	}
}

func TestPlanNotParked(t *testing.T) {
	c := ctxLocal()
	c.PendingLocal = nil
	c.Delivery.PendingGate = ""
	if _, err := Plan(c, Config{CLI: "claude"}, "/stage"); err == nil {
		t.Errorf("非本机停车应报错")
	}
}

var _ = context.Background // 保持 context 引用（daemon 测试复用本文件的构造器）
