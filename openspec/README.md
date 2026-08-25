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
├── sdd-integration.md SDD 流程接入建议：把 spec 维护嵌进功能任务交付（任务模板改法）
├── specs/             活文档业务模型（唯一事实源），每域一个目录
│   └── <domain>/spec.md
└── changes/           功能变更提案（对 specs 的增量 delta）
    ├── templates/     change.md 提案模板 + 假想示例（ADDED/MODIFIED 写法演示）
    ├── example-sdd-spec-maintenance/  worked example：在真实 spec 上提 delta 并并回的
    │                                  完整示范（教学件，不参与并回、永不归档）
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
| `specs/statistics/` | 统计 | 项目需求统计与列表统计行、项目/跨项目执行时序、工作区统计页 |
| `specs/agent-orchestration/` | Agent 编排与执行 | 阶段图引擎、agent 可绑定节点、workdir 与产物固化 |
| `specs/web-routing/` | 路由与页面结构 | 前端路由树、布局、错误页、鉴权守卫的页面结构 |
| `specs/auth/` | 认证 | 登录、会话与访问控制 |
| `specs/mcp-integration/` | MCP 接口与同步集成 | MCP 驾驶面、任务源同步、标签镜像 |

## 域 ↔ 模块 ↔ 既有文档映射表（终版）

> 归属规则：一个包/feature 只归一个主域；门禁、统计等横切能力按其**业务语义**
> 归域，REST/存储等**技术横切**基础设施单独列出。映射是导航用的，
> 之后随 delta 演进维护；不改变任何代码归属。

| 能力域 | server/internal 包 | apps/web/src/features | 既有文档（docs/） |
|---|---|---|---|
| task-management | `reqservice`、`flow` | `requirements`、`deliveries` | `requirements-flow.md`；`superpowers/specs/2026-08-17-sdd-stages-design.md`、`2026-08-17-split-deliveries-design.md` |
| gates-approvals | `gatepoll`、`github`、`deliverylock` | `decisions` | `requirements-flow.md`（审批/终审节点）；`superpowers/specs/2026-08-19-r10-dual-review.md`、`superpowers/plans/2026-08-12-infera-p5-gate-ui.md`、`superpowers/plans/2026-08-19-r10-dual-review.md` |
| projects-board | —（REST 面在 `api/projects.go`、`api/taskgroups.go`、`api/discovery.go`、`api/requirementcreate.go`） | `projects`、`discovery` | `linear-behavior-spec.md`、`linear-features-2026.md`、`linear-产品介绍.md` |
| statistics | —（`api/projects.go` stats/stage-runs、`api/agent-activity.go`、`api/workspacestats.go`、`store` 聚合查询） | `agent-activity`、`stats` | `linear-features-2026.md`（分析/Insights 章）；`superpowers/specs/2026-08-12-infera-product-design.md` |
| agent-orchestration | `engine`、`orchestration`、`agent`、`testrunner`、`workspace`、`persist`、`git` | `pipeline` | 根 `README.md`（架构/阶段图）；`superpowers/specs/2026-08-17-agent-orchestration-config.md`、`superpowers/plans/2026-08-12-infera-p1-foundation.md`～`p4-github.md`、`2026-08-12-infera-p6-realtime.md`（实时事件流）；`smoke/`（R11 真实 agent 冒烟） |
| web-routing | —（纯前端域） | `errors`（另含 `apps/web/src/routes/`、`components/layout/`） | `linear-behavior-spec.md`（page-by-page 结构）；根 `README.md` |
| auth | —（登录/会话在 `api` 中间件与 handler） | `auth`（另含 `routes/(auth)/`） | `superpowers/specs/2026-08-13-infera-projects-auth-design.md`、`superpowers/plans/2026-08-13-infera-projects-auth.md` |
| mcp-integration | `mcp`、`syncsvc`、`tasksource` | `task-sync` | `mcp.md`、`task-sync.md`、`labels-import.md`；`helper/README.md`（本机闭环，跨仓参考） |
| 横切基础设施（服务全部 8 域） | `api`、`store`、`db`、`config` | — | 根 `README.md`；`superpowers/specs/2026-08-15-backend-greenfield-design.md`、`superpowers/plans/2026-08-15-backend-greenfield.md` |

包覆盖核对：`server/internal` 全部 19 个包（task-management 2 + gates-approvals 3 +
agent-orchestration 7 + mcp-integration 3 + 横切 4）；
feature 覆盖核对：`apps/web/src/features` 全部 11 个 feature（task-management 2 +
gates-approvals 1 + projects-board 2 + statistics 2 + agent-orchestration 1 +
web-routing 1 + auth 1 + mcp-integration 1）。`helper/`（infera-link 本机守护）整仓
归 mcp-integration 域。

### 跨域引用备注（包主归属唯一，行为可跨域书写）

少数包的主归属在上表唯一，但与其行为相关的 Requirement 写在另一个域——按域找
行为时从下表跳转：

| 包 / feature | 主归属（上表） | 行为同时书写在 |
|---|---|---|
| `git` | agent-orchestration | mcp-integration「仓库检出与交付分支语义」（检出/凭据/分支命名的外部通道契约） |
| `github` | gates-approvals | mcp-integration「GitHub PR 代理面」（PR 元数据/评论/diff/合并的通道契约与失败归因） |
| `deliverylock` | gates-approvals | agent-orchestration「互斥驱动同一交付」（引擎侧驱动串行与重启恢复） |
| `features/deliveries` | task-management | agent-orchestration（阶段推进语义）、gates-approvals（门禁裁定面） |
| `features/agent-activity` | statistics | projects-board（页签挂载位置）、agent-orchestration（stage_run 产生） |

### 流程文档（非能力域）

- [sdd-integration.md](sdd-integration.md)（INFERA-283 / T08）：SDD 流程接入建议——
  把「维护对应能力域 spec」纳入功能任务交付要求的任务模板改法；
- [changes/example-sdd-spec-maintenance/change.md](changes/example-sdd-spec-maintenance/change.md)：
  可照抄的 worked example（教学示范件，不参与并回、永不归档）。

两者均非能力域 spec，不进入 8 域清单与上表。
