# OpenSpec（infera）

infera 的**活文档业务模型**：把产品的每个能力域沉淀为一份持续演进的 spec，
任何功能变更都以增量 delta 提案（change）的方式在原模型上修改或新增，
评审并回后归档留痕。`openspec/specs/` 是全项目业务行为的**唯一事实源**。

机制参考开源项目 [Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec)
（目录形态与 delta 语义对齐），但本仓是**纯 Markdown 约定**：
不引入 openspec CLI、不装任何依赖，照着 [conventions.md](conventions.md) 写文件即可。

## 目录说明

```
openspec/
├── README.md          本文件：是什么、怎么用、域↔模块↔文档映射
├── conventions.md     spec 与 delta 的文件格式约定（写任何文件前先读它）
├── EVOLUTION.md       演进规则：delta → 评审 → 并回流程、谁维护、何时更新
├── specs/             活文档业务模型（唯一事实源），每域一个目录
│   └── <domain>/spec.md
└── changes/           功能变更提案（对 specs 的增量 delta）
    ├── templates/     change.md 提案模板 + 假想示例（ADDED/MODIFIED 写法演示）
    ├── archive/       已并回的提案按 <YYYY-MM-DD>-<change-id>/ 归档
    └── <change-id>/   进行中的提案（change.md）
```

## 日常怎么用

一个小闭环，三步：

1. **提 change**：开新提案目录 `openspec/changes/<change-id>/change.md`
   （从 [templates/change.md](changes/templates/change.md) 复制起手），
   用 `## ADDED / MODIFIED / REMOVED Requirements` 表达对现有 spec 的增量。
   假想完整示例见 [templates/example.md](changes/templates/example.md)。
2. **评审**：change 连同功能一起交付评审；spec 层面的改动以 change.md 为准讨论。
3. **并回**：提案合入后，按 [EVOLUTION.md](EVOLUTION.md) 的并回流程把 delta
   应用回 `specs/` 对应域，提案目录移入 `changes/archive/`。

规则要点（详见 [EVOLUTION.md](EVOLUTION.md)）：

- `specs/` 只能通过 change 并回来演进——**禁止另起炉灶重写既有 spec**；
- 功能任务交付时必须带上对应能力域的 delta 提案；
- 每个能力域的格式、目录名以 [conventions.md](conventions.md) 为准，不得另立格式。

## 能力域（8 个，已冻结）

| 域目录 | 域 | 一句话 |
|---|---|---|
| `specs/task-management/` | 需求接入与任务卡 | 外部需求接入、需求大节点状态机、需求卡与交付卡 |
| `specs/gates-approvals/` | 审批卡与门禁 | 人工门禁（spec 审批 / 代码审查 / 终审合并）、审批卡与待决策 |
| `specs/projects-board/` | 项目与看板 | 项目列表/详情、看板与发现视图、需求在项目下的组织 |
| `specs/statistics/` | 统计 | 项目维度需求统计、agent 执行时序聚合等只读分析面 |
| `specs/agent-orchestration/` | Agent 编排与执行 | 阶段图引擎、agent 可绑定节点、workdir 与产物固化 |
| `specs/web-routing/` | 路由与页面结构 | 前端路由树、布局、错误页、鉴权守卫的页面结构 |
| `specs/auth/` | 认证 | 登录、会话与访问控制 |
| `specs/mcp-integration/` | MCP 接口与同步集成 | MCP 驾驶面、任务源同步、标签镜像 |

## 域 ↔ 模块 ↔ 既有文档映射表（初始版）

> 归属规则：一个包/feature 只归一个主域；门禁、统计等横切能力按其**业务语义**
> 归域，REST/存储等**技术横切**基础设施单独列出。映射是导航用的初始版，
> 之后随 delta 演进维护；不改变任何代码归属。

| 能力域 | server/internal 包 | apps/web/src/features | 既有文档（docs/） |
|---|---|---|---|
| task-management | `reqservice`、`flow` | `requirements`、`deliveries` | `requirements-flow.md`；`superpowers/specs/2026-08-17-sdd-stages-design.md`、`2026-08-17-split-deliveries-design.md` |
| gates-approvals | `gatepoll`、`github`、`deliverylock` | `decisions` | `requirements-flow.md`（审批/终审节点）；`superpowers/specs/2026-08-19-r10-dual-review.md`、`superpowers/plans/2026-08-12-infera-p5-gate-ui.md` |
| projects-board | —（REST 面在 `api/projects.go`） | `projects`、`discovery` | `linear-behavior-spec.md`、`linear-features-2026.md`、`linear-产品介绍.md` |
| statistics | —（`api/projects.go` stats、`api/agent-activity.go`、`store` 聚合查询） | `agent-activity` | `linear-features-2026.md`（分析/Insights 章）；`superpowers/specs/2026-08-12-infera-product-design.md` |
| agent-orchestration | `engine`、`orchestration`、`agent`、`testrunner`、`workspace`、`persist`、`git` | `pipeline` | 根 `README.md`（架构/阶段图）；`superpowers/specs/2026-08-17-agent-orchestration-config.md`、`superpowers/plans/2026-08-12-infera-p1-foundation.md`～`p4-github.md` |
| web-routing | —（纯前端域） | `errors`（另含 `apps/web/src/routes/`、`components/layout/`） | `linear-behavior-spec.md`（page-by-page 结构）；根 `README.md` |
| auth | —（登录/会话在 `api` 中间件与 handler） | `auth`（另含 `routes/(auth)/`） | `superpowers/specs/2026-08-13-infera-projects-auth-design.md`、`superpowers/plans/2026-08-13-infera-projects-auth.md` |
| mcp-integration | `mcp`、`syncsvc`、`tasksource` | `task-sync` | `mcp.md`、`task-sync.md`、`labels-import.md`；`helper/README.md`（本机闭环，跨仓参考） |
| 横切基础设施（服务全部 8 域） | `api`、`store`、`db`、`config` | — | 根 `README.md`；`superpowers/specs/2026-08-15-backend-greenfield-design.md` |

包覆盖核对：`server/internal` 全部 19 个包（2+3+7+3 + 横切 4）；
feature 覆盖核对：`apps/web/src/features` 全部 10 个 feature。
