# infera

Agent 主导的代码交付流水线：提需求 → agent 生成规格 → 人工审批 → 测试/实现/自测回环 → 代码审查 → 完成。

## 架构

**单 Go 二进制（`server/`）+ Vite 前端（`apps/web/`）+ postgres。** 后端一个进程装下全部：HTTP API、引擎、agent 执行、迁移（内嵌 SQL，启动时自动跑）。

- **workdir 是一等资源**：delivery 创建即 intake（绿地建目录 / 已有仓库 clone），全程共享给各阶段，终态后延迟清理（默认 30min，留排查窗口）。
- **agent 可替换**：`AGENT_CMD` 指定命令（默认 `claude`，可换 `pi` 等），`AGENT_BACKEND` 选本地进程（默认）或 docker 容器执行。
- **引擎阶段图**（`internal/engine/graph.go`）：

  ```
  intake → spec → [spec_approval] → test_gen → code_gen → unit_test → [code_review] → DONE
                                    （人工门禁）                ↑______回环（失败重试，≤3 次）______|
  ```

  agent 节点产出产物，`[...]` 人工门禁暂停等待批准，`unit_test` 失败回环到 `code_gen`，连续 3 次失败转 blocked。

## 本地开发

```bash
docker compose up -d        # postgres（:5433，库 infera_v2；首次启动自动建库）
cp .env.example .env        # 填 INFERA_PASSWORD 等
(cd apps/web && pnpm install)  # 前端依赖（首次需要；pnpm，npm 不可用）
./run-dev.sh                # 后端 :8080 + 前端 vite :5173（/api 代理到 8080）
```

迁移在启动时自动执行；数据库默认指向 `infera_v2`（新后端 v1 起全新 schema；旧库 `infera` 保留 legacy 数据仅供查阅）。

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `PORT` | `8080` | HTTP 端口（`8080` 与 `:8080` 写法均可） |
| `DATABASE_URL` | `postgres://infera:infera@localhost:5433/infera_v2?sslmode=disable` | postgres 连接串 |
| `INFERA_PASSWORD` | 必填 | 登录密码（单用户，缺则启动 fatal） |
| `INFERA_MCP_TOKEN` | 空（禁用） | MCP 服务 Bearer token（`/mcp` 端点，见 [docs/mcp.md](docs/mcp.md)） |
| `GITHUB_TOKEN` | 空 | 导入已有仓库 / 推 PR 用 |
| `AGENT_CMD` | `claude` | agent 命令；本地调试可用 `echo` |
| `AGENT_BACKEND` | 空（本地进程） | `docker` = 容器执行（配合 `AGENT_IMAGE`） |
| `AGENT_IMAGE` | `infera-agent` | agent 容器镜像（仅 docker 后端用） |
| `TEST_CMD` | `true` | unit_test 阶段命令（本地模式） |
| `REPO_WORK_ROOT` | `/tmp/infera-workdirs` | workdir 根目录 |

## 接入其他 agent（示例：pi）

```bash
# .env 中
AGENT_CMD=pi
```

本地进程模式下引擎以 `AGENT_CMD "$INFERA_PROMPT"` 执行（prompt 同时写 stdin），并通过环境变量传入 `INFERA_ROLE`（spec / test_gen / code_gen / code_review）与 `INFERA_WORKDIR`。任何吃参数/stdin、输出 Markdown 的 CLI agent 均可接入。

## MCP 服务（外部 agent 驾驶流水线）

设置 `INFERA_MCP_TOKEN` 后，`/mcp` 暴露 MCP 服务（无状态 Streamable HTTP，Bearer 鉴权），claude / codex 等任意 MCP 客户端可驾驶流水线：`get_context` 查交付上下文、`submit_stage_output` 交回本机绑定节点（local 绑定）的阶段产出、`get_gate` / `approve_gate` / `reject_gate` 查看与裁定人工门禁（走引擎 Approve/Reject 单入口）。配置示例与冒烟记录见 [docs/mcp.md](docs/mcp.md)。
