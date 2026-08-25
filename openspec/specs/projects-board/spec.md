# projects-board（项目与看板）

## Purpose

管理项目（列表、详情、项目内需求/交付的组织与看板视图）与跨项目的发现视图；项目是资源（Git 仓库）与编排绑定的容器。本域管项目的创建/配置/置顶、项目详情三页签的组织、项目任务看板（父子任务树）与需求发现视图；门禁与审批流转归 gates-approvals，指标口径与时序聚合归 statistics，编排引擎与绑定语义归 agent-orchestration。

## Requirement: 项目创建必须绑定可达的 Git 仓库

系统 SHALL 仅在提供非空项目名与非空仓库地址时创建项目（暂不支持绿地项目）；仓库地址 SHALL 限于 https、ssh、git@ 与本地绝对路径四种形态（白名单之外的协议在可达性校验之前直接拒绝）；创建时 SHALL 对仓库做可达性校验（ls-remote），不可达或无权限时拒绝创建，且客户端只收到固定文案——原始错误只进服务端日志。默认分支缺省为 main。（REST 面：api/projects.go；前端入口：features/projects/projects-list.tsx）

#### Scenario: 合法输入创建成功

- **WHEN** 提交非空项目名与 https 仓库地址，未填默认分支
- **THEN** 项目以默认分支 main 创建成功（201），随后出现在项目列表

#### Scenario: 缺名、缺仓库或协议非法被拒

- **WHEN** 项目名为空白、或未填仓库地址、或仓库地址是 http://、file:// 等白名单外形态
- **THEN** 创建被拒（400），分别提示「项目名不能为空」「必须绑定 Git 仓库（暂不支持绿地项目）」「仓库地址必须是 https、ssh 或本地绝对路径」

#### Scenario: 仓库不可达返回固定文案

- **WHEN** 仓库地址协议合法但 ls-remote 不可达或无权限
- **THEN** 拒绝创建（400）并返回固定文案「仓库不可达或无权限（地址与访问凭据是否正确？）」；含服务器本地路径等内部信息的原始错误只写服务端日志，不回传客户端

## Requirement: 项目列表按置顶与最近活动排序

项目列表 SHALL 一次带回各项目的活跃/待审批统计与最近活动时间（`include=stats`）；展示排序 SHALL 为置顶项目在前、其余按最近活动倒序。置顶状态 SHALL 服务端持久化，切换时前端先乐观更新本地缓存，失败回滚并提示。（REST 面：api/projects.go；统计行口径见 statistics 域）

#### Scenario: 置顶项目排最前

- **WHEN** 项目 A 未置顶但最近有活动、项目 B 已置顶且较久无活动
- **THEN** 列表中 B 排在 A 之前；其余未置顶项目之间按最近活动时间倒序

#### Scenario: 置顶持久化与乐观更新

- **WHEN** 点击某项目卡片右上角的置顶/取消置顶按钮
- **THEN** PATCH 请求把置顶态写入服务端，本地列表即时重排；请求失败时恢复原序并提示错误

#### Scenario: 卡片统计徽标

- **WHEN** 某项目有 2 个进行中任务、1 个待审批任务
- **THEN** 卡片分别显示「2 个进行中」「1 个待审批」徽标；两者皆无时显示「没有活跃任务」，活动时间取统计最近活动（无统计时回退项目更新时间）

## Requirement: 项目详情以三页签组织

项目详情 SHALL 以页内一级导航承载三个页签——「总览」（/projects/{id}）、「项目任务」（/projects/{id}/tasks）、「Agent 执行时序」（/projects/{id}/agent-activity）；当前页签 SHALL 以精确路由匹配标注（aria-current='page'），页签即标题，页头不再重复页面级标题。「Agent 执行时序」页签内数据 SHALL 保持工作区全局口径（跨项目统计），页头说明如实标注。（前端：features/projects/dashboard/project-tabs.tsx）

#### Scenario: 默认进入总览

- **WHEN** 访问 /projects/{id}
- **THEN** 「总览」为当前页签，页头显示项目名、仓库地址与默认分支

#### Scenario: 精确匹配高亮

- **WHEN** 位于 /projects/{id}/tasks 页
- **THEN** 仅「项目任务」标注为当前页签，「总览」链接不因前缀匹配而高亮

## Requirement: 项目总览承载 KPI、执行时序与必需配置

项目总览页 SHALL 分区呈现——项目统计（KPI 瓦片：任务总数/待决策/已交付/最近活动 + 五桶状态分布条；分布条分母取五桶计数之和以保证分段恒和为 100%；指标定义归 statistics 域）、Agent 执行时序（项目维度甘特泳道 + 分 stage 聚合表，契约见 statistics 域）、必需配置（Git 仓库与默认分支只读呈现，/ 开头的本地路径不入行；仓库地址空值显示「未绑定」，默认分支空值显示「—」——服务端建项缺省补 main，空分支仅在历史脏数据下可见）。页头 SHALL 提供新建需求与项目编排对话框入口；统计区与时序区 SHALL 各自具备加载失败提示与重试。编排对话框以项目级绑定为准（全量另存或清空，绑定语义归 agent-orchestration 域）。

#### Scenario: 分区呈现与配置只读

- **WHEN** 进入绑定 https 仓库的项目总览
- **THEN** 依次呈现项目统计、Agent 执行时序、必需配置三区；必需配置区只读显示仓库地址与默认分支

#### Scenario: 局部失败不阻塞整页

- **WHEN** stats 请求失败而 stage-runs 正常
- **THEN** 统计区显示「统计数据加载失败」与重试按钮，时序区照常渲染

#### Scenario: 编排对话框全量另存

- **WHEN** 在编排对话框为项目各节点指定 agent 后保存，或点「清空项目编排」
- **THEN** 以项目级绑定全量另存（PUT）或清空（PUT {}），成功提示「项目编排已保存 / 已清空项目编排」

## Requirement: 项目任务看板以父子任务树组织

项目任务页 SHALL 以唯一数据源 GET /api/projects/{id}/task-groups 呈现项目任务：顶层行 = 无父交付（按创建时间升序），每个顶层行内嵌其子任务按阶段（wave）分组——编号阶段升序、无阶段（wave 0，同步镜像无 stage 的子任务）垫底；父子行 SHALL 均带五态状态（active/queued/completed/blocked/cancelled）与标签，父任务带子任务完成进度（n/n）。页面只读，行点击进入任务详情。（REST 面：api/taskgroups.go；前端：features/projects/project-tasks.tsx）

#### Scenario: 父任务带子任务进度

- **WHEN** 某父任务有 3 个子任务、其中 2 个已完成
- **THEN** 左栏行与右栏卡片均显示「子任务 2/3」，右栏渲染对应进度条

#### Scenario: 子任务按阶段分组

- **WHEN** 某父任务的子任务分属阶段 1、阶段 2 与无阶段
- **THEN** 右栏以「阶段 1」「阶段 2」「无阶段」分组呈现，无阶段组排在编号阶段之后

#### Scenario: 选中联动与默认选中

- **WHEN** 进入页面尚未选择，或点击任一子任务行
- **THEN** 默认选中第一个父任务 / 选中该子任务所属父任务，右栏渲染其父子树，被选父任务行以 aria-current 高亮

#### Scenario: 子任务分组收起与展开

- **WHEN** 点击右栏选中卡片头部的收起/展开按钮
- **THEN** 子任务清单收起或展开，进度摘要（n/n）恒保持可见，收起不丢失选中态

## Requirement: 列表页数据源失败按空态呈现

项目列表（/）、项目任务（/projects/{id}/tasks）与需求发现（/discovery）三张列表页 SHALL 以「查询失败与空数据同形」为当前契约：数据源请求失败时页面呈现与空数据完全相同的空态文案（「还没有项目 / 还没有任务 / 还没有需求发现任务」），SHALL NOT 单独呈现错误提示或重试入口（区别于项目总览的分区级失败提示与重试）。该行为是已知取舍——列表页不区分「没有数据」与「取不到数据」。

#### Scenario: 查询失败呈现空态

- **WHEN** 列表数据源请求失败（如后端 5xx）
- **THEN** 页面渲染与空数据相同的空态文案与引导，不出现错误横幅或重试按钮

#### Scenario: 与总览失败提示的差别

- **WHEN** 同一项目下任务列表查询失败、总览 stats 查询也失败
- **THEN** 总览统计区显示「统计数据加载失败」与重试；任务页只显示「还没有任务」空态

## Requirement: 项目任务页锁定一屏滚动

项目任务页 SHALL 整页锁定一屏（头部 → 页签头 → 内容区，文档不滚动）；宽屏（lg 起）下滚动 SHALL 只发生在左右两栏内部——左侧列表再长也不推动页签头与右栏选中卡片；窄屏（<lg）SHALL 退化为上下堆叠、内容区整体滚动。（前端：features/projects/project-tasks.tsx）

#### Scenario: 长列表不推动右栏

- **WHEN** 左栏父任务列表远超一屏高度
- **THEN** 仅左栏内部滚动，页签头与右栏选中卡片保持可视不动

#### Scenario: 窄屏堆叠

- **WHEN** 视口宽度小于 lg 断点
- **THEN** 主/子任务树与详情栏上下堆叠，内容区整体滚动

## Requirement: 项目任务页不渲染需求挖掘域标签

项目任务页 SHALL NOT 渲染「情报」「候选」两个需求挖掘域分类标签（按标签名匹配，无开关/配置项）；该过滤 SHALL NOT 影响任务本体与计数；某行标签全被滤掉时标签行整行不渲染、不留空壳。需求发现视图与全局标签库不受此口径影响。

#### Scenario: 挖掘域标签不显示

- **WHEN** 某任务挂有「情报」「候选」「bug」三个标签
- **THEN** 项目任务页仅渲染「bug」chip，任务行内容与子任务计数不受影响

#### Scenario: 全部标签被滤掉

- **WHEN** 某任务只挂「候选」一个标签
- **THEN** 标签行不渲染，不占位不留空壳

## Requirement: 项目内创建需求走上游同步回流

项目详情页与项目任务页 SHALL 共享「新建需求」入口，提交 POST /api/projects/{id}/requirements：字段为标题（必填）、描述（可留空）、状态两档（backlog=待规划，创建后不触发执行 / todo=待办，指派即唤醒智能体；缺省 backlog）、优先级（none/urgent/high/medium/low 透传）、自动合并开关、智能体（缺省 Tech Lead，可选自定义 agent id 显式指派）与目标项目（缺省当前项目）。创建编排未装配时 SHALL 返回 503，项目不存在 404，项目未映射上游 409；前端错误提示 SHALL 用中性文案，不透传上游/同步细节。（REST 面：api/requirementcreate.go；前端：features/projects/requirement-create-dialog.tsx）

#### Scenario: 创建成功出新卡

- **WHEN** 填写标题提交成功（201）
- **THEN** 提示「需求已创建」，任务分组查询被失效，列表出现新任务卡

#### Scenario: 状态两档语义

- **WHEN** 状态分别选「待规划」与「待办」创建
- **THEN** 待规划卡创建后不触发执行；待办卡创建即唤醒智能体（对话框副标题如实提示）

#### Scenario: 失败中性提示

- **WHEN** 提交失败（编排未装配 503、项目未映射上游 409 或项目不存在 404）
- **THEN** 前端一律提示中性「创建失败，请稍后重试」，对话框保持打开可修改重试，上游细节不透传

## Requirement: 需求发现视图跨项目聚合两类 agent 任务

需求发现页（/discovery）SHALL 以 GET /api/discovery-tasks 为唯一数据源：agent 类型由标签判定——「情报」= 需求挖掘（mining）、「候选」= 需求分析（analysis）；agent 参数省略取并集，重复传参取非空已知值的并集（空值参数被静默跳过、不参与判定；同卡多标签命中只返回一次），未知取值 400。响应行内嵌交付全字段 + agent_types 全集（双标签卡可同时命中两类）+ 项目名 + 标签，按 updated_at 倒序，跨项目行以项目名打头；同步镜像任务卡无 current_stage 时以外部 issue key 顶替阶段位。卡片 SHALL 为到任务详情的下钻入口（点击进入 `/deliveries/{id}`）。类型/状态筛选与分组 SHALL 在客户端完成（行内已带全集，契约层不加 status 参数）；呈现为「候选 / 已放弃」左右双栏——cancelled 行进右栏「已放弃」、其余进左栏「候选」，筛选与分组先作用于全量再拆栏，窄屏退化为上下堆叠。（REST 面：api/discovery.go；前端：features/discovery/）

#### Scenario: 缺省合并取回

- **WHEN** 不带 agent 参数请求
- **THEN** 返回「情报」「候选」两类标签命中的并集，同时挂两类标签的卡只出现一次，且 agent_types 带两项全集

#### Scenario: 未知类型拒绝、空值跳过

- **WHEN** agent 参数传 mining|analysis 之外的取值，或传空值参数（如 `agent=mining&agent=`）
- **THEN** 前者返回 400「agent 只支持 mining|analysis」；后者静默跳过空值、按 mining 过滤返回（全部参数为空值时等同缺省取并集）

#### Scenario: 页内筛选分组与双栏拆分

- **WHEN** 用户在页内选择类型/状态筛选与分组方式，且结果中既有候选行也有 cancelled 行
- **THEN** 筛选与分组在客户端作用于全量后，非 cancelled 行进入「候选」栏、cancelled 行进入「已放弃」栏，栏头各带计数；双标签卡在任一类型筛选下保留，按类型分组时依全集出现在两组

#### Scenario: 卡片下钻任务详情

- **WHEN** 点击发现视图中的任一任务卡
- **THEN** 进入该交付的任务详情页（/deliveries/{id}）
