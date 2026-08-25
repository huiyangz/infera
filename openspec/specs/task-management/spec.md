# task-management（需求接入与任务卡）

## Purpose

管理外部需求的接入与在 infera 内的呈现：需求大节点状态机（`requirements.node`，单一状态源，由闸门轮询按上游父 issue 状态推进）、需求卡与交付卡（delivery，即项目内任务卡）的生命周期——创建入路、状态集、字段与展示规则、阶段产物与交付详情，以及需求 → 任务的拆分关系（父子、批次分层）在本域的呈现。本域不管：闸门卡的识别协议与门禁审批/终审合并策略细节（gates-approvals）、任务源同步通道与镜像机制（mcp-integration）、阶段图引擎执行与 agent 编排绑定（agent-orchestration）、项目/看板组织与统计聚合（projects-board / statistics）。（server：`reqservice`、`flow` 及 `store` 的 Delivery 形态；web：`features/requirements`、`features/deliveries`；文档：`docs/requirements-flow.md`）

## Requirement: 发起需求并派发到任务源

发起需求 SHALL 以标题为必填项（空白即拒绝，不建卡不落库），派发链路固定为：任务源固定项目建父 issue（`backlog` 起步、不触发 run）→ 指派 Tech Lead（置 `todo` 唤醒 agent）→ infera 落库且大节点为「已派发」（`dispatched`）。任务源侧失败时 SHALL NOT 在 infera 落库（宁可不派发，不留无 issue 的本地孤儿行）。（REST 面：`api/requirements.go`；服务：`reqservice.Create`）

#### Scenario: 标题非空提交成功派发

- **WHEN** 用户提交标题非空的需求（描述/验收标准/来源/优先级/验收人任意）
- **THEN** 上游建卡、指派 Tech Lead、infera 落库三步全部完成，返回需求行（含任务源深链），大节点为 `dispatched`

#### Scenario: 标题为空或空白被拒绝

- **WHEN** 提交的需求标题为空或全空白
- **THEN** 请求被 400 拒绝，不在任务源建卡、不在 infera 落库

#### Scenario: 上游失败不落库

- **WHEN** 上游建卡或指派 Tech Lead 失败
- **THEN** infera 不落需求行；指派失败时尽力把已建 issue 停回 `backlog`（suppressRun 防误唤醒），回收失败只并入错误链、不掩盖原始失败

## Requirement: 需求业务元数据只存 infera

需求描述、验收标准、来源、优先级、验收人等业务元数据 SHALL 只存 infera 侧需求行，SHALL NOT 下发任务源（上游只承载执行态）。（`flow.Requirement`；`reqservice.CreateInput`）

#### Scenario: 发起需求带元数据

- **WHEN** 发起需求时填写描述、验收标准、来源、优先级、验收人
- **THEN** 这些字段只落 infera 需求行，上游父 issue 不携带

#### Scenario: 读取需求返回元数据

- **WHEN** 读取需求详情或列表
- **THEN** 业务元数据随需求行完整返回（验收人为数组，空时为空数组非 null）

## Requirement: 大节点由父 issue 状态推进（单一状态源）

每个需求 SHALL 只有一个业务大节点，主链 `intake → dispatched → in_progress → in_review → delivered` 只进不退、允许跨级（一个轮询窗口内上游状态可能连跳）；节点 SHALL 由闸门轮询器按父 issue 状态映射推进：`todo→dispatched`、`in_progress→in_progress`、`in_review→in_review`、`done→delivered`（字面、大小写敏感）。`blocked`/`backlog`/`cancelled`/未知状态无映射，节点保持原值不推进；infera 是单一状态源——上游状态回退 SHALL NOT 导致节点回退，人工动作（含终审合并）SHALL NOT 直接推进节点（节点只随轮询推进）。（`server/internal/flow/node.go`）

#### Scenario: 上游状态连跳跨级推进

- **WHEN** 轮询见到父 issue 状态从 `todo` 跃至 `done`
- **THEN** 大节点跨级推进为 `delivered`（不因跳过中间态而卡住）

#### Scenario: 不可映射或非法前进保持原节点

- **WHEN** 父 issue 状态为 `blocked`/`backlog`/`cancelled`/未知词，或映射目标不构成合法前进（含上游侧状态回退）
- **THEN** 大节点保持原值不变

#### Scenario: 终审合并成功不推进大节点

- **WHEN** 用户在合并卡上执行合并且 GitHub 合并成功
- **THEN** 卡收口、审计落库，但大节点不变——由后续轮询按父 issue 状态推进

## Requirement: needs_decision 异常停驻与出口

需求 SHALL 仅从活跃节点（`dispatched`/`in_progress`/`in_review`）进入 `needs_decision`（由闸门协议检测触发，协议识别归 gates-approvals 域）；`intake`（无 issue 无闸门）SHALL NOT 进入该节点，`delivered` 是终态。停驻期间任务源状态推进挂起，需求 SHALL 只经用户决策动作离开 `needs_decision`：重试/跳过/自定义回复 → 回 `in_progress`；中止 → 直达 `delivered`；节点返回与收卡 SHALL 在同一事务内完成。（`flow.CanTransition`；`reqservice.Decide`）

#### Scenario: 停驻期间上游状态变化不推进

- **WHEN** 需求处于 `needs_decision` 且轮询见到父 issue 状态变为 `in_review`
- **THEN** 大节点保持 `needs_decision`（推进挂起，等用户决策）

#### Scenario: 决策「重试/跳过/自定义」回执行中

- **WHEN** 用户对决策卡选择重试、跳过或填写自定义回复
- **THEN** 代发评论成功后，同一事务内卡置 resolved、审计一行、大节点回 `in_progress`；与上游执行态的偏差由后续轮询按父 issue 状态一轮内校正

#### Scenario: 决策「中止」直达已交付

- **WHEN** 用户对决策卡选择中止
- **THEN** 代发固定文本「中止」后节点直达 `delivered`（需求退出在途）

## Requirement: 需求列表与详情读取

需求列表 SHALL 按创建时间新 → 旧返回全部需求，每行附待处理卡计数；需求详情 SHALL 返回需求行（含任务源深链 `external_issue_url` 与 PR 深链 `pr_url`）加**仅待处理**闸门卡（已处理卡不返回——历史走审计时间线），待处理卡按到达顺序（旧 → 新）排列。需求服务未装配（`TASK_SYNC_*` 配置全空 = 未接入）时需求 API SHALL 统一返回 503。（`reqservice.List/Get`；`api/requirements.go`）

#### Scenario: 列表附待处理卡计数

- **WHEN** 读取需求列表
- **THEN** 行按创建时间新 → 旧排列，每行带 `pending_card_count`（无待处理卡为 0）

#### Scenario: 详情只返回待处理卡

- **WHEN** 读取需求详情且该需求存在已处理与待处理卡
- **THEN** 响应只含待处理卡（按到达顺序），已处理卡不出现在详情，经审计时间线回溯

#### Scenario: 需求服务未装配返回 503

- **WHEN** 服务以 `TASK_SYNC_*` 全空（未接入）启动并收到需求 API 请求
- **THEN** 响应 503，不产生半途失败副作用

## Requirement: 大节点时间线与标签呈现

前端 SHALL 以主线 5 节点线性渲染大节点（受理 → 已派发 → 执行中 → 待验收 → 已交付）：当前节点高亮、已过节点 done、抵达终点即全 done；`needs_decision` 不在主线序列——出现时主线保持中性 upcoming 态并以异常横幅单独呈现；未知节点值 SHALL 回退显示原文，页面不崩。（web `features/requirements/node-timeline.tsx`、`types.ts`）

#### Scenario: 主线节点推进渲染

- **WHEN** 需求大节点为 `in_progress`
- **THEN** 时间线渲染「受理/已派发」为 done、「执行中」为 active、其余 upcoming

#### Scenario: 异常节点单独呈现

- **WHEN** 需求大节点为 `needs_decision`
- **THEN** 主线 5 节点全部保持中性 upcoming 态，另渲染异常横幅提示处理决策卡后恢复推进

#### Scenario: 未知节点值回退原文

- **WHEN** 接口返回词表外的节点值
- **THEN** 徽标/时间线显示该原文值，页面不崩溃

## Requirement: 代理动作经 infera 代发且审计只增

卡片代理动作（批准/驳回/决策/返工/合并）SHALL 只作用于「待处理且类型匹配」的卡：校验通过后先向任务源代发评论（或经 gh API 合并 PR），成功后才在单事务内收卡（resolved）+ 写审计（`actor=user`，决策动作另含节点返回）。代发失败 SHALL NOT 收卡、SHALL NOT 留审计（失败动作不算动作，卡保持待处理可重试）；卡已处理或动作与卡类型不匹配 SHALL 返回冲突（409）。审计表 SHALL 只增不改，删除带审计历史的需求 SHALL 被外键拒绝（不悄悄抹掉轨迹）。（`reqservice/actions.go`；`docs/requirements-flow.md` 运维注意）

#### Scenario: 成功动作单事务收口

- **WHEN** 用户对一张待处理的审批卡执行批准（或带反馈的驳回）
- **THEN** 代发评论成功后，同一事务内卡置 resolved、审计落一行 `actor=user`

#### Scenario: 代发失败卡保持待处理

- **WHEN** 代发评论时任务源故障
- **THEN** 卡保持 pending、无审计行，用户可重试同一动作

#### Scenario: 已处理或类型不匹配返回冲突

- **WHEN** 对已 resolved 的卡再次动作，或对审批卡执行合并类动作
- **THEN** 请求以冲突（409）拒绝，不产生代发与审计

## Requirement: 任务卡状态集与推进语义

任务卡（delivery）SHALL 使用固定状态集：`active`（引擎驱动中——服务重启会被重新点火驱动）、`queued`（已入列未被驱动）、`completed`（已交付终态）、`blocked`（流水线阻塞）、`cancelled`（上游「放弃」的独立终态，SHALL NOT 折叠为 completed——放弃 ≠ 交付，交付统计口径不计入）。任务同步镜像 SHALL NOT 产出 `active`（任何输入都不翻出 active——重启恢复会对全部 active 交付点火，镜像被点火等于替镜像跑引擎）：`done→completed`、`cancelled→cancelled`、`blocked→blocked`，其余（todo/backlog/in_progress/in_review/未知词）一律 `queued`。（`store.Delivery`；engine 状态常量；`syncsvc.translateStatus`）

#### Scenario: 镜像回流映射终态与阻塞

- **WHEN** 同步回流的上游 issue 状态为 `done`（或 `blocked`）
- **THEN** 对应任务卡状态落为 `completed`（或 `blocked`）

#### Scenario: 镜像永不翻出 active

- **WHEN** 同步回流的上游 issue 状态为 `in_progress`（或 todo/backlog/in_review/未知词）
- **THEN** 任务卡状态落为 `queued`，引擎不驱动该镜像

#### Scenario: 放弃是独立终态

- **WHEN** 上游 issue 状态为 `cancelled`
- **THEN** 任务卡状态原样落 `cancelled`，交付数口径不把它计入 delivered

## Requirement: 项目内新建任务卡

在项目下新建任务卡 SHALL 以标题必填、状态两档 `backlog`（缺省，不触发 agent run）/`todo`（指派即唤醒）受理，优先级按上游词表透传，智能体缺省 Tech Lead（可显式指定）；项目无上游映射（从未同步/纯本地建项）SHALL 返回冲突（409）。`auto_merge=true` 时 SHALL 先解析 workspace 的 `auto` 标签（解析不出则拒绝创建、不建半成品卡）再上游建卡打标；建卡后 SHALL 触发一轮同步回流并按外部 issue ID 读回同步后的任务卡作响应；回流 SHALL 是尽力而为——同步占用/失败不转为创建失败（防重试建出重复卡），读不回时退化为按上游回包拼装的行（无 infera 侧 id，锚点齐全），下一轮自动同步补齐。（`POST /api/projects/{id}/requirements`；`syncsvc.Creator`）

#### Scenario: 有映射项目建卡成功

- **WHEN** 在有上游映射的项目提交标题非空的任务卡（状态缺省）
- **THEN** 上游以 `backlog` 建卡并指派（缺省 Tech Lead），auto_merge 时打 `auto` 标，回流后返回同步落库形状的 Delivery（201）

#### Scenario: 输入或映射不合法被拒绝

- **WHEN** 标题为空、状态超出 backlog|todo 两档，或项目无上游映射
- **THEN** 分别以 400 / 400 / 409 拒绝，上游不建卡

#### Scenario: 回流尽力而为不阻断创建

- **WHEN** 建卡成功但同步回流被占用或失败
- **THEN** 创建仍按成功响应，返回按上游回包拼装的行（锚点齐全），下一轮自动同步补齐 infera 侧行

## Requirement: 拆分子任务与批次调度

设计审批门「批准并拆分」SHALL 将父任务置 `split_mode` 并直接停在 `code_gen`（跳过 tasks/tasks_approval/test_gen——父的实现就是子任务分支的合并），为每条规格创建一个子任务卡（`parent_id` 指向父、批次号 `wave` 1..N、状态 `queued`、各自走完整流水线且复杂度各自判定），wave 1 立即启动。批次调度 SHALL 保证同批次并行、最小 queued 批次先启动、更低批次全部 completed 且已合并后下一批次才启动；`wave` 0（任务同步镜像的无阶段子任务）SHALL NOT 参与批次调度。子任务完成 SHALL 增量合并进父工作区（完成一个合一个，不等齐）；全部子任务完成并合并后父走 unit_test → code_review。（`engine/split.go`）

#### Scenario: 批准并拆分落地父子结构

- **WHEN** 在 design_approval 门以含 wave 1、wave 2 规格的拆分方案批准
- **THEN** 父置 split_mode、当前阶段停在 `code_gen`；每条规格建一张 queued 子卡，wave 1 子卡转 `active` 启动

#### Scenario: 下一批次等前序批次收口

- **WHEN** wave 1 的全部子卡 `completed` 且均已合并进父
- **THEN** wave 2 的 queued 子卡才转 `active` 启动（此前保持 queued，重启恢复也不提前点火）

#### Scenario: 子任务完成即增量合并

- **WHEN** 某子任务跑完其流水线到达完成
- **THEN** 该子分支立即增量合并进父工作区，不等同批次或其它批次子任务

## Requirement: 拆分父合并冲突恢复

子分支与父分支冲突时，父 SHALL 置 `merge_state=conflict` 并暂停合并队列（其它子任务继续执行），并向用户给出人工解冲突的 git 指令；人工解决冲突并推送 `infera/<父id前8位>` 分支后，SHALL 提供恢复入口：fetch+reset 父工作区后重跑合并队列（可能又停在等剩余子任务），流水线继续。（`engine/split.go`；`POST /api/deliveries/{id}/merge/resume`）

#### Scenario: 冲突暂停队列不阻塞其它子任务

- **WHEN** 某子分支合并进父时发生冲突
- **THEN** 父 `merge_state=conflict`、合并队列暂停，任务详情展示冲突横幅（含 git 指引与「继续」入口）；其它子任务的执行不受阻

#### Scenario: 人工解冲突后恢复

- **WHEN** 人工在本机解决冲突、推送 `infera/<父id前8位>` 分支并点「继续」
- **THEN** 服务端 fetch+reset 父工作区并重跑合并队列，流水线继续推进

## Requirement: 任务卡详情与阶段呈现

任务卡详情 SHALL 返回任务行 + 时间线事件 + 阶段产物（拆分父另附子任务清单）。阶段条 SHALL 按复杂度派生展示：`small` 及老数据（complexity 空）走 7 阶段，`large` 走全 11 阶段（拆分父的 tasks/tasks_approval/test_gen 显示跳过态）；终态渲染规则：`completed` 全部 done、`blocked` 当前阶段 failed（之前 done、之后 pending、无进行中指示）、`cancelled` 停在放弃点不再推进。`large` 模式的逐任务实现进度 SHALL 由 tasks/task_done 产物推导（无清单产物时回退 task_done 事件拼装，进度持久不丢）；未知阶段/事件词表外的值 SHALL 回退原文显示不崩；镜像任务卡无 current_stage 时 SHALL 以占位符或 issue key 顶替阶段位，不留悬空「阶段」标签。（`GET /api/deliveries/{id}`；web `features/deliveries/delivery-detail.tsx`、`lib/infera-types.ts`）

#### Scenario: large 拆分父的阶段条与子任务清单

- **WHEN** 读取一张 `complexity=large` 的拆分父任务详情
- **THEN** 阶段条展示全 11 阶段且 tasks/tasks_approval/test_gen 为跳过态，`code_gen` 位显示等待子任务进度（已完成/总数），另附子任务清单（各带批次徽标、阶段、状态、标签）

#### Scenario: blocked 卡当前阶段显示失败

- **WHEN** 任务状态为 `blocked`
- **THEN** 阶段条上当前阶段为 failed、之前阶段 done、之后 pending，无进行中指示

#### Scenario: cancelled 卡停在放弃点

- **WHEN** 任务状态为 `cancelled`
- **THEN** 阶段条停在放弃点：之前阶段 done、当前及之后 pending，不再呈现任何推进或等待

## Requirement: 项目任务分组视图（父子分层呈现）

项目任务分组 SHALL 以「顶层行 = `parent_id` 为空的任务卡（创建时间升序），子任务嵌于父行 stages 分组」呈现，子任务 SHALL NOT 重复出现在顶层。子任务 SHALL 按批次 wave 归组：编号阶段升序、无阶段（wave 0）分组垫底、桶内按创建时间升序；每个任务行 SHALL 带状态/当前阶段/待处理门禁/外部 issue 键/负责人/优先级/标签（未挂标签为空数组非 null），父行另带 `child_total`/`child_completed` 摘要；无子任务的顶层行 stages SHALL 为空数组。（`GET /api/projects/{id}/task-groups`；`api/taskgroups.go`）

#### Scenario: 父子按批次分组

- **WHEN** 项目含一张带 wave 1、wave 2 子任务的父任务卡
- **THEN** 顶层只出父行（含 child_total=2），子任务按 stage 1、stage 2 两组升序嵌于父行 stages 内

#### Scenario: 无阶段子任务垫底

- **WHEN** 父任务带一张 wave 0（任务同步镜像）子任务与编号批次子任务
- **THEN** wave 0 分组排在全部编号阶段之后（不混进编号序列头部），且不参与批次调度

#### Scenario: 普通任务行无子分组

- **WHEN** 项目含一张从未拆分的顶层任务卡
- **THEN** 该行 stages 为空数组（非 null），child_total 为 0

## Requirement: 需求发现视图中的任务卡呈现

需求发现视图 SHALL 以标签驱动识别两类 agent 任务（`mining`=「情报」、`analysis`=「候选」；行内带 `agent_types` 全集而非仅命中项），行按 `updated_at` 降序、跨项目以项目名打头；cancelled 行 SHALL 独立归入「已放弃」栏——筛选与分组先作用于全量再按 cancelled 拆成「候选 / 已放弃」双栏；同步镜像无 current_stage 时以 issue key 顶替阶段位。（`GET /api/discovery-tasks`；web `features/discovery`）

#### Scenario: 双标签卡在任一类型筛选下保留

- **WHEN** 一张任务卡同时挂「情报」与「候选」标签，用户按 `mining` 筛选
- **THEN** 该行保留，且 `agent_types` 为全集 [mining, analysis]（分组/筛选用全集而非仅命中项）

#### Scenario: cancelled 拆入已放弃栏

- **WHEN** 视图内同时存在非 cancelled 与 cancelled 任务卡
- **THEN** 非 cancelled 行归左栏「候选」、cancelled 行归右栏「已放弃」，行序与分组结构不变
