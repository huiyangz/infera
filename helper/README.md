# infera link（本机交互通道 helper，R4）

`infera-link` 是常驻**用户本机**的守护进程：把 infera 网页上的「在本地处理此阶段」
按钮接到本机的 claude / codex CLI。local 绑定节点不再是停车占位——点击按钮即在本机
终端拉起一个带 infera MCP 配置与初始提示的 CLI 会话，交互阶段（规格 / 设计的链式
问答等）在本机完成，产出由 CLI 经 MCP `submit_stage_output` 交回，流水线自动推进。

```
网页按钮 ──POST /handle──▶ infera-link 守护进程 ──MCP get_context──▶ infera 服务
                              │ 生成 MCP 客户端配置 + 初始提示（~/.infera/link/<id>/）
                              ▼
                     新终端窗口：cd <workdir> && claude --mcp-config … "$(cat prompt)"
                              │  本机完成阶段工作
                              ▼
                     claude 经 MCP submit_stage_output 交回 ──▶ 流水线推进
```

## 安装

需要 Go ≥ 1.26（标准库实现，无第三方依赖）：

```bash
cd helper
go build -o /usr/local/bin/infera-link ./cmd/infera-link   # 或任意 PATH 内位置
```

前置：infera 服务已启用 MCP（设置了 `INFERA_MCP_TOKEN`，见 [../docs/mcp.md](../docs/mcp.md)）；
本机装有 `claude`（默认 CLI）或 `codex`。

## 启动

```bash
infera-link                       # 全默认：server=http://localhost:8080，listen=127.0.0.1:8788
infera-link --token "$INFERA_MCP_TOKEN"            # token 与服务端 INFERA_MCP_TOKEN 同值
infera-link --server http://localhost:8080 --cli codex
infera-link --terminal none       # 调试/无头：不执行，把命令打到守护进程日志
```

| 配置 | flag | 环境变量 | 默认 |
|---|---|---|---|
| infera 服务基址 | `--server` | `INFERA_URL` | `http://localhost:8080` |
| MCP token | `--token` | `INFERA_MCP_TOKEN` | 空（daemon 可启动，`/handle` 时报可操作错误） |
| 监听地址 | `--listen` | `INFERA_LINK_ADDR` | `127.0.0.1:8788` |
| 本机 CLI | `--cli` | `INFERA_LINK_CLI` | `claude`（可选 `codex`） |
| 拉起方式 | `--terminal` | `INFERA_LINK_TERM` | `auto`（可选 `none`） |

优先级：flag > 环境变量 > 默认。token 与服务端 `.env` 里的 `INFERA_MCP_TOKEN` 同值即可，
最省事的启动方式是在仓库根 `source .env` 后直接 `infera-link`。

验证：`curl http://127.0.0.1:8788/healthz` → `{"ok":true,...}`（不回显 token）。

## 使用

1. infera 编排里把某节点绑定到 runner=local 的 agent（如种子自带的 `local-console`，
   Agent 管理页可改绑；spec / test_gen / code_gen 可绑流水线节点，code_review /
   spec_conformance / code_quality 可绑审查角色）；
2. 提交需求，流水线停在该节点——交付详情页出现「在本地处理此阶段」按钮
   （code_review 门禁的审查角色绑本机时，门禁页出现同样的按钮做本机预审）；
3. 点按钮 → 本机弹出终端窗口，claude 已带着 MCP 配置与初始提示定位在交付 workdir；
4. 在终端里完成该阶段（可以和 claude 多轮问答），claude 会调
   `submit_stage_output` 交回，网页自动刷新、流水线推进。

每次拉起的 MCP 配置与初始提示落盘在 `~/.infera/link/<delivery_id>/`
（`mcp.json` / `prompt.txt`，含 token，权限 0600，可复查审计）。

## 两种拉起形态

- **停车节点（pending_local）**：初始提示 = 与引擎发给绑定 agent 完全同源的角色 prompt
  （get_context 的 `pending_local.prompt`）+ 交回契约。spec 节点会要求末尾附
  ` ```infera-complexity ` block，code_gen 的代码改动直接做在 workdir、output 为改动摘要。
- **门禁预审（code_review 角色绑本机）**：初始提示引导 `get_gate` 看门禁详情（含真 diff
  与双道审查意见）、`approve_gate` / `reject_gate` 裁定。

## codex 说明

codex 没有 `--mcp-config` 入参，daemon 用 `-c mcp_servers.infera.url=…` /
`-c mcp_servers.infera.http_headers.Authorization=…` 内联覆盖拉起（token 会出现在命令行，
单用户本机可接受；介意者可把生成的 `~/.infera/link/<id>/mcp.toml` 片段并入
`~/.codex/config.toml` 做持久注册后改用裸 `codex`）。**端到端只在 claude 上验证过**，
codex 形态按 [docs/mcp.md](../docs/mcp.md) 的配置契约生成，遇到字段差异以所用 codex
版本文档为准。

## 端到端实录（2026-08-19，本仓库 attempt 环境复验）

环境：会话专属 postgres 库、本地 bare git 仓作项目仓库、`SEED_LOCAL_SPEC=true`
（spec 绑 `local-console`）、`AGENT_CMD=echo`、服务起在 :18080、helper 以
`--terminal none` 无头运行（完整步骤可复制为手动验收清单）：

1. 登录建项目建需求 → 交付停车在 spec（`local_stage_pending`，active 无门禁）；
2. 模拟网页按钮的原样 HTTP 调用
   `POST http://127.0.0.1:18788/handle {"delivery_id":"…"}`（与
   `LocalHandleButton` 的 fetch 完全一致）→ 200，`{"node":"spec","workdir":"…"}`；
3. 复核落盘：`~/.infera/link/<id>/mcp.json` 指向 `http://localhost:18080/mcp` 且带
   Bearer token；`prompt.txt` 为同源角色 prompt + 交回契约；
4. 取守护进程日志里的命令，以 `claude -p`（提示经 stdin）执行 → claude 经真实 MCP
   握手调 `submit_stage_output` 交回规格；
5. 事件流验证：`spec local_stage_submitted` → `spec_approval gate_pending` → 批准后
   一路推进 `test_gen → code_gen → unit_test → code_review`（`persist_done`、双道
   `review_findings`、门禁挂起）。claude 会话还继续用 `get_gate` / `reject_gate`
   驾驶了门禁并正确识别出 `AGENT_CMD=echo` 桩环境 diff 为空的问题。

**已知环境限制**：`--terminal auto` 的开窗原语（osascript 驱动 Terminal.app）在无人
值守沙箱里被 macOS AppleEvent 超时（-1712）挡下——首次交互式使用时系统会弹
「自动化」授权，允许即可；无法授权的环境用 `--terminal none` 取命令手动粘贴，
其余链路完全一致。

## 故障排查

| 症状 | 处置 |
|---|---|
| 按钮报「无法连接本机 infera-link 守护进程」 | helper 没起或端口不符：启动 `infera-link`；端口改过则同步设前端 `VITE_INFERA_LINK_URL`（或还原 `--listen 127.0.0.1:8788`） |
| `/handle` 报「MCP 服务返回 503」 | 服务端未设 `INFERA_MCP_TOKEN`，在 `.env` 配置后重启服务 |
| `/handle` 报 401 | helper 的 token 与服务端不一致（`--token` / `INFERA_MCP_TOKEN`） |
| 拉起终端失败：AppleEvent 已超时 (-1712) | macOS 自动化授权未通过：系统设置 → 隐私与安全性 → 自动化，允许调用方控制 Terminal；无头环境改 `--terminal none` |
| 拉起后 claude 连不上 infera 工具 | 服务地址不对（`--server`）或 MCP 被禁用；用 `curl -H "Authorization: Bearer <token>" -d '{}' <server>/mcp` 自查 |
| 想换默认端口 8788 | helper `--listen` 与前端 `VITE_INFERA_LINK_URL`（如 `.env.local`）同步改 |

## 实现位置

- `cmd/infera-link/main.go`：入口（flag/env → 守护进程）。
- `internal/link/`：`config.go`（配置解析）、`mcpclient.go`（MCP 客户端，仅 get_context）、
  `launch.go`（配置生成 / 初始提示 / 命令构建，纯函数）、`server.go`（healthz / handle /
  CORS）、`terminal_darwin.go`（osascript 开窗；其他平台见 `terminal_other.go`）。
- 单测：`go test ./...`（协议编解码 / 配置生成 / 守护进程全链路 httptest 覆盖）。
