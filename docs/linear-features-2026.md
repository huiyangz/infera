# Linear 功能全景（2026 当前版）

> **生成日期**：2026-08-08
> **方法论**：5 路并行搜索 + 对抗式验证（每条论断 3 票，需 2/3 反驳才剔除）+ 定向补抓官方文档。
> **来源**：均为 linear.app 一手文档（页面构建号 `linear-web@1.64778.0` → `1.72869.0`，确认是 2026 当前发布线）。共 23 条核心论断全部 3-0 通过验证。

---

## 架构总览：四层制品 + 团队为核

Linear 是一个**强观点、数据模型严谨**的工程与产品规划平台。它的全部功能围绕一个明确的层级组织：

```
Initiative（倡议 / 公司级目标）
   └─ Project（项目 / 特性级，可跨团队）
        └─ Issue（问题 / 任务 / bug）
             └─ Sub-issue（子任务）
```

- **Team（团队）是核心组织单元**——状态工作流、cycle、triage、标签、模板全部按团队配置。
- **View（视图）是 filter 驱动的**——不是静态列表，而是保存下来的过滤器组合，数据自动流入。
- **"Roadmap" 不是一个独立对象**：官方文档明确 *"Roadmaps are renamed to Initiatives"*。"路线图"现在是 **Initiative + Timeline 视图**的组合呈现。

---

## 一、核心追踪原语（Issue 及其关系）

### 1.1 Issue 属性

基础字段：ID、状态、负责人(assignee)、优先级、SLA、项目、截止日、里程碑、cycle、release、估算(estimate)、标签、链接、客户、客户营收、time in status、创建/更新时间、**PR 与 commit、Sentry issue**。

> 关键点：PR/commit、Sentry issue、客户营收能作为**可查询的显示属性**存在，说明这些外部集成是"一等公民"——不是简单超链接，而是写进 API schema 的字段。

### 1.2 Issue 关系（两套独立机制）

| 机制 | 类型 | 说明 |
|---|---|---|
| **层级** | Parent / Sub-issue | 分解"一个大 issue 放不下、一个项目又嫌小"的工作 |
| **对等关系** | Blocked / Blocking / Related / Duplicate | 共 4 种，对称的 |

**子问题属性继承规则**（确定性，非用户可选）：
- **必继承**：team、priority、project
- **条件继承**：cycle（仅当在 active 状态下创建）、assignee（当你本人是 parent 的 assignee，或所有现存 sub-issue 共享 parent 的 assignee）
- **永不继承**：labels

### 1.3 依赖与重复（两个被设计进数据模型的概念）

- **依赖（Blocked/Blocking）** 是有方向的，侧边栏可视化：blocked issue 显示橙色旗标（Blocked by），blocking issue 显示红色旗标（Blocks）。
- **Duplicate 是"状态结局"，不只是关系**：标记重复会强制把 issue 移入系统保留、**不可改名不可自定义**的 "Duplicate" 状态，让重复在报表中成为可见的独立结局（单向，不能反向标记 canonical issue）。

### 1.4 状态工作流（按团队配置）

- 默认有序集：`Backlog > Todo > In Progress > Done > Canceled`
- 团队可增 / 删 / 改名 / 重排**状态**，但**状态类别(category)顺序固定**、不可重排。
- **Triage 是额外的状态类别**，充当团队的收件箱。

---

## 二、Triage 收件箱（Linear 的差异化能力）

专为"外部来源进来、还没归类"的 issue 设计：

- **路由**：集成（Slack、Sentry、Intercom / Front / Zendesk）创建的、或非本团队成员创建的 issue，默认进入 Triage 状态；除非显式用状态过滤器，否则**所有视图默认排除 triage issue**。
- **4 个键盘动作**（快捷、流畅）：`1` 接受、`2` 标记重复、`3` 拒绝（→ Canceled 类型）、`H` 延后（snooze，到时间或新活动时回归）。
- **Triage Rules（规则引擎）**：基于可过滤属性触发，自动更新 team / status / assignee / label / project / priority；**从上到下顺序执行**；若规则把 issue 路由到另一团队的 triage，会**级联**应用目标团队的规则；冲突在界面里显式提示。

---

## 三、规划制品（向上扩展）

### 3.1 Projects（项目）

- 属性：name、lead（单一负责人，保持归属清晰）、members（可选，需 opt-in 收通知）、icon、状态、**start / target date（支持"时间框"粒度：年 / 半年 / 季度 / 月 / 精确日，匹配不确定度）**、progress、health、scope。
- 多团队共享（多团队时会按团队分 tab）。
- 可附加 **issue views**（如"我的 issue""bug""standup"）和 **project views**（如"进行中"）作为 tab。
- 删除的 project 进团队归档，**30 天可恢复**。
- 一个 issue 同一时刻只能属于一个 project。

### 3.2 Initiatives（倡议 / 原 Roadmap）

- **有意组织**一批围绕同一公司目标的 projects（区别于"按 filter 自动收集"的 project view）。
- 5 个状态：`Proposed / Planned / Active / Completed / Canceled`。
- 支持 sub-initiatives（2025-07 新增），是最上层规划单元。Initiative 视图限 **Enterprise 套餐**。

### 3.3 Cycles（迭代 / 类 sprint）

- 保持团队节奏的实践（类敏捷 sprint）。
- 有 cycle 详情面板：完成百分比、总 effort、进度图（effort 与 scope 随时间变化）。

### 3.4 Milestones / Dependencies / Project Graph

- **Milestones**：项目生命周期内的阶段标记。
- **Project dependencies**：项目间依赖映射（仅 end→start），在时间线上可视化阻塞，可自动顺延下游 backlog / planned 项目。
- **Project Graph（预测）**：基于每周项目 velocity 和剩余 issue 估算，可视化 scope / velocity 随时间变化，**预测完成日期与可能延期**。
- ⚠️ 注意："critical path""resource allocation"是营销措辞，Linear **不做**自动 CPM / 资源分配计算，本质是手动 end→start 依赖可视化。

---

## 四、视图、过滤与显示

### 4.1 自定义视图（3 类）

`Issue` / `Project` / `Initiative`（后者限 Enterprise）。创建方式：加 filter → `Option/Alt+V` 或保存视图图标保存。

### 4.2 分组 / 布局

- **可按** status / assignee / project / priority / cycle / label / parent issue / team / customer / release / SLA status 分组。
- **布局**：list 和 board，`Cmd/Ctrl+B` 切换；board 默认手动拖拽排序。
- **Timeline 视图**：任何含 project 的视图（团队项目页 / Initiative / 工作区项目页）都可在 "Display" 里选。

---

## 五、自动化体系

Linear 的自动化散落在多个配置点，但模型清晰：

| 自动化 | 位置 | 能力 |
|---|---|---|
| **Triage Rules** | Team 设置 | 见第二节 |
| **Parent / Sub auto-close** | Settings > Team > Workflow | parent auto-close（子全 done→父 done）/ sub-issue auto-close（父 done→剩余子 done），2024-09 上线 |
| **PR & commit 自动化** | Settings > Team > Issue statuses & automations | 见第六节 GitHub |
| **Auto-assign & move to start** | Preferences | 复制 git 分支名时自动把 issue 指给自己并移到"已开始"状态 |
| **Triage Intelligence / Linear Agent** | AI 设置 | 见第八节 |

---

## 六、GitHub 集成（最深的第三方集成，作为范本）

| 能力 | 细节 |
|---|---|
| **PR linking** | 分支名含 issue ID / PR 标题含 ID / magic word + ID |
| **Magic words** | **closing 类**（close / fix / resolve / implement…→ 合并时改 Done）vs **non-closing 类**（ref / related to / part of…→ 不自动关） |
| **状态自动化** | PR drafted / opened / review requested / ready for merge / merged → 各自定义目标状态；**按目标分支细分规则**（如合并到 `staging`→"In QA"，`main`→"Deployed"，支持正则 `^fea/.*`） |
| **Issue 双向同步** | 单向 / 双向；同步 title / description / status / assignee / labels / sub-issues / comments；支持多级与跨团队 / 跨仓库层级 |
| **PR review state** | 评审者评论 / 请求修改 / 批准直接在 Linear 附件上显示头像与动作 |
| **Preview links** | 自动识别 Vercel / Netlify / Cloudflare / AWS Amplify 与自定义（以 "preview" 结尾的链接）；多预览下拉；30 天不活跃自动清理 |
| **企业版** | GitHub Enterprise Cloud（需 IP allow list）/ Enterprise Server（独立 GHES App，支持 PR linking 但不支持 issue 同步与 commit linking） |

同类深度集成还有：**GitLab、Slack（Asks）、Figma、Sentry、Intercom / Front / Zendesk（工单）**、Zapier、Airbyte、Discord。

---

## 七、Insights（实时分析）

把 issue 数据当作可分析数据集。可在任何视图右侧栏 `Cmd/Ctrl+Shift+I` 打开，最佳实践是建**工作区级共享自定义视图**（跨所有团队、任意属性过滤）。

| Measure（度量，Y 轴） | 含义 | 图类型 |
|---|---|---|
| Issue count | 总数 | 柱状 |
| Effort | 总估算值 | 柱状 |
| **Cycle Time** | 开始到完成 | 散点 |
| **Lead Time** | 创建到完成 | 散点 |
| **Triage Time** | 停在 Triage 的时间 | 散点 |
| Issue Age | 自创建至今 | 散点 |

- **Slice**（X 轴）+ **Segment**（颜色再切分）。
- **Burn-up charts（累计流图）**：展示工作流随时间变化，默认月粒度，可调周 / 含归档。
- 散点图带 25 / 50 / 75 / 95 百分位标记，可缩放、点选跳到 issue。
- 可全屏、复制链接、**导出 CSV**、内建 Help Center 示例库。

---

## 八、AI 与 Agent 平台（2026 战略重点）

这是 Linear 当前投入最大、变化最快的方向，分四层：

### 8.1 Linear Agent（第一方 AI，2026-03 上线）

代号 **Charlie**，作为 assignee 菜单里的 agent 出现。

### 8.2 Code Intelligence（2026-05 上线）

AI 代码理解能力。⚠️ **Guest 角色被明确禁止使用**。

### 8.3 Agents in Linear（第三方 agent / "app users"）

- agent 行为**类似用户**：可被 @mention、被指派（delegate）、创建 / 回复评论、协作项目与文档。
- **Delegate 语义**：把 issue 指派给 agent 是"委派"，**人类仍是主负责人与 owner**——这是 Linear 独特的设计。
- **Agent Guidance（agent 指引）**：markdown 编辑器（带完整历史），workspace 级 + team 级（team 优先），告诉 agent 用哪个仓库、如何引用 issue、走什么评审流程。
- 可追踪 agent 活动：agent 用户页、按 Delegate 过滤的自定义视图、按 Delegate 切分的 Insights。
- **不计费席位**；agent 不能登录 app、不能访问 admin 功能。

### 8.4 Linear for Agents（开发者预览，2025-05）

开发者文档专门一节 **Agents**：含 **AIG（Agent Interaction Guidelines）**、Getting Started（明确 *"APIs are currently in active development and available as a Developer Preview"*）、Interaction Best Practices、**Signals** 概念。

### 8.5 Linear MCP（2026-02 上线）

面向产品管理的 MCP server，让外部 LLM / Claude 等 agent 可读写 Linear 数据。

### 8.6 Triage Intelligence

triage 场景下的 AI 辅助分类。

---

## 九、开发者平台

| 表面 | 说明 |
|---|---|
| **GraphQL API** | 查询与变更数据的核心接口 |
| **TypeScript SDK**（`@linear/sdk`，2026-01 v70+） | 把 GraphQL schema 暴露为强类型模型与操作 |
| **Webhooks** | 事件订阅 |
| **OAuth apps** | 应用认证（enterprise 下需 owner 审批） |

Schema 可在 Apollo Studio 查（如 `ViewPreferencesValues.fieldSentryIssues` 字段印证了 Sentry 集成是一等公民）。

---

## 十、协作与通知

### 10.1 协作

- **评论 / @mention / 订阅(subscribe)**。
- 自动订阅触发：创建、被指派、评论或描述中被 @mention、手动订阅。被 @mention 在某评论线程→订阅该线程但不一定订阅整个 issue。
- Documents（文档，挂在项目里）。

### 10.2 通知（4 通道）

**Desktop / Mobile / Email / Slack**。绿点 = 启用、灰点 = 禁用，每通道独立开关。

- Desktop / Mobile / Slack：实时；Email：按紧急度的摘要(digest)或立即投递，仅在未读 inbox 时才发。
- 通知**分组**（如"状态变化"含完成 / 取消 / 紧急优先级变更 / 阻塞关系变更，不可只挑状态变化一项）。
- inbox 最多保留 **2000 条**未读，超限自动归档。

---

## 十一、客户端

| 客户端 | 备注 |
|---|---|
| **桌面** | macOS (Intel + Apple Silicon)、Windows；原生通知、常驻 dock、tab、快捷键无浏览器冲突；自动更新（可 MDM 关闭） |
| **Web** | 支持 Chrome / Firefox / Safari 最近 3 版；离线模式几乎全功能；Universal links 可配置"在桌面 app 打开" |
| **移动** | iOS / Android 原生；Home / Inbox / 创建 / 搜索 / 设置 tab |
| **PWA** | 平板与移动浏览器 |
| 离线 | 实时同步 + 本地重试；标注为 failsafe 非完整功能（离线大量编辑可能覆盖他人改动） |
| 协议 | `linear://` 直接打开桌面 app；localhost 探测端口 44450 / 18450 / 33234 |

---

## 十二、权限与治理

### 12.1 角色（5 级）

| 角色 | 套餐 | 能力 |
|---|---|---|
| **Workspace Owner** | Enterprise | 全权，含 billing / security / audit logs / exports / OAuth 审批 |
| **Admin** | 全 | 日常 workspace 管理（Enterprise 下权限受限） |
| **Team Owner** | Business / Enterprise | 团队级委派管理；删团队 / 设私有 / 改 parent 仅限此角色 |
| **Member** | 全 | 跨有权限团队协作；不能进 workspace 管理 |
| **Guest** | Business / Enterprise | 仅限被加入的团队；看不到工作区视图 / initiatives；不能用 Code Intelligence |

### 12.2 团队级可配置权限

Team settings > Access and permissions：Issue Label / Template / Team Settings / Member 管理——均可选"全体成员"或"仅 team owner"。**权限不从 parent team 继承到 sub-team**。

### 12.3 企业治理

- **SCIM**（IdP 管理成员）、**SAML SSO**、**audit logs**、**workspace exports**、**Workspace restrictions**（owner 可配置哪些角色能做敏感操作）。
- Guest 与集成的数据隔离有专门指引（Slack / Discord / Front / Zendesk 邮箱鉴权的集成会自动限到受邀团队）。

---

## 置信度与遗留缺口

**高置信（3-0 验证通过 + 官方文档原文）**：核心数据模型、关系继承、triage、views、insights、projects / initiatives、GitHub 集成、roles、apps、notifications、agents-in-linear——这部分可直接作为产品决策依据。

**已澄清的歧义**：Roadmap = Initiatives + Timeline 视图（非独立对象）。

**需注意的时点性**：**Linear for Agents API 仍处 Developer Preview / active development**，其 API 表面会变；Code Intelligence 仅有发布时间(2026-05)与"guest 禁用"两点确认，未拿到能力细节。

---

## 来源（Sources）

**一手文档（primary）：**
- `linear.app/docs/issue-relations`
- `linear.app/docs/parent-and-sub-issues`
- `linear.app/docs/configuring-workflows`
- `linear.app/docs/triage`
- `linear.app/docs/custom-views`
- `linear.app/docs/display-options`
- `linear.app/docs/projects`
- `linear.app/docs/initiatives`
- `linear.app/docs/cycles`
- `linear.app/docs/insights`
- `linear.app/docs/notifications`
- `linear.app/docs/get-the-app`
- `linear.app/docs/members-roles`
- `linear.app/docs/github`
- `linear.app/docs/agents-in-linear`
- `linear.app/docs/api-and-webhooks`
- `linear.app/plan`、`linear.app/developers`、`linear.app/developers/sdk`、`linear.app/developers/agents`、`linear.app/developers/aig`

**Changelog（时间锚点）：**
- `2024-09-06` auto-close parent & sub-issues
- `2025-05-20` Linear for Agents
- `2026-02-05` Linear MCP for product management
- `2026-03-24` Introducing Linear Agent
- `2026-05-14` Code Intelligence

**佐证：**
- `github.com/linear/linear-typescript`（SDK 版本号印证）
- `studio.apollographql.com/public/Linear-API`（schema 字段印证）
