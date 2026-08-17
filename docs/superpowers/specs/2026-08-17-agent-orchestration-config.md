# Agent 编排配置（项目级 + 默认）设计

日期：2026-08-17
状态：用户确认优先级最高（"每一个项目都应该可以配置，当然要有默认配置，先搞定这个才好统一对齐"）
前置：11 节点阶段图（2026-08-17-sdd-stages-design.md）、双世界架构（本机交互/服务器执行）

## 原则

**Agent 是主体，流程是编排。** Agent 先注册，节点再绑定；默认编排全局一套，项目可覆盖。

## 数据模型（migration 0004）

```sql
-- Agent 注册表（全局）
CREATE TABLE agents (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,            -- 显示名，如 infCode / testAgent / 本机控制台
    runner     TEXT NOT NULL,                   -- cli | http | docker | local
    config     JSONB NOT NULL DEFAULT '{}',     -- runner 参数：{command:[...]} | {url} | {image,command} | {}
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 节点→Agent 绑定。project_id 为空 = 全局默认；非空 = 项目覆盖
CREATE TABLE pipeline_bindings (
    id         UUID PRIMARY KEY,
    project_id UUID NULL REFERENCES projects(id) ON DELETE CASCADE,
    node       TEXT NOT NULL,                   -- spec|design|tasks|test_gen|code_gen|code_review
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, node)                   -- NULL 参与唯一约束（PG15+ NULLS NOT DISTINCT；否则部分唯一索引）
);
-- 用部分唯一索引保证 PG 兼容：
CREATE UNIQUE INDEX idx_bindings_default ON pipeline_bindings(node) WHERE project_id IS NULL;
CREATE UNIQUE INDEX idx_bindings_project ON pipeline_bindings(project_id, node) WHERE project_id IS NOT NULL;
```

## 绑定与解析规则

- 可绑定节点（AGENT 节点，6 个）：`spec` `design` `tasks` `test_gen` `code_gen` `code_review`
- 有效绑定 = 项目覆盖 ?? 全局默认
- **校验（启动 + 交付启动时）**：6 节点必须全部有有效绑定且指向存在的 agent → 否则报错并写明缺哪个节点；项目创建/改绑时校验 agent 存在
- runner 语义：
  - `cli`：服务器子进程，config.command（占位符 $PROMPT/$WORKDIR/$ROLE）
  - `http`：POST config.url {role,prompt,workdir,context} → {output}
  - `docker`：RunInContainer(config.image, config.command+prompt)
  - `local`：本机交互（批 B 的 MCP+按钮通道）；本批实现为：交付停在该阶段 + 事件 `local_stage_pending`（占位，批 B 接管）
- 引擎：ExecuteStage 前按项目解析绑定 → runner 工厂构造执行器（cli/http/docker 新增 HTTPRunner；local 同上占位）

## 默认编排（启动种子）

boot 时若无默认绑定，按环境种子（幂等 upsert）：
- 种子 agent：`default-cli`（runner=cli, command 取 AGENT_CMD 环境）+ `local-console`（runner=local）
- 默认绑定：spec/design → local-console；tasks/test_gen/code_gen/code_review → default-cli
- 既有 E2E/流程不破坏：E2E 里如需走通 spec，可用 API 把项目绑定改为 default-cli（或环境开关 SEED_LOCAL_SPEC=false 全量 default-cli）

## API

- `GET /api/agents` / `POST /api/agents` {name,runner,config} / `PATCH /api/agents/:id` / `DELETE /api/agents/:id`（有绑定引用则 400）
- `GET /api/pipeline` → 默认绑定 + 可绑定节点清单 + agents 摘要
- `PUT /api/pipeline` {bindings:{node:agent_id}} —— 全量替换默认（校验 6 节点齐）
- `GET /api/projects/:id/pipeline` → {default:…, overrides:…, effective:…}
- `PUT /api/projects/:id/pipeline` {bindings:{node:agent_id}} —— 全量替换覆盖（可传 {} 清空）
- 认证同现有门禁（管理员即单租户用户）

## 前端（本批做最小可用）

- 项目详情页新增「编排」入口（设置图标）→ 弹窗/页：
  11 节点表：节点名 | 类型（门/命令/AGENT）| 执行者下拉（AGENT 行：全部 agents 可选，标注"默认"或"项目覆盖"）| 恢复默认按钮
- 全局默认：项目列表页「默认编排」对话框（同表格，管理员视角）
- agents 管理本批从简：仅展示 + JSON 配置提示（完整 CRUD UI 后续）

## E2E（新增 TestAgentBindings）

- 默认编排下创建项目 A → 交付跑通（cli 假 agent）
- 建第二个 cli agent（echo 别的角色标记）→ 项目 B 绑定 test_gen → 该项目交付的 test_gen 产物来自 B 的 agent；项目 A 不受影响
- 绑定缺失（删默认 test_gen 绑定）→ 新交付立即 blocked + 事件写明缺 test_gen

## 非目标

- 复杂编排（条件分支/并行节点自定义）、agents 完整管理 UI、绑定版本历史、local 通道实装（批 B）
