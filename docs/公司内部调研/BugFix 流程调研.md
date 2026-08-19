# BugFix 流程调研

# 1\. 三个项目的交集流程

本章从 Symphony、ClawSweeper 与 OpenHands Resolver 的实现中抽取共同主干，忽略各项目在触发方式、复现强制程度、GitHub 写入方式和自动合并策略上的差异。

**核心结论：**三者都采用“确定性外层工作流 \+ Agent 执行循环 \+ 验证与人工反馈边界”。确定性系统负责准入、环境、状态和权限，Agent 负责开放式分析、修改和命令执行。

## 1\.1 交集流程图

主链路表示三个项目都具备的流程阶段；复现、验证和交付的具体强制程度并不相同，差异见 1\.3。

## 1\.2 共同阶段说明

1. **任务输入：**接收 Issue、PR 评论、Tracker 工单、定时扫描或其他问题信号。

2. **准入与调度：**检查状态、标签、权限、账户、阻塞关系、并发额度或仓库策略，避免无权限执行和重复运行。

3. **隔离环境：**为单个任务创建 Workspace 或 Sandbox，检出固定代码版本，并通过 Setup、Hooks 或仓库脚本准备依赖。

4. **上下文组装：**把问题描述、评论、日志、代码、仓库规范、测试命令和执行 Prompt 交给 Agent。

5. **诊断与复现：**Agent 阅读代码、运行测试或脚本、启动服务、调用 API，必要时使用 Browser/E2E，尝试建立问题存在的证据。

6. **修复循环：**Agent 在“搜索—假设—修改—执行—观察”循环中完成代码修改，并根据结果继续迭代。

7. **验证门禁：**运行目标测试、回归测试、Lint、类型检查、Build、CI 或二次 Review；失败时进入有限重试或 Repair Loop。

8. **结果交付：**回写任务状态或说明，并按项目能力创建或更新 Branch、Commit 和 PR。

9. **反馈与终态：**成功后进入 Review、Merge 或关闭流程；无法复现、缺少权限、需要产品决策或验证失败时进入 Blocked、No\-op 或人工评审。

## 1\.3 共性能力与实现边界

|流程环节|三个项目的交集|主要差异|
|---|---|---|
|任务准入|执行前都有资格检查与去重机制|Symphony 依据 Tracker 状态；ClawSweeper 使用严格 Implementation Gate；OpenHands 检查触发方式、Write 权限和账户|
|环境隔离|都在任务级独立工作区中运行 Agent|环境创建、依赖安装和生命周期管理方式不同|
|问题复现|都允许 Agent 通过测试、命令和仓库工具调查问题|ClawSweeper 对 strict\_bug 强制先复现或建立失败测试；OpenHands 默认没有修改前失败的硬 Gate；Symphony 由 WORKFLOW\.md 定义|
|代码修复|都使用 Agent Action\-Observation Loop 完成分析、编辑和执行|模型、工具、最大轮次和恢复机制不同|
|质量验证|都把测试、检查或 Review 放在 Agent 完成声明之后|ClawSweeper 的确定性 Gate 最完整；OpenHands 依赖 Hooks、CI 与人工 Review；Symphony 依赖仓库执行契约|
|交付与反馈|都将执行结果回写外部系统，并允许失败或反馈进入下一轮|PR 创建、自动重试和合并策略不同；Symphony 不规定统一的 PR/合并实现|

# 2\. Human\-on\-\*：人在链路中的位置

本章比较的不是“项目是否需要人”，而是人在什么时间点成为流程的阻塞条件。除 Human\-in\-the\-loop、Human\-on\-the\-loop 和 Human\-on\-exception 外，文中使用的 Human\-on\-state、Human\-on\-trigger、Human\-on\-feedback 与 Human\-on\-approval 是为了描述这些项目而采用的分析标签，并非项目官方术语。

## 2\.1 Human\-on\-\* 模式定义

|模式|人在流程中的位置|自动化影响|
|---|---|---|
|Human\-in\-the\-loop|人的输入或批准是正常主链路中的必要步骤，系统必须等待人才能继续|控制力强，但人的等待时间直接限制吞吐量|
|Human\-on\-the\-loop|系统自动执行，人持续观察结果，并在关键边界批准、纠偏或终止|正常执行无需逐步确认，关键风险仍由人控制|
|Human\-on\-exception|正常路径完全自动化，仅在无法复现、缺少权限、需要决策或触发风险规则时找人|自动化程度最高，但依赖清晰的状态、证据和升级协议|

## 2\.2 三个项目的人类触点对比

|项目|任务启动|运行中介入|反馈后再执行|最终交付|主要模式|
|---|---|---|---|---|---|
|Symphony|Scheduler 可自动领取符合条件的 Tracker 工单，不要求逐个由人触发|进入 Blocked 时等待信息、授权或审批；进入 Human Review 时停止自动修改|人修改 Tracker 状态或补充信息后，由 Reconcile 恢复或终止|PR Review 与 Merge 由 WORKFLOW\.md 和仓库流程规定|Human\-on\-state \+ Human\-on\-exception|
|ClawSweeper|Issue 可经严格 Gate 自动排队，也允许 Maintainer 显式创建 Repair Job|自动完成计划、复现、修复和验证；无法复现、需要产品决策或触发安全边界时停止|自动执行有限的 Exact\-head Review、CI Repair Loop；Blocked 后由人补证据或作决策|Issue\-to\-PR 和 autofix 由维护者评审、合并；automerge 必须预先显式授权|Human\-on\-exception \+ Human\-on\-approval|
|OpenHands Resolver|每次任务必须由有 Write 权限的人添加 Label 或输入 @openhands|Conversation 启动后，Agent 自动分析、修改、测试、Commit、Push 并创建 Draft PR|Review 意见或 CI 失败后，需要人再次输入 @openhands 才创建新 Conversation|由维护者 Review、Merge、发布、监控和回滚|Human\-on\-trigger \+ Human\-on\-feedback|

## 2\.3 人出现的精确时间线

### 2\.3\.1 Symphony

**人或系统创建 Tracker 工单** → Scheduler 自动领取 → Agent 自动执行和重试 → 仅在 `Blocked` 或 `Human Review` 时等待人 → 人补信息、授权、评审或修改工单状态 → Reconcile 自动恢复、终止或清理。

### 2\.3\.2 ClawSweeper

**人提交 Issue、系统扫描或 Maintainer 下命令** → 自动 Review 和准入 → 自动复现、修复、验证和 Repair Loop → 无法自动处理时由人补证据或做产品/安全决策 → 自动创建或更新 PR → Issue\-to\-PR 最终由人 Review 和 Merge。

### 2\.3\.3 OpenHands Resolver

**人添加 Label 或输入 ****`@openhands`** → Agent 自动执行并创建 Draft PR → 人 Review → 如果要求修改，人再次输入 `@openhands` → 新 Conversation 修改原 PR → 人最终 Merge。

## 2\.4 对 BugFix Bot 自动化边界的启示

- **正常路径不应逐步等人：**从任务准入到验证完成的 Draft PR 可以无人值守执行。

- **人应在语义和风险边界出现：**无法复现、预期行为不明确、缺少权限、涉及安全或产品决策时再升级。

- **异常必须结构化：**升级给人时应同时提供已尝试步骤、失败证据、当前假设和需要回答的单一决策问题。

- **合并策略需要分级：**Issue 自动修复默认人工合并；只有低风险、证据完整并经仓库显式授权的任务才考虑自动合并。

# 附录 1\. Symphony（交响乐）

Symphony 的核心定位是调度与运行 Agent：从问题跟踪系统持续领取工单，在隔离工作区中启动 Codex，并根据工单状态协调继续执行、重试、人工评审与清理。具体如何复现和验证 Bug，需要由仓库的 WORKFLOW\.md、测试、E2E、日志和环境脚本共同定义。

## 附录 1\.1 流程总览

## 附录 1\.2 流程图名词说明

本节解释流程图中的核心名词，以及它们在 Symphony 规范中的职责边界。

### 附录 1\.2\.1 WORKFLOW\.md

**定义：**项目仓库内、随代码进行版本管理的执行契约。Symphony 启动时读取该文件，获得运行配置和发送给 Agent 的任务 Prompt。

**关键内容：**

- **文件格式：**Markdown 文件，由 YAML Front Matter 和 Markdown 正文组成。

- **YAML Front Matter：**位于文件开头的两个 `---` 之间，一级字段包括 `tracker`、`polling`、`workspace`、`hooks`、`agent` 和 `codex`。

- **Markdown 正文：**作为每个工单的 Prompt 模板，可以读取 `issue` 和 `attempt` 等变量。

**示例：**

```yaml
---
tracker:
  kind: linear
  provider:
    project_slug: "your-project"
polling:
  interval_ms: 30000
workspace:
  root: ~/code/symphony-workspaces
hooks:
  after_create: |
    git clone git@github.com:your-org/your-repo.git .
agent:
  max_concurrent_agents: 4
  max_turns: 20
codex:
  command: codex app-server
---

You are working on {{ issue.identifier }}.

Title: {{ issue.title }}
Description: {{ issue.description }}
```

### 附录 1\.2\.2 Tracker

**定义：**Symphony 与外部工单系统之间的适配层，用于读取并标准化不同平台的工单数据。

**关键内容：**

- **候选工单：**按状态、项目范围和标签获取待处理工单。

- **状态刷新：**在 Reconcile 阶段重新查询指定工单的最新状态。

- **终态查询：**查询已结束工单，用于停止遗留任务并清理 Workspace。

- **数据标准化：**将不同平台的数据转换为统一的 Issue 模型，包括 ID、标题、描述、状态、标签、优先级和阻塞关系。

- **配置入口：**`tracker.kind` 选择适配器，`tracker.provider` 保存平台特有的项目范围、API 地址和凭证引用。

### 附录 1\.2\.3 Poll、Candidate、Dispatchable 与 Claim

**定义：**Scheduler 从查询工单到正式启动 Agent 之前使用的一组调度概念。

**关键内容：**

- **Poll：**Scheduler 按 `polling.interval_ms` 周期查询 Tracker。

- **Candidate：**Tracker 本轮返回的候选工单，还不代表一定会启动 Agent。

- **Dispatchable：**候选工单满足平台规则，并通过状态、标签、阻塞关系和并发额度等检查。

- **Claim：**调度器在内存中预占工单，避免同一个工单被重复启动、重复重试或并发执行。

### 附录 1\.2\.4 Workspace 与 Hooks

**定义：**Workspace 是单个工单的独立执行目录；Hooks 是在 Workspace 生命周期节点执行的项目脚本。

**关键内容：**

- **Workspace：**Agent 的代码读取、修改、测试和命令执行都限制在该目录内；同一工单重试时可以复用原目录。

- `after_create`：仅在新建 Workspace 后执行。

- `before_run`：每次启动 Agent 前执行。

- `after_run`：每次 Agent 尝试结束后执行。

- `before_remove`：删除 Workspace 前执行。

### 附录 1\.2\.5 Issue Context 与 Workflow Prompt

**定义：**Issue Context 是标准化后的工单数据；Workflow Prompt 是将工单数据渲染到 WORKFLOW\.md 正文后生成的任务说明。

**关键内容：**

- **Issue Context：**包括标题、描述、状态、标签、优先级、阻塞项和工单链接等字段。

- **模板变量：**正文可读取 `issue` 和当前执行次数 `attempt`。

- **最终 Prompt：**渲染结果会发送给 Codex，作为本轮 Agent 执行的输入。

### 附录 1\.2\.6 Codex App Server 与 Agent Runner

**定义：**Codex App Server 是 Codex 的进程通信模式；Agent Runner 是 Symphony 中负责启动 Codex 并管理单次执行的组件。

**关键内容：**

- **进程启动：**Agent Runner 在对应 Workspace 内启动 Codex App Server。

- **任务发送：**通过标准输入输出发送初始化请求和 Workflow Prompt。

- **事件接收：**持续读取会话事件、运行状态、Token 用量、日志和验证证据。

- **结果返回：**将执行结果和会话信息返回给 Orchestrator。

### 附录 1\.2\.7 Reconcile 与结果状态

**定义：**Reconcile 用于将 Tracker 中的最新工单状态与 Symphony 内存中的运行、重试和 Claim 状态重新对齐。

**关键内容：**

- **Continuation：**一次 Codex Turn 正常结束，但工单仍处于 Active 状态，因此继续下一轮执行。

- **Stalled：**超过配置时间没有收到新的 Agent 事件，视为停滞并进入失败重试。

- **Blocked：**Agent 需要人工输入、授权或审批，暂时不能继续自动执行。

- **Human Review：**停止自动修改，等待人工评审。

- **Terminal：**工单进入结束状态后，停止 Agent 并按策略清理 Workspace。

## 附录 1\.3 官方资料

- [Symphony Service Specification](https://github.com/openai/symphony/blob/main/SPEC.md)

- [Symphony Elixir Reference Implementation](https://github.com/openai/symphony/blob/main/elixir/README.md)



# 附录 2\. ClawSweeper（龙虾清扫器）

ClawSweeper 是 OpenClaw 仓库体系中的保守型维护自动化：它持续审查 GitHub Issue、Pull Request 和部分提交，把模型判断固化为可审计报告，再由确定性代码决定是否评论、打标签、创建修复任务、修改分支、创建 PR 或执行受控合并。它不是“把 Issue 直接扔给一个 Agent”的单进程脚本，而是由 Review、Apply、Issue Intake、Repair Worker、Validation、GitHub Mutation 和 Durable State 多条流水线组成。

以下内容基于 `openclaw/clawsweeper` 的 `main` 分支提交 `c712241162fc4878c31460b9f1bbaf3fd3870736`（2026 年 7 月 23 日）。

## 附录 2\.1 流程总览

对 `openclaw/openclaw`，自动 Issue 实现不是宽松的“可做就做”：通用 `viable` 通道被禁用，只能进入单独开关控制的 `strict_bug` 或 `vision_fit` 通道。由 Issue 自动生成的 PR 永不自动合并；只有维护者显式把既有 PR 交给 `automerge`，并且所有审查、检查、可合并性和安全 Gate 都通过时，系统才会执行合并。

## 附录 2\.2 流程图名词说明

本节解释 ClawSweeper 自动化链路中的核心对象，以及模型判断和确定性系统之间的职责边界。

### 附录 2\.2\.1 Review Intake 与 Scheduler

**定义：**把 GitHub 中需要审查的 Issue、PR 或提交转化为 Review 任务的入口。

**关键内容：**

- **定时扫描：**`sweep.yml` 按计划扫描目标仓库中的开放 Issue 和 PR，并按容量切分 Review Shard。

- **精确事件：**目标仓库可通过 `repository_dispatch` 转发单个 Issue/PR 事件，避免等待下一轮全量扫描。

- **维护者命令：**评论中的 `@clawsweeper review`、`@clawsweeper implement issue`、`@clawsweeper autofix` 和 `@clawsweeper automerge` 会进入各自的命令路由。

- **聚类输入：**GitCrawl 负责把相关 Issue/PR 聚为 Cluster；ClawSweeper 消费发布后的 SQLite 快照，不在 Repair Intake 中临时爬取 GitHub。

### 附录 2\.2\.2 Read\-only Review

**定义：**由 Codex 对当前 `main`、Issue/PR 讨论、相关项、代码、文档、测试和历史进行只读分析，输出结构化判断。

**关键内容：**

- **严格只读：**Review Prompt 禁止修改文件、安装依赖、执行会生成产物的构建和测试、提交代码或直接操作 GitHub；检出目录必须保持字节级干净。

- **结构化结果：**输出包含是否保留/关闭、置信度、问题类别、复现状态、工作候选、风险、修复边界、可能文件和验证命令等字段。

- **不是执行 Agent：**Review 只提出决策和工作建议，不拥有写凭证，也不会在这一阶段创建修复分支或 PR。

### 附录 2\.2\.3 Review Report、Apply 与 Durable State

**定义：**Review Report 是每个被审查对象的持久化 Markdown 结论；Apply 是根据报告执行公开 GitHub 变更的确定性阶段。

**关键内容：**

- **报告位置：**报告、任务、结果和审计数据发布到 `openclaw/clawsweeper-state`，而不是混入 ClawSweeper 源码仓库。

- **标记评论：**每个 Issue/PR 只维护一条带隐藏 Marker 的 ClawSweeper 评论；后续状态通过原地编辑更新，避免重复刷屏。

- **二次校验：**Apply 在评论、标签或关闭前重新读取 GitHub 实时状态，检查 Snapshot Drift、标签、作者权限、关联 PR、仓库策略和目标是否仍然开放。

- **提案与写入分离：**Codex 生成提案，TypeScript Applicator 决定提案是否仍可安全执行。

### 附录 2\.2\.4 Issue Implementation Gate 与复现契约

**定义：**决定一个已审查 Issue 是否可以从“维护建议”升级为自动 Issue\-to\-PR 任务的准入层。

**关键内容：**

- **`strict_bug`****：**要求 Issue 是现有行为缺陷，`reproduction_status=reproduced`、复现置信度高、`work_candidate=queue_fix_pr`、工作置信度高，并且不需要新功能、新配置项或产品决策。

- **`vision_fit`****：**要求工作与仓库 `VISION.md` 明确一致、复杂度小、修复提示和验证命令完整，且无安全或产品决策阻塞。

- **`viable`****：**仅用于允许该宽松通道的外围公开仓库；`openclaw/openclaw` 和 `openclaw/clawhub` 明确禁用。

- **Review 阶段的“已复现”：**这是基于当前主干路径和已有证据形成的结构化审查结论；由于 Review Worker 本身禁止执行测试，它不等同于该 Worker 已在运行环境中重放了一次 Bug。

- **Repair 阶段的复现要求：**严格 Bug 任务进入实现 Worker 后，Prompt 明确要求先复现 Bug或先增加一个失败的回归测试；如果最新 `main` 无法复现，则报告阻塞并停止，不创建 PR。

### 附录 2\.2\.5 Durable Job、Deduplication 与 Intake

**定义：**Durable Job 是一次 Repair 工作的持久化任务描述，包含来源、目标、权限、策略、Prompt、验证和输出约束。

**关键内容：**

- **稳定身份：**Issue Implementation 使用确定性的 Job Path、Cluster ID 和分支名；重试复用同一个逻辑任务，而不是不断创建新任务。

- **多层去重：**Intake 会检查现有开放 PR、ClawSweeper 生成分支、Review Report 中的相关 PR、已有 Job、Intake Receipt 和同 Cluster 的实现。

- **实时保护：**正式入队前重新检查 Issue 是否开放、是否锁定、是否包含安全/保护/暂停标签，以及是否出现新的实现 PR。

- **派发：**通过 `repair-issue-implementation-intake.yml` 写入任务和审计记录，再以 `autonomous` 模式派发 `repair-cluster-worker.yml`。

### 附录 2\.2\.6 Plan Worker、Execute Worker 与 Codex Thread

**定义：**Repair Worker 分为规划和执行两个 GitHub Actions Job；Codex 负责理解和修改代码，工作流负责准备环境、传递 Artifact 和控制权限。

**关键内容：**

- **Plan Worker：**校验 Job，检出目标仓库，读取 Issue/PR 与 Review Context，通过 Codex App Server 形成结构化修复计划或 Fix Artifact。

- **Execute Worker：**在固定 Base SHA 上执行复现、编辑和验证；不会让 Codex 自行决定 GitHub 写操作。

- **持久线程：**Codex Thread Cache 可跨 GitHub Actions 尝试恢复；恢复失败时可以新建线程，但 Durable Job 和逻辑工作身份保持不变。

- **可选 Steering：**CrabFleet 提供心跳、阶段状态、浏览器终端和人工 Steering；浏览器不是执行宿主，即使无人连接，GitHub Actions 仍会继续运行。

### 附录 2\.2\.7 Validation、Exact\-head Review 与 Repair Loop

**定义：**代码修改完成后，由确定性验证器、再次 Review 和 GitHub Checks 共同构成闭环，而不是仅采信 Agent 的完成声明。

**关键内容：**

- **验证命令：**执行 Review Report、Fix Artifact 和仓库规则提供的命令；对 OpenClaw，`pnpm check:changed` 是默认 Changed\-surface Gate，任务也可以指定更严格的命令。

- **固定基线：**编辑和本地验证围绕固定的 Base SHA 进行；后续主干移动由确定性 Final Base Sync 处理，避免 Agent 在执行中随意改变基线。

- **失败归因：**验证失败时，系统可在固定 Base 上重跑，以区分修复引入的问题和主干原有失败。

- **Exact\-head Review：**每轮修改后重新审查 PR 的精确 Head SHA；若 Review 或 CI 仍有可执行问题，则继续有上限的修复、验证和重审循环。

- **完成条件：**不仅要求本地命令通过，还要求精确 Head Review 无可执行 Finding、必要 Checks 出现并转绿、GitHub Merge State 就绪。

### 附录 2\.2\.8 GitHub Mutation Boundary 与凭证隔离

**定义：**把模型的代码能力和 GitHub 写权限隔离开，所有远端变更均由确定性 Wrapper 执行。

**关键内容：**

- **短期凭证：**GitHub App 按规划、执行、状态仓库和工作流派发等用途签发不同权限的短期 Token，不使用个人 PAT 作为写入降级方案。

- **模型无写凭证：**Codex 子进程环境会清除 GitHub App 私钥、写 Token、CrabFleet Service Token 等敏感凭证。

- **确定性写入：**Commit、Push、创建 PR、评论、标签、关闭和合并由 TypeScript/GitHub Actions Wrapper 在重新检查实时状态后执行。

- **并发保护：**使用 Concurrency、Force\-with\-lease、Head SHA 检查和推送前等待窗口，避免覆盖贡献者刚刚推送的代码。

### 附录 2\.2\.9 PR 结果、合并策略与恢复

**定义：**不同来源的 Repair 任务具有不同终态；Workflow Success 不自动等于代码已经合并。

**关键内容：**

- **Issue\-to\-PR：**创建的 PR 会带有 `clawsweeper`、`clawsweeper:autogenerated` 和 `clawsweeper:autofix` 标签；通过修复闭环后仍保持开放，由维护者最终评审和合并。

- **`autofix`****：**修复和验证既有 PR，但不执行合并；干净后移除 Repair Loop 标签并交回维护者。

- **`automerge`****：**只有维护者显式授权，并且 Exact\-head Review、CI、可合并性、安全、暂停标签、Review Thread 和仓库策略全部通过后，才执行受控合并。

- **Blocked/No\-op：**已经修复、无法复现、需要产品决策、触发安全边界或验证无法通过时，可以在不创建 PR 的情况下结束，并把明确原因写入 Result Ledger 和状态评论。

- **恢复：**Runner 被替换、Source Head 漂移或 Thread 无法恢复时，系统通过稳定 Work Key、缓存、Requeue 和结果账本继续同一个逻辑任务。

## 附录 2\.3 官方资料

- [ClawSweeper Repository](https://github.com/openclaw/clawsweeper)

- [ClawSweeper README](https://github.com/openclaw/clawsweeper/blob/main/README.md)

- [Steerable Repair Automation](https://github.com/openclaw/clawsweeper/blob/main/docs/steerable-repair-automation.md)

- [Work Lane](https://github.com/openclaw/clawsweeper/blob/main/docs/work-lane.md)

- [Review Prompt](https://github.com/openclaw/clawsweeper/blob/main/prompts/review-item.md)

- [Issue Implementation Intake](https://github.com/openclaw/clawsweeper/blob/main/src/repair/issue-implementation-intake.ts)

- [Repair Cluster Worker Workflow](https://github.com/openclaw/clawsweeper/blob/main/.github/workflows/repair-cluster-worker.yml)

# 附录 3\. OpenHands Resolver

OpenHands Resolver 的核心定位不是一个独立的 BugFix 脚本，而是由 GitHub Integration、V1 Conversation、Sandbox、Agent Server 与 MCP 工具共同组成的 Agent 修复入口：它把 Issue、PR 评论或 Review Thread 转换为一个新的代码 Agent 会话，在隔离工作区中完成调查、修改、测试与 Git 交付。它的优势是平台、工具链和仓库定制能力完整；但“修改前必须复现失败、修改后必须留下通过证据”并不是平台默认强制状态机，需要通过项目 Skills、Hooks、测试脚本与 CI 补齐。

## 附录 3\.1 流程总览

## 附录 3\.2 流程图名词说明

本节解释当前 OpenHands Resolver 源码中的真实调用链、各组件职责，以及复现、测试和 PR 交付的能力边界。

### 附录 3\.2\.1 Resolver Integration 与 V1 Conversation

**定义：**Resolver Integration 是把 GitHub 事件转换为 OpenHands Agent 任务的编排层；V1 Conversation 是每次任务实际运行的会话实例。

**关键内容：**

- **组合式实现：**当前流程由 GitHub Webhook、GithubView、App Conversation Service、Sandbox、Agent Server、Git 服务与 MCP 工具组合完成，不存在一个独立的“Bug Reproducer \+ Fixer”状态机。

- **单次触发单次会话：**每次符合条件的 Label、Issue 评论、PR 评论或行级 Review 评论都会创建一个新的 V1 Conversation。

- **PR 迭代方式：**PR 上再次触发时会创建新会话并重新检出当前 PR Branch，而不是恢复最初的 Agent 会话；跨轮连续性主要来自 Git 分支、Issue/PR 上下文和仓库配置。

- **职责边界：**Integration 负责触发、上下文、凭证与结果回写；Agent 负责调查、工具选择、代码修改、测试和 Git 操作。

### 附录 3\.2\.2 Trigger、权限与账户 Gate

**定义：**Trigger Gate 决定一个 GitHub 事件是否有资格启动 Resolver，避免机器人回环、未授权调用和无凭证执行。

**关键内容：**

- **支持触发：**Issue 添加 `openhands` Label；Issue 或 PR 新评论中精确出现 `@openhands`；PR 行级 Review 新评论中出现 `@openhands`。

- **事件限制：**评论必须是 `created` 事件，编辑旧评论不会重新触发；机器人自身消息和系统生成的修复建议会被过滤，防止循环启动。

- **权限检查：**触发者必须对目标仓库拥有 Write 权限，并且需要存在可用的 OpenHands Cloud 账户、GitHub 身份和 Token。

- **凭证分工：**GitHub App Installation Token 用于 Reaction 与评论；用户 GitHub Token 用于 Clone、Push 和创建 PR。

- **接收反馈：**请求通过后先添加 `eyes` Reaction，再创建会话并回帖进度链接。

### 附录 3\.2\.3 GithubView、Issue/PR Context 与 Resolver Prompt

**定义：**GithubView 将不同 GitHub 事件标准化，并把 Issue、PR、评论和分支信息渲染成 Agent 的初始任务 Prompt。

**关键内容：**

- **Issue Context：**读取 Issue 标题、正文和默认最多 10 条历史评论；Label 触发时，任务目标是解决整个 Issue。

- **Issue 评论：**触发评论被放在 Prompt 的高优先位置，再附带 Issue 正文和历史评论，要求 Agent 优先处理本次明确指令。

- **PR 普通评论：**读取 PR 标题、正文、评论和当前 Head Branch，并将该分支作为 `selected_branch`。

- **行级 Review 评论：**携带文件路径、行号和对应 Review Thread，只围绕该线程构造修改任务。

- **测试契约：**Prompt 明确要求应用代码变更补充适当测试，并在结束前运行测试；纯文档或配置修改可以不新增测试。

- **交付契约：**Issue 修复 Prompt 要求创建 `openhands/` 前缀分支、Commit、Push、调用 `create_pr`，并按仓库 PR 模板使用 fixes/closes 关联 Issue。

### 附录 3\.2\.4 Sandbox、Workspace、Clone 与 Branch

**定义：**Sandbox 是 Agent 的隔离运行环境；Workspace 是当前 Conversation 的代码目录；Branch 决定本轮修改基于哪个 Git 状态执行。

**关键内容：**

- **隔离启动：**平台先分配或启动 Sandbox，并为 Conversation 建立独立 Working Directory。

- **仓库初始化：**默认使用 Shallow Clone 拉取目标仓库，减少启动时间和磁盘占用。

- **Issue 任务：**未指定目标分支时先在独立工作分支上准备 Workspace，最终交付分支仍由 Agent 按 Prompt 创建为 `openhands/` 前缀。

- **PR 任务：**普通 PR 评论和行级 Review 评论直接检出 PR Head Branch，后续 Push 会更新原 PR。

- **隔离边界：**Sandbox 提供文件、进程、网络和浏览器运行条件，但是否能够完整复现仍取决于依赖、密钥、外部服务和仓库初始化脚本。

### 附录 3\.2\.5 Repository Setup、Skills、Hooks 与仓库契约

**定义：**仓库契约是项目向通用 Agent 注入环境准备、调试方法、测试命令和质量门禁的定制层。

**关键内容：**

- **`.openhands/setup.sh`****：**Clone 后执行，用于安装依赖、生成配置、准备服务或完成项目特有初始化。

- **`.openhands/pre-commit.sh`****：**如果存在，会被安装为 Git Pre\-commit Hook，在 Agent 提交代码时执行项目检查。

- **项目 Skills：**`.agents/skills/`、`.openhands/microagents/` 和兼容的 Skills 目录可以描述复现步骤、日志位置、E2E 入口与测试策略。

- **`.openhands/hooks.json`****：**可配置 Tool、Session 和 Stop 等 Hook；Stop Hook 可以在 Agent 准备结束时执行检查并拒绝完成。

- **非默认保证：**这些机制只有在仓库实际配置且成功加载时才形成约束；OpenHands 不会替所有仓库自动推导确定性的 Bug 复现与验收流程。

### 附录 3\.2\.6 Agent Action\-Observation Loop 与工具

**定义：**Action\-Observation Loop 是 Agent 根据当前上下文选择工具、观察结果、更新判断并继续行动的循环。

**关键内容：**

- **默认执行工具：**`TerminalTool`、`FileEditorTool` 和 `TaskTrackerTool` 分别负责命令执行、文件编辑和任务拆解。

- **浏览器能力：**运行环境可用时会注入 `BrowserToolSet`，用于页面访问和 Web 场景验证；它不是通用桌面 Computer Use。

- **子 Agent：**启用 Sub\-agent 配置时可以加载 `TaskToolSet`，把独立调查或实现子任务委派给其他 Agent。

- **自主策略：**平台没有硬编码“人工复现、自动化测试、E2E、Browser”四选一规则，Agent 会根据 Issue、仓库 Skills、可用命令和观察结果决定下一步。

- **循环终止：**Agent 自己判断任务完成并请求 finish；Hook 可以阻断，平台级 Callback 只消费最终状态，不替 Agent执行调试决策。

### 附录 3\.2\.7 Reproduction、Evidence 与真实边界

**定义：**Reproduction 是在修改前观察到报告行为；Evidence 是支撑“问题存在、根因成立、修复有效”的命令输出、日志、测试或浏览器观测。

**关键内容：**

- **可用复现手段：**Agent 可以运行现有单测、最小脚本、启动服务、调用 API、检查日志，或在 Browser Tool 可用时操作 Web 页面。

- **选择机制：**复现路径由 Issue 信息、Prompt、项目 Skills、环境可用性和 Agent 推理共同决定，不是独立 Reproducer 服务预先生成。

- **没有前置硬 Gate：**Resolver Prompt 要求测试修复，但没有强制 Agent 在第一次编辑代码前必须获得一份失败结果。

- **没有双态证据协议：**平台默认不校验“Base 版本失败、Patch 版本通过”的成对证据，也不要求每个修复保存统一格式的复现产物。

- **证据回写：**命令与工具结果主要保存在 Conversation 事件中；只有 Agent 主动写入 PR 描述、测试或 Final Response 的内容，才会成为 GitHub 上长期可审阅的证据。

- **Computer Use 边界：**当前内置能力偏 Terminal、File Editor 和 Web Browser，不等同于可操作任意本地桌面应用的通用 Computer Use。

### 附录 3\.2\.8 Code Change、Tests 与 Quality Gate

**定义：**Quality Gate 将 Agent 的“我认为已完成”转换为仓库可执行、可重复的验收条件。

**关键内容：**

- **代码修改：**Agent 通过搜索、Terminal 和 File Editor 定位相关实现，修改应用代码并按 Prompt 补充测试。

- **测试执行：**Agent 可以运行单元测试、集成测试、E2E、Lint、类型检查和仓库自定义验证命令。

- **提交门禁：**`.openhands/pre-commit.sh` 可以阻止不满足检查的 Commit；Stop Hook 可以拒绝 Agent finish，并把失败原因返回会话继续修复。

- **平台边界：**如果仓库未配置 Hook，Resolver 主要依赖 Prompt 驱动 Agent 自觉运行测试；Callback 不会读取测试日志来判定任务是否真正通过。

- **最终门禁：**PR 创建后仍由 GitHub CI、代码扫描、Branch Protection 和人工 Review 提供确定性验收。

### 附录 3\.2\.9 Branch、Commit、Push 与 MCP create\_pr

**定义：**Git 交付阶段把 Sandbox 中的修改转换为可供维护者审阅的远端分支和 Pull Request。

**关键内容：**

- **Agent 主动交付：**Branch、Commit 和 Push 由 Agent 根据 Prompt 通过 Git 命令完成，不是 Conversation Callback 自动生成。

- **Issue\-to\-PR：**Agent 创建 `openhands/` 前缀分支，提交并推送后主动调用 MCP `create_pr`。

- **Draft 默认值：**当前 `create_pr` 工具默认以 Draft PR 创建，PR 正文会追加 Conversation Link，并保存 PR Number 到会话元数据。

- **PR Review 修复：**如果任务来自现有 PR 评论，Agent 在该 PR Branch 上 Commit 和 Push，直接更新原 PR，不再创建一个新的 PR。

- **PR 内容：**Prompt 要求遵循仓库 PR Template，并用 fixes/closes 语义关联原 Issue，使修复范围和关闭条件可审阅。

### 附录 3\.2\.10 Finished Callback、Review 与 CI Repair Loop

**定义：**Callback 负责把 Agent 终态映射回 GitHub；Review Loop 通过新的 GitHub 评论启动下一轮修复。

**关键内容：**

- **成功回写：**Callback 监听 `execution_status`，仅在状态为 `finished` 时获取 Agent Final Response，并写回原 Issue、PR 或行级 Review Thread。

- **完成语义：**`finished` 代表 Agent 执行结束，不等于 Callback 已验证测试、CI 或 PR 一定成功创建。

- **异常终态：**`error`、`stuck` 等状态会被记录，但不会沿用成功路径发布完成摘要。

- **Review 迭代：**维护者在 PR 普通评论或行级线程再次输入 `@openhands`，平台会创建新 Conversation、检出当前 PR Branch、处理反馈并 Push 更新。

- **CI 建议：**Proactive CI 能识别失败的 GitHub Actions 或 Merge Conflict，并在 PR 中建议维护者使用 `@OpenHands` 请求修复；当前逻辑不会仅凭 CI 失败自动启动 Agent。

- **合并边界：**OpenHands 负责提出和迭代代码变更，最终合并、发布、监控和回滚仍属于仓库原有研发流程。

## 附录 3\.3 官方资料

- [OpenHands Overview](https://docs.openhands.dev/overview/introduction)

- [OpenHands Cloud GitHub Integration](https://docs.openhands.dev/openhands/usage/cloud/github-installation)

- [OpenHands GitHub Action](https://docs.openhands.dev/openhands/usage/run-openhands/github-action)

- [Repository Customization](https://docs.openhands.dev/openhands/usage/customization/repository)

- [Hooks and Quality Gates](https://docs.openhands.dev/openhands/usage/customization/hooks)

- [Sandbox Overview](https://docs.openhands.dev/openhands/usage/sandboxes/overview)

- [Incident Triage](https://docs.openhands.dev/openhands/usage/use-cases/incident-triage)

