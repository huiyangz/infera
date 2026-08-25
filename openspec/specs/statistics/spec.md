# statistics（统计）

## Purpose

只读分析面：项目维度需求统计（总数/按状态分布/分 stage 聚合）、跨项目 agent 执行时序聚合与工作区统计页，作为项目总览 KPI、Agent 执行时序 tab 与「统计」页的数据源。本域只定义指标口径、数据来源与展示规则，聚合纯只读（无写路径）；项目总览等页面的组织结构归 projects-board，stage_run 的产生与阶段流转归 agent-orchestration。

## Requirement: 项目需求统计冻结五状态口径

GET /api/projects/{id}/stats SHALL 返回冻结形状的统计载荷：任务总数、by_status 恒含 active/queued/completed/blocked/cancelled 五个固定键（无行为 0）、待决策数（pending_gate 非空且未完结）、已交付数与最近同步时间。已交付 SHALL 与 by_status.completed 同源（cancelled 不计入——放弃 ≠ 交付）；最近同步时间取项目的外部同步时间，从未同步为 null。项目不存在 SHALL 404；项目存在但无交付 SHALL 返回全零统计而非 404。（REST 面：api/projects.go；存储口径：store.RequirementStats）

#### Scenario: 五键恒在、空项目全零

- **WHEN** 项目只有 active 与 queued 两种状态的交付，或项目存在但没有任何交付
- **THEN** by_status 均返回五个固定键（无行为的键为 0，空项目全零统计），不缺键、不返回 404

#### Scenario: 放弃不计交付

- **WHEN** 项目有 5 个 completed、2 个 cancelled 交付
- **THEN** delivered = 5（与 by_status.completed 同源），cancelled 只体现在 by_status.cancelled = 2

#### Scenario: 项目不存在

- **WHEN** 查询不存在的项目 id
- **THEN** 返回 404「项目不存在」，而非全零统计

## Requirement: 项目执行时序限窗明细与分阶段聚合

GET /api/projects/{id}/stage-runs SHALL 返回项目内各交付的 stage_run 时序明细与分 stage 聚合，两者同一窗口：明细按 started_at 倒序、只保留最近 200 条；聚合行含 total/done/failed/running 计数与平均耗时、P95 耗时，行按 stage 字典序升序返回（前端按流水线阶段序重排）。耗时 SHALL 只统计已收尾（finished_at 非空）的运行，P95 取最近邻位法，无已收尾运行时为 0；agent 名 SHALL 经项目编排绑定（node=stage）关联，未绑定为 null。项目不存在 SHALL 404。（REST 面：api/projects.go；存储口径：store.ProjectStageRuns）

#### Scenario: 明细限最近 200 条

- **WHEN** 项目窗口内有超过 200 条 stage_run
- **THEN** runs 只保留最近 200 条（最旧被截掉），by_stage 聚合计同一窗口的全部被保留行

#### Scenario: 耗时只算已收尾

- **WHEN** 某阶段有 3 条 done、1 条 running
- **THEN** 该阶段 total=4、running=1，avg_ms 与 p95_ms 只由 3 条已收尾运行计算

#### Scenario: 未绑定 agent 为 null

- **WHEN** 某条运行的 stage 没有编排绑定
- **THEN** 该行 agent_name 为 null，前端以门禁/系统节点样式（描边空心条）呈现

## Requirement: 跨项目 agent 执行时序按桶聚合

GET /api/agent-activity SHALL 在 [now-hours, now) 窗口内按 bucket_minutes 分桶统计各 agent 的执行次数：hours 缺省 24（1..168）、bucket_minutes 缺省 30（合法值 5/10/15/30/60），非法参数 400。计数口径 SHALL 为 started_at 落桶的 stage_runs——attempt 各计一次、不分状态。每条曲线的 points SHALL 覆盖窗口内全部桶（含 count=0，各曲线等长对齐，前端免补零）；series 按 agent 名升序，窗口内零执行的 agent 不出现。agent 归属 SHALL 按 stage_run → 交付 → 项目 → 编排绑定（node=stage）解析：项目绑定优先、project_id 为空的全局绑定兜底、无绑定归「unbound」分组（agent_id 空串）。该接口为「Agent 执行时序」页签（工作区全局口径，不按项目过滤）的唯一数据源。（REST 面：api/agentactivity.go）

#### Scenario: 参数校验与缺省

- **WHEN** hours=200 或 bucket_minutes=45，或不带任何参数请求
- **THEN** 非法取值返回 400（提示 hours 需为 1..168 / bucket_minutes 需为 5/10/15/30/60 之一）；缺省请求按 hours=24、bucket_minutes=30 返回

#### Scenario: 曲线补零等长

- **WHEN** 某 agent 在窗口中段若干桶内没有执行
- **THEN** 其曲线中段桶 count=0，与窗口内其他曲线等长对齐；窗口内全程零执行的 agent 不出现在 series 中

#### Scenario: 无绑定归 unbound

- **WHEN** 某次 stage_run 的 stage 既无所属项目的绑定、也无全局绑定
- **THEN** 该次执行计入 agent_name 为「unbound」的曲线（agent_id 空串），不并入任何已注册 agent

## Requirement: 工作区统计区分快照与窗口口径

GET /api/stats SHALL 返回跨项目统计聚合（hours 缺省 168、1..720；tz 缺省 UTC、取 IANA 时区名；非法参数 400）：任务状态分布为全量快照，不受窗口影响，五类归并对齐 Multica 工作台口径——Done←completed、InProgress←active、Todo←queued+blocked、Cancelled←cancelled，Total 为全部任务（含未知状态），ByStatus 保留 infera 原始五键计数；执行统计与逐小时分桶只统计 [from,to) 半开窗口内的 stage_runs——执行次数含进行中（running 计次不计时长），累计时长只累计已收尾运行的 finished−started；逐小时分桶（0..23）按查询时区的本地小时归桶，跨小时收尾的执行整段计入起始小时桶、不拆分。该接口为「统计」页唯一数据源。（REST 面：api/workspacestats.go；存储口径：store.WorkspaceStats）

#### Scenario: 状态快照不受窗口影响

- **WHEN** 分别以 24 小时与 30 天窗口查询
- **THEN** task_status 各计数完全一致（全量快照），仅 execution 与 hourly 随窗口变化

#### Scenario: Todo 口径合并

- **WHEN** 工作区有 3 个 queued、2 个 blocked 任务
- **THEN** todo = 5（queued+blocked 归并），ByStatus 仍分别给出 queued=3、blocked=2 的原始计数

#### Scenario: 时区归桶与跨小时不拆分

- **WHEN** tz=Asia/Shanghai，某执行 UTC 16:30 启动并于两小时后收尾
- **THEN** 该执行计入本地小时 0 的桶（次日 00:30 启动），其整段时长全部累计进该起始桶，不向后续小时拆分

## Requirement: 统计页呈现快照卡片与时段分布直方图

「统计」页（/stats）SHALL 全部消费 GET /api/stats（不另开入口）：数字卡片呈现任务总数/已完成/进行中/待办/已取消（快照口径）与执行次数（附注进行中·失败）、累计时长（仅计已收尾）；窗口切换 SHALL 提供 24 小时 / 7 天（缺省）/ 30 天三档，只影响执行维度；时段分布 SHALL 按浏览器时区归桶（IANA 名解析失败回退 UTC），夜间 22:00–06:00 的柱体高亮，并在图下如实标注口径说明；加载失败可重试，窗口内无执行记录显示空态。（前端：features/stats/）

#### Scenario: 窗口切换只影响执行维度

- **WHEN** 从「7 天」切到「24 小时」
- **THEN** 执行次数与时段分布直方图更新，任务状态五张卡片数值不变

#### Scenario: 夜间时段高亮

- **WHEN** 直方图同时含 23:00 与 14:00 两个小时的桶
- **THEN** 23:00 桶按夜间样式高亮，14:00 桶为常规样式，图下标注夜间时段口径

#### Scenario: 空窗口空态

- **WHEN** 所选窗口内没有任何执行记录
- **THEN** 显示「窗口内没有执行记录」空态与引导文案，而非空图表

## Requirement: 统计面只读且契约冻结

统计域各响应形状 SHALL 视为前端的冻结契约（项目需求统计、项目执行时序、跨项目 agent 执行时序、工作区统计四个入口各自冻结），SHALL NOT 静默变更形状或另开并行统计入口；聚合 SHALL 纯只读——无写路径、无 schema 变更，窗口右端恒取当前时刻。

#### Scenario: 不另开并行入口

- **WHEN** 前端页面需要统计数据
- **THEN** 只消费四个冻结入口之一；新增统计维度须扩展现有载荷或另立提案评审，不新增并行的统计查询端点

#### Scenario: 只读聚合

- **WHEN** 任意统计查询执行
- **THEN** 聚合在读取面完成，不产生数据写入，不改变数据库 schema，窗口右端取当前时刻
