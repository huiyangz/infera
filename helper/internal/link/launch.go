// 拉起准备：把 get_context 的结果变成「本机 CLI 会话」的三件套——
// MCP 客户端配置（claude JSON / codex TOML，形态与 docs/mcp.md 客户端配置一致）、
// 初始提示（阶段指引与引擎发给绑定 agent 的同源 + 交回契约）、终端命令。
// 全部为纯函数：文件落盘与终端拉起由 daemon 做，便于单测。
package link

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MCPConfigFor 生成本机 CLI 的 MCP 客户端配置内容。
// claude：--mcp-config 吃的 JSON（一次性使用，不落 ~/.claude.json）；
// codex：config.toml 片段（codex 无 --mcp-config 入参，daemon 用 -c 内联覆盖启动，
// 该片段供用户按需并入 ~/.codex/config.toml 做持久注册）。
func MCPConfigFor(cli, server, token string) (string, error) {
	endpoint := strings.TrimRight(server, "/") + "/mcp"
	switch cli {
	case "claude":
		b, err := json.Marshal(map[string]any{
			"mcpServers": map[string]any{
				"infera": map[string]any{
					"type":    "http",
					"url":     endpoint,
					"headers": map[string]string{"Authorization": "Bearer " + token},
				},
			},
		})
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "codex":
		return fmt.Sprintf(
			"[mcp_servers.infera]\nurl = %q\n\n[mcp_servers.infera.http_headers]\nAuthorization = %q\n",
			endpoint, "Bearer "+token,
		), nil
	default:
		return "", fmt.Errorf("不支持的 CLI %q（claude | codex）", cli)
	}
}

// stageContract 各停车节点 submit_stage_output 的 output 契约速查（详版在角色 prompt 里）。
func stageContract(node string) string {
	switch node {
	case "spec":
		return "output 为规格文档全文，末尾另起一行附 ```infera-complexity fenced block（内容仅一行：small 或 large）"
	case "design":
		return "output 为设计文档全文"
	case "tasks":
		return "output 需含 ```infera-tasks fenced block（JSON 数组任务清单）"
	case "test_gen":
		return "output 为测试产出全文（按阶段指引生成）"
	case "code_gen":
		return "代码改动直接做在 workdir 检出里，output 为改动摘要"
	case "code_review":
		return "output 为你的预审意见全文（门禁的放行/打回另用 approve_gate / reject_gate 裁定）"
	default:
		return "output 为按阶段指引产出的全文"
	}
}

// BuildPrompt 组装本机 CLI 的初始提示。两种合法形态（与 engine.LocalPrompt 对齐）：
// 停在本机绑定节点（pending_local）→ 阶段工作 + 交回指引；
// 挂起人工门禁（无本机停车节点）→ 门禁审查驾驶。
// 两者皆非（交付未停在本机）→ 报错：拉起会话没有意义。
func BuildPrompt(c *Context) (string, error) {
	id := c.Delivery.ID
	if c.PendingLocal != nil {
		p := c.PendingLocal.Prompt
		return fmt.Sprintf(`你在本机驾驶 infera 交付流水线的一个阶段。

## 交付
- delivery_id：%s（调用 MCP 工具时的参数都用它）
- 需求：%s——%s
- 当前停车节点：%s（local 绑定，流水线停在本机等待处理）

## 仓库与工作区
- workdir：%s（终端已定位到该目录；代码改动直接做在这个检出里）
- %s

## 阶段指引（与引擎发给绑定 agent 的指令同源）
%s

## 完成后交回
调用 MCP 工具 submit_stage_output：delivery_id=%s，output=阶段产出。
产出契约——%s 阶段：%s。
交回后流水线自动推进；推进到人工门禁时用 get_gate 查看详情、approve_gate / reject_gate 裁定（打回 reason 会注入重跑反馈）。`,
			id, c.Delivery.Title, c.Delivery.Description,
			c.PendingLocal.Node,
			c.Repo.Workdir, c.Repo.Convention,
			p,
			id, c.PendingLocal.Node, stageContract(c.PendingLocal.Node),
		), nil
	}
	if c.Delivery.PendingGate != "" {
		return fmt.Sprintf(`你在本机驾驶 infera 交付流水线当前挂起的人工门禁。

## 交付
- delivery_id：%s（调用 MCP 工具时的参数都用它）
- 需求：%s——%s
- 挂起门禁：%s
- workdir：%s（需要看代码时直接在这个检出里 git diff / git log）

## 驾驶方式
1. 调用 get_gate 查看 门禁详情（code_review 门会附真 diff 与审查意见）；
2. 需要深入核验时在 workdir 里直接检查代码；
3. 用 approve_gate 批准（流水线自动推进）或 reject_gate 打回（reason 注入重跑反馈）。`,
			id, c.Delivery.Title, c.Delivery.Description,
			c.Delivery.PendingGate, c.Repo.Workdir,
		), nil
	}
	return "", fmt.Errorf("交付未停在本机绑定节点（active 于 %s、无门禁），无需本机处理",
		c.Delivery.CurrentStage)
}

// CommandInputs BuildCommand 的输入（路径由调用方决定落盘位置）。
type CommandInputs struct {
	Workdir       string
	MCPConfigPath string // claude 用（--mcp-config）
	PromptPath    string
	Endpoint      string // codex 用（-c 内联，无 --mcp-config 入参）
	Token         string
}

// BuildCommand 构造在新终端里执行的 shell 命令：定位 workdir → 拉起带 MCP 配置
// 与初始提示的交互式 CLI。提示经 "$(cat <file>)" 注入（提示含规格全文，走参数
// 会超长且难审计）。路径做单引号转义。
func BuildCommand(cli string, in CommandInputs) string {
	q := shellQuote
	cd := "cd " + q(in.Workdir) + " && "
	prompt := `"$(cat ` + q(in.PromptPath) + `)"`
	switch cli {
	case "codex":
		return cd + "codex" +
			" -c mcp_servers.infera.url=" + q(in.Endpoint) +
			" -c mcp_servers.infera.http_headers.Authorization=" + q("Bearer "+in.Token) +
			" " + prompt
	default: // claude
		return cd + "claude" +
			" --mcp-config " + q(in.MCPConfigPath) +
			" --allowed-tools 'mcp__infera__*'" +
			" " + prompt
	}
}

// shellQuote 单引号包裹（内部 ' 转义为 '\”）——路径含空格/引号也安全。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LaunchPlan 一次「在本地处理此阶段」的完整拉起计划（内容已就绪，落盘与执行由 daemon）。
type LaunchPlan struct {
	Node      string // 停车节点（pending_local.node；门禁形态为门禁名）
	Workdir   string
	MCPConfig string // 配置文件内容
	Prompt    string // 提示文件内容
	Command   string // 终端命令
}

// Plan 组装拉起计划。stageDir 为本次会话的暂存目录（配置与提示落盘处）。
func Plan(c *Context, cfg Config, stageDir string) (*LaunchPlan, error) {
	mcpCfg, err := MCPConfigFor(cfg.CLI, cfg.Server, cfg.Token)
	if err != nil {
		return nil, err
	}
	prompt, err := BuildPrompt(c)
	if err != nil {
		return nil, err
	}
	node := c.Delivery.PendingGate
	if c.PendingLocal != nil {
		node = c.PendingLocal.Node
	}
	cfgName := "mcp.json"
	if cfg.CLI == "codex" {
		cfgName = "mcp.toml"
	}
	cmd := BuildCommand(cfg.CLI, CommandInputs{
		Workdir:       c.Repo.Workdir,
		MCPConfigPath: stageDir + "/" + cfgName,
		PromptPath:    stageDir + "/prompt.txt",
		Endpoint:      cfg.MCPEndpoint(),
		Token:         cfg.Token,
	})
	return &LaunchPlan{
		Node:      node,
		Workdir:   c.Repo.Workdir,
		MCPConfig: mcpCfg,
		Prompt:    prompt,
		Command:   cmd,
	}, nil
}
