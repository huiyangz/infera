# infera MCP 服务

R3：把流水线的驾驶面暴露为 MCP（Model Context Protocol）服务，任何 MCP 客户端
（claude / codex / 其他 agent）都能驾驶 infera 流水线——查上下文、在 workdir 完成本机
绑定阶段的工作后交回产出、查看与裁定人工门禁。这是「本机交互通道」的服务端
（local 绑定节点的交回通道；本机 helper 是后续任务）。

## 启用与鉴权

```bash
# .env
INFERA_MCP_TOKEN=<随机长字符串>   # 不设置 = /mcp 端点禁用（503）
```

端点：`POST /mcp`（与 HTTP API 同进程同端口）。

**为什么是专用 token 而不是复用登录体系**（实现决策记录）：

- 现有认证是「密码登录 + cookie session」，MCP 客户端做不了浏览器登录流，
  但原生支持静态 `Authorization: Bearer` 头——token 是唯一自然的接入方式；
- 不直接拿 `INFERA_PASSWORD` 当 token：把「交互登录凭证」与「程序化控流水线凭证」
  解耦，两者可独立轮换；MCP token 会长期躺在客户端配置里，泄露面与密码不同；
- 未设置 token 时整个端点禁用（503）——不使用 MCP 的部署不暴露这个攻击面；
- 比较用常数时间（`crypto/subtle`），与 `internal/api/auth.go` 同款纪律。

其它防护：带 `Origin` 头的请求只接受 localhost 来源（防 DNS rebinding；CLI 客户端
通常不带 Origin）。token 等同流水线完全控制权，只配在本机或私有配置里。

## 协议说明

无状态 Streamable HTTP：单 `POST /mcp` + JSON-RPC 2.0、`application/json` 响应、
不分配会话（无需回传 `Mcp-Session-Id`）、不支持 GET SSE 流（405）。
支持版本 `2024-11-05` / `2025-03-26` / `2025-06-18`（握手回显请求版本，
不认识则回最新支持版）。

未引入官方 Go SDK 的理由：本面只需要 `initialize` / `ping` / `tools` 三类 method，
仓库依赖刻意保持极小，协议面小到手写完全可控，握手/版本协商/错误语义可被
`internal/mcp/mcp_test.go` 直接覆盖。

## 工具

| 工具 | 作用 |
|---|---|
| `get_context` | 交付全量驾驶上下文：需求、当前阶段/门禁、已有产物（每类最新一份全文）、仓库信息（repo / 默认分支 / workdir 路径与约定）、`pending_local`（本机停车节点的角色 prompt，与引擎发给 agent 的同源） |
| `get_gate` | 门禁详情：待审产物全文、AI 建议（复杂度/拆分/任务清单）、PR 地址；code_review 门附真 `diff`（裁决材料） |
| `approve_gate` | 批准门禁（引擎 `Approve` 单入口，选项按当前门校验：`complexity` / `split` / `tasks`），批准后自动推进 |
| `reject_gate` | 打回门禁（引擎 `Reject` 单入口），reason 注入回退阶段重跑 prompt |
| `submit_stage_output` | 本机绑定节点交回：`output` 按节点产物契约落盘（spec/design=文档全文；tasks 含 ` ```infera-tasks ` block；code_gen=改动摘要，代码改动直接做在 workdir），然后自动推进；code_review 门的本机审查交回预审意见 |

约定：门禁裁定与阶段交回全部走引擎单入口（无旁路状态修改）；产物 kind 契约与
引擎一致（`spec` / `design` / `tasks` / `tests` / `summary` / `agent_output` / `diff` / `pr`）。

## 客户端配置

### claude（Claude Code CLI）

一次性使用（不落配置）：

```bash
cat > /tmp/infera-mcp.json <<'EOF'
{"mcpServers":{"infera":{"type":"http","url":"http://localhost:8080/mcp",
  "headers":{"Authorization":"Bearer <INFERA_MCP_TOKEN>"}}}}
EOF
echo "<指令>" | claude -p --mcp-config /tmp/infera-mcp.json --allowed-tools 'mcp__infera__*'
```

持久注册：

```bash
claude mcp add --transport http infera http://localhost:8080/mcp \
  --header "Authorization: Bearer <INFERA_MCP_TOKEN>"
```

### codex

`~/.codex/config.toml`（字段名以所用 codex 版本文档为准）：

```toml
[mcp_servers.infera]
url = "http://localhost:8080/mcp"

[mcp_servers.infera.http_headers]
Authorization = "Bearer <INFERA_MCP_TOKEN>"
```

## 冒烟实录（2026-08-19，claude CLI 2.1.220 → 本地服务）

环境：本地 bare git 仓作项目仓库、会话专属 postgres 库、`SEED_LOCAL_SPEC=true`
（spec 节点绑 `local-console`）、`AGENT_CMD=echo`（后续阶段不跑真 agent）。
交互全部经 `claude -p --mcp-config` 完成：

1. 创建交付 → 引擎停车在 spec（`local_stage_pending`，active、无门禁）。
2. `get_context`：claude 正确报出「当前阶段 spec；pending_local node=spec；
   角色 prompt 要求末尾附 `infera-complexity` fenced block」。
3. `submit_stage_output`（含 complexity block 的规格）→ 引擎落 `kind=spec`
   artifact、交付推进并挂起 `spec_approval` 门禁。
4. `get_gate`（spec_approval）→ `complexity_suggestion=small`；`approve_gate`
   （不带选项=取 AI 建议）→ 推进 test_gen。
5. 引擎自动跑完 test_gen → code_gen → unit_test → code_review 门禁
   （`persist_done`：commit/push 交付分支 `infera/<id前8位>`）。
6. 打回回环验证：workdir 放一个未提交改动 → `reject_gate`（reason=补一行告别语）
   → 引擎回退 code_gen 重跑 → 重新固化 → `get_gate` 返回 `diff`
   （`+Bye infera`）——审查门的裁决材料完整可见。
7. `approve_gate`（code_review）→ 交付 `completed`，bare 仓可见交付分支。

全程五个工具 round-trip 均经真实 MCP 客户端（握手 → 工具列举 → 调用）完成。

## 实现位置

- `server/internal/mcp/`：协议层（`server.go`：JSON-RPC / 鉴权 / Origin / 版本协商）
  与工具（`tools.go`）；测试 `mcp_test.go`（握手 / tools 列举 / 五工具 round-trip）。
- `server/internal/engine/local.go`：`SubmitLocal`（本机交回的引擎落点）与
  `LocalPrompt`（只读角色 prompt）。
- `server/cmd/infera/main.go`：root 路由挂载 `/mcp`，推进回调注入 `srv.RunDelivery`
  （与 HTTP 面同一条 per-delivery 锁驱动路径）。
