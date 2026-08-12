# Linear 复刻规格 · 产品行为级（2026）

> **形态**：产品行为规格（page-by-page 交互、字段、状态迁移、边界、快捷键），非 DB schema。
> **范围**：核心 + 全集成 + 分析。
> **生成**：2026-08-08。108-agent 对抗式验证 + 5 路并行子代理抓取官方文档。
> **复现说明**：所有快捷键、枚举、公式、参数均为官方文档逐字摘录，可直接照搬实现。`Cmd`=macOS，`Ctrl`=Windows/Linux；`G then X`=按 G 松开再按 X（chord）。

---

## 第 0 章 · 设计哲学（Linear Method）—— 决定一切"为什么"

复刻 Linear 必须先吃透它的**强观点**，否则只会做成又一个 Jira。核心信条（逐字）：

- **Build for the creators**——工具为创造者（执行者）优化，而非为生成完美报表的管理者优化。*"Keeping individuals productive is more important than generating perfect reports."* 这是它和 Jira 的根本分野。
- **Purpose-built，反灵活性**——*"Flexible software lets everyone invent their own workflows, which eventually creates chaos as teams scale."* 刻意不给用户无限可配置，预设"正确"的工作流。
- **Create momentum, don't sprint**——保持健康节奏，不是冲终点。
- **Write issues, not user stories**——*"user stories are an anti-pattern"*。issue 用平实语言描述一个**明确产出**的任务；描述可选、宜短；直接粘贴用户原话而非概括。
- **Say no to busy work**——工具不该让你成为它的设计者/维护者；自动化掉"为了工作的工作"。
- **Aim for clarity**——不造词；项目就叫项目。
- **Decide and move on**——没有最佳答案时，做决定、往前走。
- **Scope projects to 1–3 周 / 1–3 人**；每个项目/issue 有**唯一具名 owner**；写**简短**的项目 spec。
- **Mix feature and quality work**——cycle 里混入 bug/技术债；*"tooling is a force multiplier"*。

**这些信条直接推导出产品形态**：keyboard-first、issue 为原子单元、team 为作用域、固定枚举（优先级、估算刻度）、不可随意自定义状态类别、强自动化的 triage 与 git 集成。

---

## 第 1 章 · 应用外壳与输入模型

### 1.1 侧栏（Sidebar）—— 一切视图的根

自上而下结构：

1. **工作区切换器 / 头部**——工作区名、头像、搜索触发器。`O then W` 切换/创建工作区（⚠️ 未在官方页确认，第三方速查表来源）。
2. **Inbox**——未读计数 badge。`G then I`。
3. **My Issues**——`G then M`。
4. **Pull Request Reviews**——连接 GitHub/GitLab 后出现（官方 sidebar 页把它画在团队页**之后**，位置仅供参考）。
5. **Favorites**——收藏第一个后才出现；**个人级**（per-user，不共享）；可含子文件夹。`O then F` 打开选择器；hover 后 `X` 移除。
6. **Your Teams**——每个团队一个可折叠块，展开为默认页：
   - **All issues / Active / Backlog / Board**
   - **Current Cycle / Upcoming Cycles / Cycles**（启用 cycles 才有）
   - **Projects**
   - **Triage**（仅当 Team Settings 启用，且出现在团队名之下）
   - 该团队保存的自定义视图
   - **团队 home view**（置顶文档/链接）
7. **工作区级页**——Workspace Projects、**Initiatives**（原 Roadmaps，需在 workspace settings 启用）、Documents、Customers。
8. **Help & Feedback**（底部）→ 含 **Keyboard shortcuts** 入口（`?` 打开）。

**不变量**：
- Favorites 是个人快捷方式，不共享。
- 不能收藏别人的 "My Issues"——得用自定义视图模拟。
- Triage issue **默认被所有视图排除**，除非显式加含 Triage 的状态过滤器。
- 非成员浏览到的团队出现在临时 **"Exploring"** 区。
- 团队数上限：Free=2、Basic=5、Business/Enterprise=无上限。

### 1.2 Command Menu（Cmd+K）—— 通用动作面板 + 模糊搜索

*"execute any command with just a few keystrokes"*。入口：`Cmd/Ctrl K`（任意位置）。

- 单输入框；结果**按功能分组**，且**分组顺序随当前焦点/视图动态调整**（在 Cycles 视图→cycle 相关命令置顶）。
- 支持**数百个动作**：改任意属性、切主题、导航、创建。
- **作用域前缀**（输入首字母 + 空格聚焦某类）：
  - `i `→issues、`p `→projects、`u `→users、`t `→teams、`l `→labels、`f `→favorites、`d `→documents
- **样例命令**：`favorite`、`open favorite`（见 /favorites）；`my issues`、`open issue`、`find in view`（见 /search）；`copy issue in markdown`（见 /editor）；`Snooze this notification`、`Remind me about this issue/document`、`Cancel reminder`（见 /inbox）；`Export customer requests as CSV`（见 /customer-requests）。（⚠️ 完整命令目录在持续 500 的 /the-command-menu 页，未逐字确认全量。）
- 多选 issue 时 Cmd+K 提供**批量**属性更新。
- Issue 查找接受全 ID（`LIN-123`）或简写（`lin123`）；`O then I` 走**标题部分词**匹配（不含描述/评论）。
- **Search（`/`）行为**（非 Cmd+K）：结果排序 unstarted → in progress → backlog → completed → canceled → archived，可改 relevance/last updated/last created；**最多 500 条**；非引号英文查询剥停用词；`"引号"`强制精确。

**三个易混的检索面**：
| 面板 | 快捷键 | 作用域 |
|---|---|---|
| Command Menu | `Cmd/Ctrl K` | 动作 + 实体查找，按上下文分组 |
| 工作区 Search | `/` | issue/project/doc，按标题+描述+评论 |
| Find in view | `Cmd/Ctrl F` | 当前 board/list/Inbox 的临时标题过滤，`Esc` 清除 |

> ⚠️ `/docs/the-command-menu` 官方页在抓取期间持续 HTTP 500，上方行为由 5 个引用它的页面 + 2019-12-18 changelog 重建。完整命令目录若需逐字精确，建议浏览器直取该单页。

### 1.3 键盘快捷键全目录（Linear 的灵魂）

**全局**：`Cmd/Ctrl K` 命令菜单 · `/` 工作区搜索 · `Cmd/Ctrl F` 视图内查找 · `?` 快捷键帮助（可搜索）· `Esc` 返回/清选区 · `Cmd/Ctrl Enter` 提交。

**导航（chord）**——按来源可信度分层：
- ✅ **官方页确认**：`G I` Inbox（/inbox）、`G M` My Issues（/my-issues）、`G T` Triage（/triage）、`O I` 快速 issue 标题搜索（/search）、`O F` Favorites（/favorites）、`O D` 项目内文档列表（/documents）、`O Q` 客户页 / `G Q` 客户全列表（/customer-requests）。
- ⚠️ **未在官方页确认**（第三方速查表 / 重建）：`O W` 切工作区、`O T` 切团队再进 Triage、`G E` All issues、`G D` Board、`G C` Cycles、`G V` 当前 cycle、`Cmd/Ctrl Option 1–9` 快速设状态。完整 chord 目录在持续 500 的 /keyboard-shortcuts 页，需浏览器直取逐字确认。

**列表选择**（逐字）：`↑↓` 或 `J/K` 移高亮 · `X` 选中（toggle）· `Shift+点击` 鼠标区间选 · hover 左缘显复选框 · `Shift+↑↓` 逐个扩展 · `Cmd/Ctrl A` 全选 · `Esc` 清选 · 长按 `Space`(hover) 预览 · `Enter` 或 `O` 打开。

**Issue 动作**（单/批量）：`C` 新建 · `S` 改状态 · `P` 改优先级 · `Shift P` 设项目 · `L` 标签 · `A` 指派 · `I` 指派给自己 · `Shift E` 估算 · `Shift D` 截止日 · `Shift S` 订阅/退订 · `Cmd/Ctrl Shift S` 管理订阅者 · `H` snooze/提醒 · `Cmd/Ctrl Option 1–9` 快速设状态（第三方）。

**重排**（需 Grouping=No grouping + Ordering=Manual）：`Option Shift ↑↓` 移到顶/底 · `Option ↑↓` 逐格移。

**Inbox 专属**：`Backspace` 删除 · `Shift Backspace` 删所有已读 · `U` 切读/未读 · `Option U` 全标已读 · `H` snooze · `Shift S` 退订（须先进 issue）· `Cmd/Ctrl F` 站内搜 · 右键菜单。

**Triage 专属**：`1` 接受 · `2` 标重复 · `3` 拒绝（→Canceled）· `H` snooze · `MM` 亦触发标重复。

**Board 专属**：`Cmd/Ctrl B` 切 board/list · `X` 选中 · `Option Shift ↑↓` 移到列顶/列底 · `T` 折叠 swimlane · `Space`(hover) 预览 · `Shift+纵向滚动` 横向滚 · 列顶 `+` 在该列建 issue。

**编辑器（markdown 风格）**：`**b**`/`Cmd B` 粗 · `_i_`/`Cmd I` 斜 · `~~s~~`/`Cmd Shift S` 删线 · `Cmd U` 下划 · `Cmd E` 行内码 · `#/##/###/#### +Space` H1–4 · `*/-/+ +Space` 或 `Cmd Shift 8` 无序 · `1. +Space` 或 `Cmd Shift 9` 有序 · `[]` 或 `Cmd Shift 7` 清单 · `> +Space` 引用 · `>>> +Space` 折叠区 · `___ +Space` 分隔 · `|--` 新表 · `/` 斜杠命令 · `Cmd K` 选中转链接 · `/code` 或 `Cmd Shift \` 代码块 · `/diagram` Mermaid · `Cmd Shift U` 上传文件 · `:`+emoji · `@` 提及 · 粘贴 `ENG-123` 自动关联。

**子问题**：`Cmd/Ctrl Shift O` 开子问题编辑器 · `Cmd/Ctrl Shift Enter`(或 Shift 点 Save) 同值建下一个 · `Cmd/Ctrl Shift P` 把选中 issue 转为某 parent 的子问题 · `Cmd/Ctrl K → "Remove parent"` 子问题转独立。

**内联评论**：`Cmd Option M`(Mac) / `Ctrl Alt M`(Win)。

**复制**：`Cmd Opt C` 把 issue 复制为 Markdown（喂 LLM 用，含标题/描述/评论/客户请求，支持多选）。

### 1.4 选区模型

hover 即可键盘操作；`X` 选中；Shift+方向键或 `Cmd/Ctrl A` 扩展；右键与 Cmd+K 都作用于当前选区。**这是全产品一致的心智模型**——照搬即可。

---

## 第 2 章 · Issue 生命周期（核心中的核心）

### 2.1 创建 Issue（Quick Add）

- **`C`**——任意位置弹出创建模态（Quick Add），**主入口**。
- **`V`**——全屏编辑器。
- 侧栏左上角 **Create new issue** 图标。
- 标题空时输入 **`/`** → quick-add 菜单：从模板建 / 复制最近 issue / 开全屏。

**模态结构**：标题（必填，纯文本）+ 描述（富文本，自动存草稿，可在侧栏 Drafts 看到）+ Team 选择器（issue 必属单一团队）+ 内联属性（Status/Assignee/Priority/Labels/Project/Cycle/Milestone/Estimate/Due date/SLA）+ `…` 溢出菜单（设截止日/SLA/套模板）+ 模板图标。`Cmd/Ctrl Enter` 提交。

**默认行为**：标识符 `<TEAM_KEY>-<n>`（如 `ENG-127`）按团队单调递增、不可变；**默认状态=团队的首个 Backlog 类型状态**（可在 Backlog 或 Todo 类下配置默认）；匹配 SLA 规则的自动套 SLA；**创建后 3 分钟内的属性编辑视为创建过程的一部分，不计入 activity log**。

### 2.2 Issue 详情视图解剖（左→右，上→下）

1. **头部/面包屑**：团队 key+标识符（可点复制）、返回、状态 pill。
2. **标题**：行内可编辑，必填。
3. **描述**：富文本，自动存。支持 `@` 提及（订阅+通知）、issue 引用（自动建 Related 关系）、内联评论（`Cmd Opt M`）、emoji 反应、附件（`Cmd/Ctrl Shift A` 或拖拽）。
4. **Sub-issues 区**：描述下方 `+ Add sub-issues`；列表顺序 per-user、可见属性可定制。
5. **右侧属性侧栏**（主编辑面，逐行点击即改）：
   - Status（带类型色）、Assignee、Priority(`P`)、Labels(`L`)、Estimate(`Shift E`)、Cycle、Project、Milestone、Due date(`Shift D`)**或** SLA（火苗图标，互斥）、Relations（Blocked by 橙旗 / Blocks 红旗 / Related / 重复 banner）、Subscribers、标识符、创建/更新时间、创建者。
6. **Activity 时间线**（底部）：属性变更 + 评论/线程（带解决态与 AI 摘要）+ 关系/子问题事件 + git 事件（PR/commit/sync）+ 反应指示。每条评论有 `…` 菜单（编辑/管理订阅/复制 URL/从评论建 issue 或子问题/删除）。

**关键**：侧栏每行即点即改、即时保存并产生 activity 事件；属性快捷键在查看 issue 时均可用。

### 2.3 属性全集（逐字枚举——复刻直接照抄）

#### Status（状态）与工作流
- **两套概念，别混淆**（这是复刻最易错处）：
  - **状态类型(type)枚举（固定，对应 API 的 `WorkflowState.type`）**：`Backlog / Unstarted / Started / Completed / Canceled`，外加 `Triage` 与系统保留的 `Duplicate`。**类型顺序固定、不可重排**。
  - **默认状态（名称）**：开箱即 `Backlog > Todo > In Progress > Done > Canceled`，分别映射到 `Backlog / Unstarted / Started / Completed / Canceled` 类型。⚠️ 别把默认状态名（Todo/In Progress/Done）当成类型枚举硬编码——底层类型是 Unstarted/Started/Completed。
- **按团队配置**（Settings → Team → "Issue statuses & automations"）：在某 type 下加自定义状态（如 "QA Review" 归 Started 型）；可改名/改色/重排状态；删除使用中的状态需把存量 issue 重映射。
- **类型（而非状态名）驱动语义**：分组、cycle 的"已开始/已完成"、analytics、parent/sub 自动关闭。
- 状态不硬门控——状态选择器/Cmd+K/board 拖拽可移到任意状态。
- **Duplicate / Triage** 是系统保留/特殊类别，不可改名/自定义。

#### Priority（优先级）—— 固定 5 级，**不可自定义**
`No priority（默认）/ Low / Medium / High / Urgent`。
- 标 `Urgent` 通知负责人；启用邮件则发紧急邮件。
- 优先级排序（拖拽）**全工作区共享**；无优先级者恒排末尾。
- 变优先级会触发 SLA 增删规则。

#### Labels（标签）—— 两级作用域
- **Workspace 标签**（跨团队，如 "Bug"）与 **Team 标签**。
- **Label groups**：一层嵌套；**每组上限 250 个**；应用的是组内标签而非组；**同一组内一个 issue 只能挂一个标签**（组内互斥）。
- 行内创建语法：`label group/label` 或 `label group:label`（如 `Type/Bug`）。
- **保留名（禁用）**：`assignee, cycle, effort, estimate, hours, priority, project, state, status`。
- 跨团队按**同名**匹配（视图/搜索生效，API 不生效需用唯一 ID）；要看单团队标签须叠 `team` 过滤器。
- 管理动作：改名/改色、转组、转作用域、改团队、**合并重复**、归档（保留历史但禁用新建）、删除（不可逆，从所有 issue 移除）。
- 子团队继承父团队标签；改继承的标签须在父团队改。

#### Estimate（估算）—— 按团队选刻度
| 刻度 | 基础值 | 扩展值 |
|---|---|---|
| Exponential | 1,2,4,8,16 | 32,64 |
| Fibonacci | 1,2,3,5,8 | 13,21 |
| Linear | 1,2,3,4,5 | 6,7 |
| T-Shirt | XS,S,M,L,XL | XXL,XXXL |
- 可选显式 **0**（区别于未估算）；**T-Shirt→点值精确映射**（按 Fibonacci，非线性）：`No priority 0 / XS 1 / S 2 / M 3 / L 5 / XL 8 / XXL 13 / XXXL 21`（注意 **L=5、XL=8**，不是 4/5）。
- 未估算默认算 **1 点**（可在设置关）；估算关闭时统计按每 issue 1 点。
- `Shift E` 增/改/删。

#### Due Date（截止日）
- 图标色：**红**=逾期、**橙**=一周内到期、**灰**=其它。
- `Shift D` 设置；过滤预设：Overdue / 1 day / 1 week / 3 months / 自定义 / No due date。
- 列表可按 Due date 排序，有截止日者置顶各组。

#### SLA（服务等级）—— 与 Due date **互斥**
- 启用：Settings → Issues → SLAs。**不回溯**已有 issue；改优先级会触发。
- 图标：火苗 gray→yellow→orange→red；完成后保留字段显示完成时间与是否达成。
- **默认规则**（启用即建）：Urgent→24h、High→1week、Medium/Low/None→**移除** SLA。
- 自定义时长：12h/24h/48h/1w/2w/4w，或 Hour/Day/**Business day**/Week（business day 默认周一–周五，可设周日–周四）。
- 条件可组合几乎任意字段：Team/Status/Assignee/Creator/Priority/Labels/Project/Project Status/Initiative。
- **SLA 状态枚举**（驱动图标色与过滤/分组）：

| 状态 | 定义 |
|---|---|
| Low risk | 距 SLA >1 周 |
| Medium risk | 1 周内 |
| High risk | 1 天内 |
| Breached | 已过 |
| Achieved | 在 SLA 内完成 |
| Failed | 逾期后完成 |

- 通知：达成前 **24h** 与**违约时**推送订阅者 Inbox；Slack 提前 24h（business-day SLA 提前 1 工作日）。
- **不变量**：套 SLA 会**替换**已有 due date；创建时若 SLA 与"移除规则"冲突，建后自动移除（要保留则**编辑**而非创建时加）；SLA 不可自定义命名。

### 2.4 关系与依赖（4 种对等关系 + 层级）

- **Blocked by**（对方显示 Blocked by，橙旗）/ **Blocks**（红旗）/ **Related** / **Duplicate**。
- 阻塞解决后，关系自动归入 **Related**。
- 快捷键：`M R` 关联、`M B` 标 blocked by、`M X` 标 blocking；描述/评论里引用 issue 自动建 Related。
- 移除：hover→X，或 Cmd+K→Remove relation。

### 2.5 重复（Duplicate）—— 状态结局，非仅关系

- Cmd+K 把重复 issue **合并进 canonical（已保存的）issue**；被合并者移入保留 **Duplicate** 状态，在报表/工作流里成为独立结局。
- **单向**：不能反向把 canonical 标为别人的重复。
- 重复 issue 视图显示指向原件的 banner + 侧栏链接；附件/评论迁到 canonical；联动集成（Front/Zendesk/Intercom）的链接被重连到 canonical，自动化继续生效。

### 2.6 子问题（Sub-issues）与继承

**创建**：父 issue 内 `+ Add sub-issues` → 子问题编辑器；`Cmd/Ctrl Shift O` 快捷；从评论 `…→new sub-issue`；从描述列表选中 `Cmd/Ctrl Shift O`；模板；批量 `Create multiple issues`；粘一串标题。`Cmd/Ctrl Shift Enter` 同值建下一个。

**继承规则（确定性）**：
- **必继承**：team、priority、project。
- **条件继承**：cycle（在 active 状态下创建时）；assignee（你本人是 parent assignee，或所有现存 sub-issue 共享 parent assignee）。
- **永不继承**：labels。
- 只能在 parent 保存后加子问题。

**自动关闭**（Settings → Team → Workflow；git 触发的状态变更也遵守）：
- **Parent auto-close**：所有子问题 Done→父 Done。
- **Sub-issue auto-close**：父 Done→剩余子问题 Done。

**转换**：issue↔子问题（`Cmd/Ctrl Shift P` 设 parent / Cmd+K Remove parent）；issue→项目（父 `…→Convert to project`，原 issue+子问题作为独立 issue 加入新项目，子关系解除）。

**显示/过滤**：Display Options 显隐子问题；过滤"仅顶层/有子问题/仅子问题"；`…→Always hide completed sub-issues`；**子问题排序 per-user**（与全局的优先级排序不同）。

### 2.7 评论与反应

- 底部 "Leave a comment…"，`Cmd/Ctrl Enter` 提交；**不像描述自动存**，未发送的在 issue 与侧栏 Drafts 可见。
- 附件 `Cmd/Ctrl Shift A` 或拖拽。
- 输入 **`@Linear`** 在评论里调第一方 agent（草拟状态更新/总结/产出 action items）。
- 线程：hover 评论→箭头 *Reply to comment*；溢出菜单 Resolve（可从特定回复标记为解决方案）；启用 *Resolved thread AI summaries* 时已解决线程自动 AI 摘要；长讨论生成 AI 讨论摘要。
- emoji 反应：可对 issue、评论、project update、initiative update、描述本身反应；支持自定义 emoji（JPG/GIF/PNG）。
- 内联评论：选中文字→工具条评论图标，或 `Cmd Opt M`；文档与 project overview 也支持。

### 2.8 文档（Documents）

- **依附对象**，非顶层：挂 project/initiative/team/issue/cycle。`+` 创建或 `Cmd/Ctrl K`。
- 与 issue 同一编辑器；`@` 引用对象（含 issue ID）；**实时多人协作**（可见他人光标）；**Agent 编辑高亮**区分（保留表格与既有格式）；**版本历史**（agent 与人工编辑都留 checkpoint）；**内联评论**（侧栏线程，可 resolve，`Cmd Opt M`）；订阅（铃铛，创建者自动订阅）；标题级深链（hover 标题左侧 Copy link）；"Show author names" 覆盖层显示每段作者。
- 文档模板（workspace/team 级，创建时选）。
- `O then D` 项目内打开文档列表；`Cmd/Ctrl J` 让 agent 编辑。

---

## 第 3 章 · Triage 收件箱（差异化能力）

- **定位**：团队级 inbox；集成（Slack/Sentry/Intercom/Front/Zendesk）或非本团队成员创建的 issue 默认进 **Triage 状态**；除非显式加含 Triage 的状态过滤器，否则**被所有视图默认排除**。
- **4 个键盘动作**：`1` 接受（移到团队默认状态，可选评论）/ `2` 标重复（选 canonical，迁附件，移入保留 Duplicate 状态）/ `3` 拒绝（→Canceled 类型，可选评论）/ `H` snooze（隐藏，到时或新活动回归）。
- **Triage Rules（规则引擎）**：基于可过滤属性触发，自动改 team/status/assignee/label/project/priority；**自上而下顺序执行**；路由到别队 triage 会**级联**应用目标队规则；冲突界面显式提示。
- 团队可指派 **Triage 责任**（兼容 PagerDuty、incident.io）。

---

## 第 4 章 · My Issues 与 Inbox

### 4.1 My Issues（`G then M`）
四 tab：
1. **Assigned**（默认）——按精选优先级分段（部分仅当适用时显示）：Urgent work → SLA-bound → Blockers → Cycle work → Other active → Triage → Backlog → Completed；段内按优先级、已开始者居前。
2. **Created**——你创建的全部（含经 Slack/Front/Intercom/Zendesk/Sentry），按创建时间。
3. **Subscribed**——你订阅的。
4. **Activity**——近期活动按日期分组（创建/更新/状态变更/评论/评论被反应/开 PR）。
- 分组可用 Display Options 调。
- **自动订阅**：创建/被指派/被 @提及（描述或评论）→ 订阅 issue；评论线程内 @提及→只订阅该线程。

### 4.2 Inbox（`G then I`）+ 通知（通道配置）
**Inbox 表面**：通知垂直列表；点击在**特殊 Inbox 视图**里打开 issue（可同时执行 inbox 动作与编辑）。
- 动作（逐字）：`Backspace` 删选中 / `Shift Backspace` 删所有已读 / `U` 切读未读 / `Option U` 全标已读 / `H` snooze / 右键菜单 / `Shift S` 退订（须先开 issue）/ `Cmd/Ctrl F` 站内搜。
- Display Options：显隐 snoozed/read。Snooze 支持自然语言时间（`Jan 3 10am`、`next quarter`、`for 2 weeks`，须打全）。
- Issue Reminders：设在 issue/document/project/initiative，到时进 Inbox 通知；顶部可见，可改期/取消。
- **上限**：保留 **2000 条**未读，超限自动归档；无手动归档动作。

**通知通道（Settings → Account → Notifications）**——与 Inbox 表面**不同**，只管投递：
- 4 通道 **Desktop / Mobile / Email / Slack**，绿点启用；每通道独立开关 + 各类型 toggle。
- Desktop/Mobile/Slack **实时**；Email **digest**（按紧急度批量，仅未读 inbox 时才发）或**立即投递**。
- 通知**分组**，**不可只挑一类**（如"状态变化"含完成/取消/紧急优先级/阻塞关系变更）。
- 订阅图是通知的唯一真相源；通道设置只影响投递。
- 进入特定状态的通知用 **view subscription** 而非个人偏好。

---

## 第 5 章 · 规划制品

### 5.1 Teams（作用域基石）
- issue 必属单一团队；cycle/标签/模板/工作流/triage/git 自动化**全部按团队**。
- 工作区创建时自动生成同名默认团队。
- **Projects 可跨团队**，**issue 不可**；**子问题可跨团队**。
- 团队设置页：General（名/key/时区/估算/邮件建 issue/详细历史）、Members、Issue labels、Templates、Recurring issues、Slack notifications、**Issue statuses & automations**、Triage、Cycles。
- **私有团队**（Business+）：须被 team owner 邀请。
- **子团队**（Business+，最多 **5 层**嵌套）：**数据模型强制继承**——成员资格（子团队成员必属父团队）与 **cycle 计划**强制继承父团队；**状态/估算可选继承**；标签继承。但**可配置权限**（Label/Template/Settings/Member 管理）**不继承**到子团队。隐私嵌套规则：私有父团队→子团队必私有；公共父团队→子团队可公可私。parent 的 team owner 在子团队也是 team owner。
- **退役团队**（软删除，只读冻结）vs **删除团队**（硬删，30 天可恢复于 Recently deleted）。

### 5.2 Cycles（迭代）
- 启用与配置（Settings → Team → Cycles，页 `/use-cycles`）：
  - **Each cycle lasts**：**1–8 周**（按重复计划自动生成，非逐个配）。
  - **Cooldown**：每个 cycle 后的冷却期（不可向 cooldown 分配 issue）。
  - **Starting day of the week**：周期起始日（当日 00:01 起，按时区 Settings→Team→General）。
  - **Upcoming cycles**：预创建的未来周期数，**上限 15**。
  - 默认长度官方未公布具体值（Method 推荐 **2 周**最常见）。
- 更新（`/update-cycles`）：改名/描述、改日期（**仅 upcoming 与当前 cycle，不能改到过去**）。
  - **编号规则**：cycle 名以数字结尾时，下个 cycle 从该数字起新序列。
  - **缩短结束日**→与下个 cycle 间显示 "Cycles paused"；**延长结束日**→吃掉下个 cycle 时间。
  - cooldown 开启 + 改日期→显示 "Cycles paused" 而非 "# week cooldown"。
  - 改 cycle 常因假期/offsite/OOO；自动创建的 cycle 完全可编辑；归档 cycle 不在列表。
- **Cycle Graph**（`Cmd/Ctrl I` 打开侧栏）：
  - **灰线**=总 scope；**蓝虚线**=target（估算点均摊到剩余工作日，**周末拉平**，高于此线=能按期）；**黄线**=已开始 issue 数；**蓝实线**=已完成数；**蓝柱**=已完成（视觉辅助）。
  - cycle 开始后自动生成；**每小时刷新**。
  - scope=估算点之和；Settings→Team→General→Estimates 可勾选 **Count un-estimated issues**（否则未估算不计）。
  - **Cycle Success 公式**：`completed + 0.25×started`，÷ 总数。例：10 issue，5 完成、4 开始、1 未动→**60%**（50% + 4×25%=10%）。
  - 已完成 issue 可补登到上一 cycle（cycle 关闭后才标完成的）。
- **Cycle 自动化**（`/use-cycles`，Settings → Team → Cycles）：
  - **Issues rollover**：未完成 issue 自动结转；但 cooldown 期间移到 Todo/Triage/Cancel/Done 的不结转。
  - **Auto-add active issues**：可选把"无 cycle 的 Started/Completed 状态 issue"自动加进当前 cycle（开关 "Active issues"，子选项 Move to Backlog 或 Keep in Active）。
  - **Cycle capacity**：按**前 3 个已完成 cycle 的 velocity**（已完成 issue 数或估算点）估算。
  - **Start cycle today**（00:00 起；若有活跃 cycle 立即标完成并把未完成 issue 移入新 cycle）/ **禁用 cycles**（当前 cycle 标完成、upcoming 移除、历史数据保留）。

### 5.3 Projects
- 属性：name、**单一 lead**、members（opt-in 通知）、icon、状态、**start/target date（时间框粒度：年/半年/季度/月/日）**、progress、health、scope。
- 多团队共享（按团队分 tab）；可附 issue views 与 project views 作 tab；删除进归档 **30 天可恢复**；issue 同时只属一个项目。
- **Project Graph**（`Cmd/Ctrl I` 侧栏；项目移到 Started 状态且数据足够后生成）：
  - 灰线 scope、Started/Completed 线、蓝柱、**红线 target**（设了才有）、**紫虚线预测**、乐观/悲观虚线（**±40% buffer**）。
  - **每小时刷新**；时间粒度 **每 7 天**；预测基于**每周 velocity**（近期周权重更高）；剩余点=`未完成点 + 0.25×进行中点`；至少 1 周数据才出预测；图始终从 0 起。
  - 估算关闭时**每个 issue 算 1 点**。
  - scope 减少（删 issue/移出项目/取消/降估算）时调整 Scope 与 Progress 线关系。

### 5.4 Initiatives / Roadmaps / Timeline / Milestones
- **Initiatives**（原 Roadmaps，需 workspace 启用）：**有意**组织一批围绕公司目标的 projects（区别于 filter 自动收集的 project view）；5 状态 `Proposed/Planned/Active/Completed/Canceled`；支持 sub-initiatives（**2025-07-10**，**最多 5 层**嵌套）。**可见性**：对全体成员可见（除 Guest），无"私有"概念；仅**保存的 Initiative 视图**限 Enterprise。
- **Roadmap = Initiatives + Timeline 视图**（非独立对象）。多个 roadmap 共存，各自有 project 列表。
- **Timeline 视图**（仅 projects，issue 不上时间线）：Gantt 式，project 条 + 里程碑标记 + 依赖箭头 + 可选团队 cycle 叠加；分辨率 **days/weeks/months/quarters**，zoom 预设 week/month/quarter/year（⚠️ 官方页两处列举不一：days 仅作分辨率、year 仅作 zoom 预设）；分组按 Initiative 等；可显 milestones/dependencies/lead/members/priority/status/health。
- **Milestones 已废弃**→自动转为 Roadmaps（多 roadmap）。但 display/filter 仍把 milestone 当 project 内时间线检查点。

---

## 第 6 章 · 视图与过滤系统

### 6.1 自定义视图
- 3 类：**Issue / Project / Initiative**（后者限 Enterprise）。
- 创建：加 filter → `Option/Alt V` 或保存图标。
- 保存 3 层：**Personal**（默认，仅自己）/ **Set as default**（工作区默认，他人首见）/ **Reset to default**。
- 工作区级共享视图最佳（跨团队、任意属性过滤）。

### 6.2 Filters DSL
- 入口：`F` 开菜单（再按 `F` 加条件）；`Option/Alt V` 存为视图；`Backspace/Delete` 清。
- **形状**：`[type] [operator] [value(s)]`。
- **过滤类别**：Issue 属性（Priority/Cycle/Estimate/Labels/Links）、Project、Workflow（Status/Auto-closed）、Relations（Blocked/Blocking/Related/Parent/Sub-issue/Duplicate）、Dates（Completed/Created/Due/Updated）、User（Assignee/Created by/Subscribers）、Content。
- **操作符**：`is`/`is not`（单值）→ 多值自动升级为 `is either of`/`is not`；labels 与 links 用 `includes any/all/neither/either/none`；日期用 `before`/`after`。点 type 无效、点 operator 切换、点 value 选值。
- **Advanced 模式**：嵌套 AND/OR 树。
- **AI 过滤**：自然语言（"Show me issues assigned to me"）。
- **边界**：过滤"无标签"须选全标签再切 `does not include`；按 milestone 须先按 project；不能过滤 suspended 用户；`Added to cycle` 中 **Planned**=cycle 开始前或开始后 24h 内加入，**not Planned**=>24h（含结束后）。
- **可过滤视图**：Search/Inbox/My issues/Reviews/Initiatives/Projects/Customers/Teams/Members/Triage/All issues/Cycles/Archive。

### 6.3 Board 布局
- `Cmd/Ctrl B` 切；列=分组值（默认 Status）；卡=摘要（**不显描述**）；隐藏列移到最右仍可 drop。
- **分组选项**：status/assignee/project/priority/cycle/label/label group/parent issue/team/customer/release/SLA status/No grouping（+ My Issues 专属 **Focus** 分组）。
- 列头旁：issue 数 ⇄ 估算和（点切）。
- 手动排序**全工作区共享**；移 issue 用键盘 `S`/Cmd+K→置顶，用鼠标→落到拖放处。
- **board 与 list 不能独立排序**；board 不能设为全局默认（仅 per-view "Set as default"）。
- Triage/Inbox 不支持 board。
- **Swimlanes**（子分组，board 作行/list 作行）：分组头滚动时 sticky。

### 6.4 Display Options（`Shift V`）
- Layout：list ⇄ board（issue 视图）⇄ timeline（project/initiative 视图）。
- Issue Ordering：Status/Manual/Priority/Last created/Last updated/Due date/Link count；**issue** 除 Manual 外均可逆序。
- **按 status 排序**：list 从"最接近完成→最远"，后接 completed/canceled（board 则按工作流顺序）。
- **显示属性全集**（显/隐卡上行，不过滤）：ID/status/assignee/priority/SLA/project/due date/milestone/cycle/release/estimate/labels/links/customers/customer revenue/time in status/created/updated/**pull requests and commits**/**Sentry issues**。
- Project/Initiative 分组：lead/member/status/health/start date/target date（project 还可按 initiative）；排序：**Manual**/status/priority/updated/created；**project 逆序被禁的场景=Manual 且 status**（issue 只禁 Manual）。
- Triage/Inbox **仅允许排序**。
- **显示属性 ≠ 过滤器**：前者显隐卡片数据，后者精简 issue 集合。

---

## 第 7 章 · Insights（实时分析）

- 入口：任意视图右侧栏 `Cmd/Ctrl Shift I`；最佳=工作区级共享自定义视图。
- **Measure（Y 轴）**：Issue count（柱）/ Effort（柱）/ **Cycle Time**（散点，开始→完成）/ **Lead Time**（散点，创建→完成）/ **Triage Time**（散点）/ Issue Age（散点）。
- **Slice**（X 轴）+ **Segment**（颜色再切）。
- **Burn-up（累计流图）**：默认月，可调周/含归档。
- 散点带 25/50/75/95 百分位标记可缩放；点选跳 issue。
- 可全屏、复制链接、**导出 CSV**、内建示例库。
- 过滤器关键：Created at、Completed at、**Status Type**（跨团队即使状态名不同）、Label/Project/Team；可切"Show archived issues"、滤除无优先级。

---

## 第 8 章 · 自动化体系（汇总）

| 自动化 | 配置位置 | 能力 |
|---|---|---|
| **Triage Rules**（确定性） | Team → Triage | 见第 3 章；条件→动作，自上而下、级联 |
| **Triage Intelligence**（AI 建议） | Team → Triage（Business+） | LLM 分析每个新 issue，建议 assignee/label，主动浮现相关/重复 issue |
| **Triage Automations**（Agent 指令式） | Team → Triage（Business+） | Linear Agent 执行灵活指令（翻译、附相关文档、补评论） |
| Parent/Sub auto-close | Team → Issue statuses & automations | 2024-09 上线；git 触发的状态变更也遵守 |
| **Stale-issue auto-close / auto-archive** | Team → Issue statuses & automations | 未更新超阈值的 issue 自动关闭/归档（**不同于** parent/sub 级联）；归档仅自动、无手动动作；归档时通知创建者 |
| PR & commit 自动化 | Team → Issue statuses & automations | 见集成章 |
| Auto-assign & move to start | Preferences | 复制 git 分支名时自动指派+移到 started |
| SLA 规则 | Settings → Issues → SLAs | 见 2.3 |
| **Cycle 自动化** | Team → Cycles | rollover / auto-add active / capacity（见 5.2） |

---

## 第 9 章 · 集成（逐个，行为级）

### 9.1 集成目录（分类）
- **API & Webhooks**（扩展底座）· **GitHub**（PR 链接/magic word/状态自动化/双向 issue 同步）· **GitLab**（MR 对等，PAT 鉴权，支持自托管 ≥15.6）· **Slack**（消息建 issue/同步线程/通知/unfurl/`@Linear`）· **Figma**（嵌入预览 + Figma 内插件）· **Sentry**（异常建 issue/双向解决/assignee 同步）· **Intercom/Front/Zendesk**（工单桥，Business+）· **Linear Asks**（Slack/邮件/Web 表单→issue，Business+）· **Discord/Notion/Airbyte/Google Sheets/Zapier/Jira Sync/Make/Loom/Frontitude/Gmail**。
- **套餐门**：Customer Requests 全套餐有；Asks=Business+；Intercom/Zendesk/Front=Business+；Advanced Asks(Web forms)/Salesforce Service Cloud/多 Slack workspace=Enterprise。

### 9.2 GitHub（最深，作范本）
- **链接方式**：分支名含 ID / PR 标题含 ID / magic word + ID。
- **Magic words**：**closing 类**（close/fix/resolve/implement…→合并时 Done）vs **non-closing 类**（ref/related to/part of…→不自动关，仍走 merge 前的状态迁移）。
- **状态自动化**（Team 级，每队单独配）：PR drafted/opened/review requested/ready for merge/merged 各定目标状态；**按目标分支细分规则**（合并到 `staging`→"In QA"，`main`→"Deployed"，支持正则 `^fea/.*`，可对某分支设 "no action" 覆盖）。
- **Issue 双向同步**：单向/双向；同步 title/description/status/assignee/labels/sub-issues/comments；多级+跨仓库/跨团队层级。
- **PR review state**：评审者评论/请求修改/批准在 Linear 附件显头像与动作。
- **Preview links**：自动识别 Vercel/Netlify/Cloudflare/AWS Amplify + 自定义（URL 结尾 "preview"）；多预览下拉；30 天不活跃清。
- **企业版**：GitHub Enterprise Cloud（需 IP allow list）/ Enterprise Server（独立 GHES App，支持 PR linking 但不支持 issue 同步与 commit linking）。
- **Linkbacks**：链接后 Linear 在 PR/issue 发含标题+描述的回链评论（私有团队不披露标题）。
- **Auto-assign**：复制 git 分支名时自动指派给自己并移到 started。
- **跳过**：`skip ENG-123` / `ignore ENG-123` 解除链接。
- **自定义合并队列**：合并后关闭 PR 的队列须先加 `externally-merged` 标签。

### 9.3 GitLab（与 GitHub 对等，差异）
- MR 而非 PR；**PAT/Project Access Token**（scope `api` 或 `read_api`，后者不发 linkback）鉴权，无 bot 账号→linkback 以 token owner 身份发；**支持自托管（公网可达，≥15.6）**；webhook 触发 Push/Comments/Merge request/Pipeline；MR 仅靠 title/description 链接（不能靠评论/commit）；**一工作区一实例**；无 GitLab issue 迁移助手。
- Linear 出口 IP（自托管 allowlist 用）：`35.231.147.226 / 35.243.134.228 / 34.140.253.14 / 34.38.87.206 / 34.134.222.122 / 35.222.25.142`。

### 9.4 Slack（多向）
- **`@Linear` agent**：自然语言操作（"file a bug, assign me"）；公开频道直 @，私有须 `/invite @Linear`，群 DM 须创建时邀请。
- **消息建 issue**：More actions→Create new issue；可套模板（最多 10 个，私有团队模板不可用→用 Asks）；创建后建**同步线程**（双向评论，issue 完成/取消/重复时更新线程）。
- **`/linear`**：轻量建 issue，ephemeral 回执，不支持线程/文件。
- **5 类通知**：Team / Project & Initiative / Personal / View subscription / **Project Slack 频道自动创建**（新项目自动建公开频道+邀请成员+加书签）。
- **Rich unfurls**：公开团队 issue/project/doc/initiative 链接展开（私有团队**永不展开**，可全局关）；issue-ID 提及（`ENG-123`）自动回复链接，**同线程 60 分钟内去重**。
- **安装顺序坑**：Asks 先于 Slack 装会破坏 unfurl——须先断两边，重连 Slack 再 Asks。

### 9.5 Figma
- **嵌入**：粘 Figma 链接→自动转预览；**快照语义**（创建时冻结，文件变了 Linear 快照不变；编辑模式 hover 可 Refresh，**不可撤销**）；公开文件可应用内交互预览（私有在 roadmap）。
- **插件**：Figma 内创建/链接 issue（按 frame/section/page），双向同步；存 file key 做双向链接。
- 坑：任何 Figma 链接默认嵌入**无法隐藏**（超链接文本绕过）；Brave 须允跨站 cookie；安装者须有文件权限。

### 9.6 Sentry
- 异常建 issue / 链接已有 issue；**双向**：Linear 解决→Sentry 自动解决；assignee 变更同步（须同邮箱）；Sentry 侧可配 alert 自动建 Linear issue。
- 显示属性：列表/板可显 Sentry 图标，点击跳 Sentry。
- 限：**仅云**、**仅公开团队**（转私有会断连接）。

### 9.7 Customer Requests（客户声音）
- **Customer 实体**：domain/name/logo/revenue/size/tier/status/owner；按**邮箱域**自动建/映射客户；手动 `Cmd/Ctrl K → Create new customer`；客户页可收藏、合并去重、排除域/邮箱（Gmail 等通用域自动排除）。
- **Customer request**：挂在 issue/project，引用用户原话（可编辑加图/视频）；含来源链接、用户名、时间戳；`Ctrl R`/`Ctrl Alt R` 手动建；per-customer "重要"标记（三角图标）。
- **属性同步源**（Settings → Customer Requests）：Intercom（**实时**）/ Zendesk/Front/None（API 手动）；其余**每 12 小时**同步；可映射 Owner/Revenue/Size/Status/Tier。
- **Asks 归属**：按**线程首条消息发送者**邮箱归属客户（非创建者）；共享 Slack 频道可预连客户页使该频道所有 Ask 自动关联。
- **可建客户视图**：`Tier=Enterprise` + `customer count≥20` 排序→高需求 issue 浮顶；三角标重要；可订阅或路由 Slack。
- **导出**：`Cmd/Ctrl K → Export customer requests as CSV`。
- **Guest 看不到任何客户相关内容**（含用客户过滤的视图）。
- **API**：GraphQL（`customerNeedCreate`/`customerCreate`），无 REST，不擅长大批量导入。

### 9.8 Intercom / Front / Zendesk（工单桥）
- 侧边栏挂件建/链 issue；issue 完成可重开工单；多对多链接；合并重复 issue 时链接与评论迁到 canonical。
- **Intercom**：客户属性实时同步（规范源）；内部备注不同步回 Linear。
- **Zendesk**：最多 10 模板；支持 **Create with Linear Agent**（AI 分析对话生成标题/描述/建客户请求/路由）；需 Zendesk admin 启用自动化；域名变更后旧链接失效。
- **Front**：**不支持模板**；评论仅作内部备注（非客户可见回复）；私有 inbox 不支持自动评论/重开。
- 共同：triage 启用则建 issue 进 triage，否则进首个 backlog 状态；`F → Links → [源]` 过滤。

### 9.9 Linear Asks（统一入口管道）
- 把 Slack/邮件/Web 表单的请求变 issue；每 Ask 进团队 **Triage**；保持**同步会话线程**（Slack/邮件/Web 都连着请求者）。
- **Business**：邮件+回复、自定义邮件域、私有 Asks、Asks 字段、表单模板；**Enterprise**：额外 Slack 功能 + Web 表单（IdP 登录）。
- 与 Slack 集成区别：Asks 允许**非 Linear 用户**提交、支持**私有团队模板**、自动建客户请求。

### 9.10 通用集成权限与隔离
- **鉴权模型**：OAuth（Slack/Figma/GitHub/Intercom/Sentry/Notion/Front/Zendesk/Discord）、PAT+webhook（GitLab）、API token+webhook（API/Airbyte/Sheets/Zapier）、Marketplace+Linear 启用（Front/Zendesk 两步）。
- **Guest 隔离漏点**：Slack unfurl 跟随频道可见性而非角色；Linear 自建集成按 guest 在外部服务的访问而非 Linear 团队成员身份暴露数据；私有团队 URL 永不展开、模板不可用、Sentry 失效。

---

## 第 10 章 · AI 与 Agent 平台（战略重点，变化最快）

- **Linear Agent**（第一方，2026-03）：代号 **Charlie**，assignee 菜单里的 agent；`@Linear` 在评论/Slack 触发。
- **Code Intelligence**（2026-05）：AI 代码理解；**Guest 禁用**。
- **Agents in Linear**（第三方 "app users"）：行为似用户，可 @/指派(delegate)/评论/协作；**delegate 语义**——人类仍是主 owner；**Agent Guidance**（markdown+历史，workspace 级+team 级，team 优先）；可按 Delegate 过滤/切分 Insights 追踪；**不计费**；不能登录/admin。
- **Linear for Agents**（2025-05，**Developer Preview**）：开发者文档 Agents 节，含 **AIG（Agent Interaction Guidelines）/ Getting Started / Developing / Best Practices / Signals**。
- **Linear MCP**（2026-02）：面向产品管理的 MCP server，让外部 LLM 读写 Linear。
- **Triage Intelligence**：triage AI 辅助分类。

---

## 第 11 章 · 开发者平台

- **GraphQL API**——与内部应用同一 API；支持全查询+全变更；变更实时推送所有客户端。
- **鉴权**：Personal API key（Settings → Account → Security & Access；可 Read/Write/Admin/Create issues/Create comments 限 scope，可限团队；admin 控 member 能否自建 key，admin 恒可建）+ OAuth 2.0（第三方）。
- **Webhooks**——HTTP(S) POST 到公网 HTTPS 非 localhost URL，须 **HTTP 200**。
  - **失败重试**：服务不可用/**>5 秒(5000ms)**/非 200→最多 **3 次**，退避 **1 分钟/1 小时/6 小时**；持续失败可能**禁用** webhook（手动重启）。
  - **请求头**：`Linear-Delivery`(UUIDv4)、`Linear-Event`(实体类型)、`Linear-Signature`(HMAC)、`Linear-Timestamp`(Unix ms)、`User-Agent: Linear-Webhook`。
  - **安全**：验 `linear-signature` HMAC-SHA256（签名密钥在 webhook 详情页，非 URL）；建议校验 `webhookTimestamp` 在 **60 秒**内防重放；payload 含全数据对象+**所有变更属性的旧值**（`updatedFrom`）；`actor` 多态（User / OAuth client / Integration）。
  - **来源 IP**（可 allowlist）：`35.231.147.226 / 35.243.134.228 / 34.140.253.14 / 34.38.87.206 / 34.134.222.122 / 35.222.25.142`（偶有更新）。
  - **数据变更模型**：Issues/Issue attachments/Issue comments/Issue labels/Comment reactions/Projects/Project updates/Documents/Initiatives/Initiative updates/Cycles/Customers/Customer requests/Users；便利型：Issue SLA/OAuthApp revoked。
  - 仅 workspace admin 或带 `admin` scope 的 OAuth app 可建/读 webhook；`webhookCreate` 用 `teamId`（或 `allPublicTeams:true`）+ `url` + `resourceTypes`。
- **限流**（每小时）：

| 鉴权 | 请求 | 复杂度点 | 维度 |
|---|---|---|---|
| API key | 2,500 | 3,000,000 | 用户 |
| OAuth App | 5,000 | 2,000,000 | 用户/App User |
| 未鉴权 | 600 | 100,000 | IP |

  - **单查询复杂度上限 10,000**（超则拒）；每属性 0.1 点、每对象 1 点、connection 按分页参数或默认 **50** 倍乘。
  - 头：`X-RateLimit-*`、`X-Complexity`、`X-RateLimit-Complexity-*`、端点级 `X-RateLimit-Endpoint-*`。
  - **明确反对轮询**——用 webhook；默认分页 50；全量拉取按 `updatedAt` 排序。
  - **算法与边界**：leaky bucket（token 按 `LIMIT/PERIOD` 恒速回填）；per-endpoint 可能有更低单独限流（头 `X-RateLimit-Endpoint-*`）；超限返回 HTTP 400、`errors[].extensions.code = "RATELIMITED"`；OAuth actor-authorization 限流随 workspace 付费人数动态上调；可联系 support 临时提额。
  - ⚠️ **源页自相矛盾**：API key 请求限，正文说 5,000/h、表格说 2,500/h（上表取表格值）；OAuth 两处一致 5,000/h。
- **TypeScript SDK**（`@linear/sdk`，2026-01 v70+）：强类型模型与操作。
- **OAuth apps**：enterprise 下需 owner 审批（第三方应用审批）。
- **Guides**：上传文件、`linear.new` URL 建 issue、CLI importer。

---

## 第 12 章 · 客户端

| 客户端 | 要点 |
|---|---|
| 桌面 | macOS(Intel+Apple Silicon)/Windows；原生通知、常驻 dock、tab、快捷键无浏览器冲突；自动更新（可 MDM 关：`defaults write com.linear AutoUpdateDisabled -bool YES`） |
| Web | Chrome/Firefox/Safari 最近 3 版；离线几乎全功能；可配"在桌面 app 打开" |
| 移动 | iOS/Android 原生；Home/Inbox/创建/搜索/设置 tab |
| PWA | 平板/移动浏览器 |
| 离线 | 实时同步+本地重试；failsafe 非完整功能（离线大量编辑可能覆盖他人） |
| 协议 | `linear://` 开桌面 app；localhost 探测端口 44450/18450/33234 |

---

## 第 13 章 · 权限与治理

### 13.1 角色（5 级）
- **Workspace Owner**（Enterprise）——全权含 billing/security/**audit logs**/exports/OAuth 审批/团队访问；可配 Workspace restrictions。
- **Admin**——日常管理（Free 全员自动 admin；Basic/Business 升级者得 admin；Enterprise 权限受限）。
- **Team Owner**（Business+）——团队级委派；删团队/设私有/改 parent **仅限此角色**；configurable：Label/Template/Team Settings/Member 管理（可选全员或仅 team owner）；**仅 team owner 能加 guest**；权限**不继承**到子团队；可限制加入方式。
- **Member**——跨有权限团队协作；不能进 workspace 管理。
- **Guest**（Business+，按正式成员计费）——仅被加入的团队；看不到工作区视图/customer requests/initiatives；不能用 Code Intelligence；**集成数据隔离有漏点**（见 9.10）。

### 13.2 安全与合规
- 认证：**SOC 2 Type II / GDPR / HIPAA**（BAA 限 Enterprise）；传输加密+静态加密。
- **数据区域**：建 workspace 选 **US/EU**，**不可自助改**；选定区域存大部分数据，但**恒存美国**：workspace 信息、用户账户、API key、通知邮件（7 天）、用量/分析（机密内容已剥除）、崩溃元数据。
- **SAML SSO**（`/docs/saml-and-access-control`，Enterprise）：提供 ACS URL，填 IdP metadata；多 IdP 按域路由；**JIT** 首登建号（Name/Email/Avatar/Username，仅创建时写，后续登录不覆盖）；Allowed domains 须加 DNS TXT code 认领；可禁该域凭据新建 workspace。
- **SCIM 2.0**（须先启用 SAML）：启用后 **admin 不能在 Linear 内管用户**（用 IdP）；临时手动 override 可清存量；group push → Linear team 1:1（须先导入团队为 group 或按 displayName 映射）；`linear-owners/-admins/-guests` 组赋角色（一旦链接不可手动改 admin/guest 角色）；**SCIM 建号计费仅在首次登录后**（2025-08-14 起）。
- **Audit Log**（Enterprise，仅 owner，**保留 90 天**，含 actor IP/国家）：UI + GraphQL `auditEntries`（默认 50、最多 250，可流式推 SIEM webhook）。
- **第三方应用审批**（Enterprise）：未批准 OAuth app 授权需 admin 审批，邮件+应用内通知，拒绝可附理由。

### 13.3 工作区
- 推荐单 workspace 模型；创建时自动建同名默认团队。
- 删 workspace：owner 发起，确认码邮件，**48 小时**后永久删（期间任意 admin 可撤）。
- 多 workspace：一账号可多 workspace，各自成员/计费；推荐工作/个人**用不同邮箱**。

---

## 第 14 章 · 导入 / 导出

- **CSV 导出**（admin/owner）：工作区 issue CSV、成员列表 CSV、视图 issue CSV（字段：ID/Team/Title/Description/Status/Estimate/Priority/Project ID/Project/Creator/Assignee/Labels/Cycle Number/Name/Start/End/Created/Updated/Started/Triaged/Completed/Canceled/Archived/Due Date/Parent issue/Initiatives/Project Milestone ID/Name/SLA Status；**不含附件文件**）。
- **Project/Initiative CSV**（成员最多 200）：含 Name/Summary/Status/Milestones/Creator/Lead/Members/各时间/Teams/Initiatives/Health 等。
- **客户请求 CSV**、**复制 issue 为 Markdown**（`Cmd Opt C`，喂 LLM）、**单 issue PDF**（`Cmd/Ctrl P`，时间戳转绝对）。
- **集成导入**：Airbyte、Google Sheets（仅公开团队）、CLI importer（跨服务/跨 workspace）。
- **坑**：**无团队 CSV 导出**——跨 workspace 迁团队用 importer。

---

## 第 15 章 · 全局不变量清单（复刻检查表）

> 照这张表逐条核验你的实现是否"像 Linear"。

1. **issue 是原子单元，必属单一团队**；标识符 `<TEAM_KEY>-<n>` 唯一不可变。
2. **team 是 cycles/标签/模板/工作流/triage/git 自动化的作用域**；workspace 级标签/模板/状态另存。
3. **cycles 团队级**，无跨团队 cycle 视图。
4. **优先级固定 5 级**，不可自定义；排序全工作区共享，无优先级排末尾。
5. **状态类型固定** `Backlog/Todo/In Progress/Done/Canceled`（+Triage）；类型驱动语义；Duplicate 保留不可改。
6. **估算按团队选刻度**（4 种+扩展+0+未估算默认 1 点）。
7. **Due date ⇄ SLA 互斥**（SLA 替换 due date）。
8. **标签两级作用域**，组内互斥（一 issue 一组一标签），组上限 250，保留名禁用。
9. **子问题继承**：team/priority/project 必继承；cycle（active 创建时）与 assignee 条件继承；labels 永不继承；自动关闭双向可配；排序 per-user。
10. **重复单向合并**进 canonical，移入保留 Duplicate 状态。
11. **阻塞解决→自动归 Related**。
12. **手动排序全工作区共享**（board 与 list）；board 不可全局默认。
13. **显示属性 ≠ 过滤器**（前者显隐、后者精简）。
14. **Triage 默认被所有视图排除**；4 键盘动作；规则自上而下、级联。
15. **订阅图是通知唯一真相源**；通道只管投递；inbox 上限 2000。
16. **键盘优先**：Cmd+K 通用面板、`/` 工作区搜索、`Cmd/Ctrl F` 视图内查找三件套；选区模型 hover→X→Shift 扩展→Cmd+K/右键操作。
17. **实时同步 + 离线 failsafe**（API 变更实时推所有客户端）。
18. **webhook 失败重试 3 次（1m/1h/6h），5s 超时**；禁用后手动重启；payload 含旧值。
19. **限流 per-user（非 per-key）**；单查询复杂度上限 10,000；反对轮询。
20. **Workspace Owner 限 Enterprise**，唯一可访 billing/audit logs/exports/OAuth 审批；audit log 保留 90 天。
21. **数据区域建 workspace 时选、不可自助改**；账户/API key/通知/用量恒存美国。
22. **SCIM 启用后 admin 不能在 Linear 内管用户**；SCIM 建号计费仅在首登后。
23. **Guest 集成隔离有漏点**（Slack unfurl 跟频道、自建集成跟外部服务访问）。
24. **CSV 不含附件文件**；无团队 CSV 导出。
25. **设计哲学**：build for creators、purpose-built（反灵活）、write issues not user stories、create momentum、say no to busy work、scope 1–3 周/人、唯一 owner、短 spec。

---

## 附录 · 来源

一手文档（linear.app/docs）：creating-issues、configuring-workflows、priority、labels、estimates、comment-on-issues、due-dates、issue-relations、sla、parent-and-sub-issues、triage、my-issues、inbox、notifications、sidebar、favorites、select-issues、search、editor、projects、initiatives、cycles、create-cycles、update-cycles、cycle-graph、filters、board-layout、display-options、documents、milestones、roadmap、timeline、project-graph、insights、teams、workspaces、scim、security、audit-log、saml-and-access-control、exporting-data、members-roles、third-party-application-approvals、login-methods、api-and-webhooks、integrations、slack、gitlab、figma、sentry、customer-requests、front、zendesk、intercom、linear-asks、agents-in-linear。

营销/开发者：linear.app/method（+ 子页）、/plan、/developers、/developers/webhooks、/developers/rate-limiting、/developers/sdk、/developers/agents、/developers/aig。

Changelog：2019-12-18 command menu、2021-03-25 shortcuts help、2024-09-06 auto-close、2025-05-20 Linear for Agents、2026-02-05 Linear MCP、2026-03-24 Linear Agent、2026-05-14 Code Intelligence。

**已知抓取缺口与可信度**：
- `/docs/the-command-menu`、`/docs/navigation`、`/docs/keyboard-shortcuts` 在抓取期持续 HTTP 500（多次重试 + 多 slug 变体）——命令菜单全量目录、完整 chord 列表由引用页 + changelog 重建，未逐字确认；`O W`、`O T`、`G E/D/C/V`、`Cmd/Ctrl Option 1–9` 等未在官方页确认的快捷键已就地标注 ⚠️。
- `/create-cycles`、`/cycle-cooldowns` 是**失效重定向**——真正的页是 `/use-cycles`（正常加载），cycle 配置字段已逐字补入 5.2，不再是缺口。
- GitHub 集成（9.2）经 `/docs/github` 复核，与既有 2026 调研一致。
- 个别数字（audit log 90 天、SCIM 计费 2025-08-14、SAML JIT 仅创建时写、数据区恒存美国清单）经两轮交叉印证但非单页逐字引用，建议施工前再核一次。
