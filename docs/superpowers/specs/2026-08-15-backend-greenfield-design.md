# infera 后端重写（greenfield）设计

日期：2026-08-15
状态：已与用户确认（"推倒重写，不是打补丁"）

## 背景与动机

P1-P7 的后端是渐进长出来的，核心结构问题：

1. **仓库不是流水线的前置资源**：clone 发生在 code_gen（第 5 阶段），spec/test_gen
   两个 agent 完全看不到代码，写出的 spec 是空想。
2. **Workdir 无生命周期**：不记录基准 commit、永不清理、跨阶段语义含糊。
3. **阶段模型塞死在 service 里**：加阶段（主导 Agent 分诊、需求增强）要改引擎。
4. **API 长歪**：前端需要的聚合（项目活跃统计、置顶、阶段产出）无处安放。

## 核心原则（与用户讨论定稿）

**仓库是流水线的一体化前置资源**：delivery 启动即 clone（intake 前），一个
workdir 从头用到尾，所有 agent 阶段共享；clone 时记录 base_commit，整条流水线
对着同一快照（快照语义 A，零成本默认）。

**Agent 可替换**：agent runner 契约 = 子进程（容器内命令）+ workdir 进 + prompt
进 + artifacts/exit 出。今天 Claude Code，明天 pi（badlogic 的开源 agent），
换镜像与命令，不改后端。

## 架构

```
server/
  cmd/infera/           单二进制：API + 引擎同进程
  internal/
    api/                HTTP：路由/认证(cookie 密码门)/DTO，薄层
    engine/             流水线引擎：阶段图、门禁暂停、回环、终态
    workspace/          workdir 生命周期：Acquire→clone(记快照)→共享→Release
    agent/              agent 运行时：docker 容器 + 可配置命令(claude|pi|…)
    git/                ls-remote / clone / commit / push / PR（纯库）
    store/              postgres（sqlc）
```

## 数据模型（迁移从 v1 重新开始）

- `projects`：id, name, repo_url, default_branch, pinned(bool), created_at, updated_at
- `deliveries`：id, project_id, title, description, status(active|completed|blocked),
  current_stage, pending_gate, fail_count, base_commit, created_at, updated_at
- `stage_runs`：id, delivery_id, stage, attempt, status, started_at, finished_at
- `artifacts`：id, delivery_id, stage, kind(spec|tests|diff|pr|agent_output),
  content(text), created_at
- `events`：id, delivery_id, stage, event_type, payload(jsonb), created_at（WS 推送源）

## 流水线引擎

- 阶段图静态定义，节点类型：
  - `agent 节点`（需要 workdir：spec / test_gen / code_gen / code_review）
  - `gate 节点`（spec_approval / code_review：暂停等人）
  - `command 节点`（unit_test：容器里跑 go test，失败回环 code_gen，3 次阻塞）
- 引擎只调度图，不认识具体业务；新阶段 = 注册新节点。
- intake：workspace.Acquire（ls-remote 校验 → clone → 记 base_commit）。
- 终态（completed/blocked）：workspace.Release（延迟清理，保留期可配置）。

## API（前端零改动起步）

保持现有契约：`/api/login` `/api/logout` `/api/me` `/api/projects`
`/api/projects/:id` `/api/projects/:id/deliveries` `/api/deliveries/:id`
`/api/deliveries/:id/gate|approve|reject` `/ws?delivery=`。

新增：
- `GET /api/projects?include=stats` → 每项目 active/pending 计数 + 最近活动
- `PATCH /api/projects/:id` → `{pinned}` 置顶同步
- `GET /api/deliveries/:id` 详情带 `artifacts[]` 与 `base_commit`

## 保留（作为库搬入，不保留旧架构）

docker 容器执行器、git/github 客户端、WS hub 的**经验与代码片段**。

## 非目标（继续"以后再说"）

门后刷新 main、主导 Agent 分诊站、workdir 跨 delivery 复用池、多租户。
