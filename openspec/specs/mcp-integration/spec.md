# mcp-integration（MCP 接口与同步集成）

## Purpose

管理外部集成通道：MCP 驾驶面（任何 MCP 客户端查上下文、交回本机绑定阶段产出、裁定人工门禁）、任务源同步（项目/issue 全量镜像、标签镜像、需求创建编排的上游建卡链路）、GitHub / Git 集成（仓库检出、交付分支、PR 代理）与本机 helper 拉起通道。本域只管「infera 与外部系统之间的通道契约」——通道两端的业务语义归各自域：门禁裁定与审批卡语义归 gates-approvals，阶段推进 / workdir 生命周期 / 产物固化时序归 agent-orchestration，镜像落库后的需求卡呈现归 task-management，同步状态的页面呈现位置归 web-routing。

## Requirement: MCP 端点开关与鉴权

MCP 服务 SHALL 以专用静态 token（`INFERA_MCP_TOKEN`，`Authorization: Bearer`）作为唯一接入凭证，与浏览器登录会话（密码 + cookie）解耦、可独立轮换；token 比较 SHALL 采用常数时间比较。未设置 token 时整个 `/mcp` 端点 SHALL 处于禁用态（`server/internal/mcp/server.go`）。

#### Scenario: 未设置 token 时端点禁用

- **WHEN** 服务端未配置 `INFERA_MCP_TOKEN`，客户端请求 `/mcp`
- **THEN** 返回 HTTP 503 与「MCP 未启用」错误，请求不进入协议处理——不使用 MCP 的部署不暴露该攻击面

#### Scenario: token 缺失或不匹配被拒

- **WHEN** 请求未带 `Authorization: Bearer` 头，或 token 与服务端不一致
- **THEN** 返回 HTTP 401，响应携带 `WWW-Authenticate: Bearer`

#### Scenario: 非本机 Origin 被拒

- **WHEN** 请求携带 `Origin` 头且其主机不是 localhost / 127.0.0.1 / ::1
- **THEN** 返回 HTTP 403（防 DNS rebinding）；不带 Origin 的 CLI 客户端不受影响

## Requirement: 无状态 MCP 协议面

`/mcp` SHALL 以无状态 Streamable HTTP 形态服务：与 HTTP API 同进程同端口、单 POST 端点、JSON-RPC 2.0、`application/json` 响应，不分配会话（客户端无须回传 `Mcp-Session-Id`）、不提供 GET SSE 流；协议面 SHALL 限定在 initialize / ping / tools 三类 method，不支持批量请求；请求体 SHALL 以 8 MiB 为读取上限（超限部分被截断，畸形后按解析错误拒绝）。JSON 合法性只在语法层校验——`jsonrpc` 成员缺失或版本号不符 SHALL NOT 单独被拒，按正常请求分派处理。

#### Scenario: 握手与版本协商

- **WHEN** 客户端 `initialize` 携带支持列表内的协议版本（2024-11-05 / 2025-03-26 / 2025-06-18）
- **THEN** 回显该版本，返回 tools capability、serverInfo（infera）与驾驶指引 instructions
- **AND** 携带不认识的版本时，返回支持列表内最新的版本

#### Scenario: 非法请求与未知 method

- **WHEN** 请求体不是合法 JSON（含数组形态的批量请求），或 method 不在 initialize / ping / tools/list / tools/call 之内
- **THEN** 分别返回 JSON-RPC 错误 -32700（解析错误）与 -32601（未知 method）；`jsonrpc` 成员缺失 / 版本不符不在此列，按正常请求继续分派

#### Scenario: 通知静默接受，非 POST 方法拒绝

- **WHEN** 请求无 `id`（notification，含未知通知）
- **THEN** 返回 202 且无响应体
- **WHEN** 请求方法不是 POST（如 GET SSE 流、DELETE 会话终止）
- **THEN** 返回 405 并声明 `Allow: POST`

## Requirement: MCP 驾驶工具清单与错误语义

MCP 面 SHALL 恰好暴露五个驾驶工具：`get_context`、`get_gate`、`approve_gate`、`reject_gate`、`submit_stage_output`；`tools/list` 返回的名称、描述与输入 schema SHALL 是工具面的单一事实来源。协议结构层错误（参数缺必填 / 类型不符 / 未知工具名）SHALL 回 JSON-RPC -32602；业务层失败（参数合法但执行不成，如交付不存在、`output` 为空文本）SHALL 以 `isError=true` 的文本结果返回可读原因，两者不得混用——`output` 在 schema 中为必填项，其「有值但为空」属业务失败而非结构错误。

#### Scenario: 工具列举

- **WHEN** 客户端调用 `tools/list`
- **THEN** 返回五个工具的名称、描述与输入 schema，且不含其他工具

#### Scenario: 参数错误与执行错误分层

- **WHEN** 调用工具时缺必填参数（如 `delivery_id`）或参数类型不符
- **THEN** 返回 JSON-RPC -32602
- **WHEN** 工具名不在五工具之列
- **THEN** 同样返回 -32602
- **WHEN** 参数合法但执行失败（如交付不存在、output 为空）
- **THEN** 返回 `isError=true` 的文本结果，文本为客户端可读的原因

## Requirement: 驾驶上下文与门禁详情读取

`get_context` SHALL 返回交付的全量驾驶上下文：需求（标题/描述/状态/阶段/门禁/复杂度等）、项目仓库信息（repo / 默认分支）、workdir 路径与约定、每类产物最新一份全文（超长截断）、以及停在本机绑定节点时的 `pending_local`（节点角色 + 与引擎发给 agent 同源的角色 prompt）。`get_gate` SHALL 返回当前挂起门禁的裁决材料：待审产物全文、PR 地址、code_review 门的真 diff，以及门禁专属的 AI 建议。

#### Scenario: 停车上下文含本机角色 prompt

- **WHEN** 交付停在本机绑定节点时调用 `get_context`
- **THEN** 返回需求、当前阶段与门禁、每类最新一份产物、仓库与 workdir 约定（clone 自默认分支、各阶段共享、交付分支由引擎创建），且 `pending_local` 带节点名与同源角色 prompt
- **AND** 交付不在本机绑定时 `pending_local` 为空

#### Scenario: 门禁详情带门禁专属裁决材料

- **WHEN** 交付分别挂起 spec_approval / design_approval / tasks_approval / code_review 门禁时调用 `get_gate`
- **THEN** 返回对应待审产物全文与 PR 地址，并分别附 AI 复杂度建议 / 拆分建议 / 可覆盖任务清单 / 真 diff（code_review 门的 diff 由固化阶段在挂门前落盘，随门禁详情全文给出）

#### Scenario: 读取失败与超长内容截断

- **WHEN** `delivery_id` 畸形或不存在、或交付当前无挂起门禁时调用读取工具
- **THEN** 返回可读的执行错误（交付不存在 / 当前无挂起门禁）
- **AND** `get_context` 中单产物内容超过上限时截断标注（普通产物 8000 字符、test_output 2000、门禁 diff 8000），不静默丢弃也不无限返回

## Requirement: MCP 写操作走引擎单入口

`approve_gate` / `reject_gate` / `submit_stage_output` SHALL 只经引擎单入口（Approve / Reject / SubmitLocal）修改交付状态，无旁路状态修改；裁定与交回选项 SHALL 按当前门校验。簿记 SHALL 持与 HTTP API 共享的 per-delivery 锁，成功后 SHALL 把推进交给后台驱动而不是阻塞响应（跨驾驶面互斥的完整语义归 gates-approvals）。簿记与推进 SHALL 在与请求脱钩的有界上下文（10s）中执行——客户端在写操作中途断连 SHALL NOT 杀死已受理动作的持久化（读路径仍走请求上下文）。

#### Scenario: 批准后自动推进

- **WHEN** `approve_gate` 在 spec_approval 门禁不带 complexity 选项
- **THEN** 取 AI 建议作为裁定，流水线自动推进到下一个停车点，响应返回推进后的状态快照（status / current_stage / pending_gate）
- **AND** design_approval 带 split、tasks_approval 带 tasks 时分别表示「批准并拆分」「批准并覆盖任务清单」，选项与门不匹配时被拒绝

#### Scenario: 打回回退并注入反馈

- **WHEN** `reject_gate` 携带 reason
- **THEN** 流水线回退到该门禁的重跑阶段（spec/design/tasks 门回退到对应文档阶段，code_review 门回退到 code_gen），reason 注入重跑 prompt

#### Scenario: 非本机停车交回被拒

- **WHEN** 交付未停在本机绑定节点、也不在门禁前置审查的本机形态时调用 `submit_stage_output`
- **THEN** 返回执行错误且不改变任何状态（本机交回的合法形态与产物契约归 agent-orchestration）

## Requirement: 任务源全量镜像

一轮同步 SHALL 把任务源工作区的全部项目与 issue 镜像进 infera：项目按外部项目 id、交付按外部 issue id（uuid，唯一锚点，不做标题匹配）幂等 upsert，重复执行 SHALL NOT 产生重复行；外部 issue key（如 INFERA-221）仅作展示，不参与匹配。

#### Scenario: 幂等导入

- **WHEN** 同一任务源连续执行多轮同步
- **THEN** 项目与交付的行数不随轮次增长，标题、描述、优先级、负责人以最近一轮上游值为准

#### Scenario: 跳过规则不中断本轮

- **WHEN** issue 标题含 `[infera-e2e]`、未挂项目、或父子关系成环排不出导入顺序
- **THEN** 分别以 `smoke` / `no_project` / `parent_cycle` 计入 skips 计数与清单，不落库也不挂标签，本轮其余数据照常导入

#### Scenario: 父先子后与批次透传

- **WHEN** 导入带父子关系的 issue
- **THEN** 按父先子后的顺序落库并建立父子关系，子任务的 wave 取上游 stage 原值（无阶段子任务与顶层为 0，引擎批次调度跳过 wave<=0）
- **AND** 父不在本轮导入集时，子单折叠为顶层（无父、wave=0）而不是丢失

## Requirement: 镜像状态翻译不出 active

同步 SHALL NOT 把任何上游状态翻译为 `active`——active 意味着引擎正在驱动（重启恢复会对全部 active 交付点火），镜像被点火等于替镜像跑管线；非终态一律落 `queued`（镜像只排队、不被引擎点火）。

#### Scenario: 非终态一律排队

- **WHEN** 上游 issue 状态为 todo / backlog / in_progress / in_review 或任何未知词
- **THEN** 落库为 `queued`

#### Scenario: 终态三分映射

- **WHEN** 上游 issue 状态为 done / cancelled / blocked
- **THEN** 分别落 `completed` / `cancelled` / `blocked`（cancelled 是独立终态，不折叠进 completed）

## Requirement: 同步触发与状态面

同步 SHALL 有三个等价入口：服务启动即异步执行一轮（不阻塞启动）、按 `TASK_SYNC_INTERVAL` 周期轮询（默认 60s，设 0 关闭周期但启动轮仍执行）、登录后手动 `POST /api/task-sync`。同步失败 SHALL NOT 使进程 fatal：错误记入状态面，下一轮继续。同步结果 SHALL 只存进程内存（不落库），重启即空。读取面 SHALL 有两个端点：`GET /api/task-sync` 返回 `{running, last}`（last 为最近一轮完整结果——项目/issue 导入与跳过计数、标签镜像数、skips 清单与错误文本，从未同步过为 null）；`GET /api/task-sync/status` 为自动同步状态面（见下）。

#### Scenario: 三入口同一链路

- **WHEN** 服务启动、周期到点、或用户点前端「刷新数据」按钮
- **THEN** 三者执行同一轮全量同步逻辑，效果等价

#### Scenario: 最近一轮结果读取

- **WHEN** 一轮同步完成后调用 `GET /api/task-sync`
- **THEN** 返回 running=false 与 last（含 projects_imported / issues_imported / issues_skipped / labels_imported / skips / error）；从未同步过时 last 为 null；同步未装配时返回 503

#### Scenario: 手动触发的错误码

- **WHEN** 已有同步在进行时 `POST /api/task-sync`
- **THEN** 返回 409
- **WHEN** 上游拉取或落库失败
- **THEN** 返回 502
- **WHEN** 未配置任务源三键（TASK_SYNC_SERVER_URL / TASK_SYNC_TOKEN / TASK_SYNC_WORKSPACE_ID）
- **THEN** 同步路由返回 503

#### Scenario: 状态面字段语义冻结

- **WHEN** 调用 `GET /api/task-sync/status`
- **THEN** 返回 `lastSyncAt`（最近完成一轮的时间，null = 从未完成；running 期间不改写）、`status`（idle|running|success|error，running 优先于一切）、`error`（最近完成一轮的失败原因）
- **AND** 前端以中性口径呈现（「最近活动 / 正常更新 / 上次更新失败」，不暴露同步机制与上游细节），发现 lastSyncAt 变化即失效项目 / 任务 / 需求 / 待决策等视图缓存

## Requirement: 项目仓库绑定随同步解析

同步 SHALL 把任务源项目的资源解析为 infera 项目的 `repo_url`：`github_repo` 资源取其 URL，`local_directory` 资源取其 local_path（按普通 clone 语义处理，不引入 worktree/daemon 特殊模式）；两类并存时 `github_repo` 优先，同类型取 position 最小。解析不出时 SHALL 保留 infera 侧现值、不清空；`default_branch` / `pinned` 归 infera 侧配置，同步 SHALL NOT 覆写。

#### Scenario: 资源解析优先级

- **WHEN** 上游项目同时挂有 github_repo 与 local_directory 资源，或同类型多条
- **THEN** 取 github_repo 的 URL（优先），同类型取 position 最小的可用条目，目标值为空的条目跳过

#### Scenario: 无资源不覆写

- **WHEN** 上游项目无资源或无可解析条目
- **THEN** infera 项目保留现有 `repo_url`，不被清空

## Requirement: 标签库与挂标镜像

标签镜像 SHALL 内建在同步链路里（无独立导入脚本）：先按上游标签 id（幂等键 `external_label_id`）把任务源 workspace 标签库镜像进 infera 标签库，name 与 color hex 原值照抄（不做色彩换算）；再逐交付做差集对齐——缺的挂上、多的摘掉，两轮之间 SHALL NOT 积累。摘标 SHALL 只作用于镜像域（`external_label_id` 非空的同步来源标签），infera 侧人工挂的本地标签 SHALL NOT 被摘除。

#### Scenario: 标签库幂等镜像

- **WHEN** 多轮同步后查看标签库
- **THEN** 同一上游标签始终只占一行，name 与 color 以任务源为准（同 id 命中同一行），`labels_imported` 每轮同值；issue 引用了标签库拉取面未见过的标签时按内嵌对象兜底落库，仍是同一幂等键

#### Scenario: 挂标差集对齐

- **WHEN** 任务源侧在某 issue 上新增或摘除标签后触发一轮同步
- **THEN** infera 侧对应交付的挂标随之挂上或摘除（复合主键幂等，不产生重复关联），标签库条目不受挂标变化影响

#### Scenario: 摘标只动镜像域

- **WHEN** infera 侧某交付上挂有上游没有的同步来源标签，以及人工挂的本地标签（外部 id 为空）
- **THEN** 下一轮同步摘除前者（镜像域内上游没挂就该摘），保留后者；被 skips 规则跳过的单不落库也不挂标，上游整单删除时既有交付与历史挂标维持上一轮状态

## Requirement: 上游建卡与回流

在 infera 侧创建需求 SHALL 经上游建卡链路：校验输入（标题非空、状态只支持 backlog|todo 两档，缺省 backlog）→ 解析项目的外部映射 → 上游建卡（缺省智能体为装配期注入的 Tech Lead，优先级透传）→ autoMerge 时打上游 auto 标签 → 触发一轮同步回流 → 按外部 issue id 读回同步落库的行作为响应。

#### Scenario: 无上游映射的项目拒绝建卡

- **WHEN** 对从未与上游同步、无外部映射的 infera 项目创建需求
- **THEN** 返回错误（项目未绑定上游映射），不在上游建卡

#### Scenario: autoMerge 标签 fail-fast

- **WHEN** 请求带 autoMerge 且上游 workspace 无名为 `auto` 的标签
- **THEN** 建卡前即报错（不代建标签——标签是 workspace 治理对象），不留半成品；建卡成功但打标失败时如实报错并带上卡键，调用方不盲目重试

#### Scenario: 回流尽力而为

- **WHEN** 上游建卡成功后触发的回流同步被占用或失败
- **THEN** 创建请求仍按成功返回（否则前端按失败重试会建出重复卡），读不回同步行时退化为按上游回包 + 同步侧词表拼装的行（无 infera 侧 id，锚点齐全），下一轮自动同步补齐

## Requirement: 上游通道凭据纪律与评论代理

任务源通道 SHALL 只经环境变量配置凭据（ServerURL / Token / WorkspaceID 三键，齐才装配）；client 构造期 SHALL 挡掉误配（地址缺失或非 http(s)、token 或 workspace id 缺失直接报错），不让其漏到运行期变成难排查的 400/401；凭据 SHALL NOT 进入同步服务、存储或日志输出。infera SHALL 作为唯一前台代发上游评论（审批结果、驳回反馈、决策回复、返工指令），并提供增量评论拉取游标供轮询消费。

#### Scenario: 半配在构造期报错

- **WHEN** 三键任一缺失或 ServerURL 非法时构造任务源 client
- **THEN** 构造期返回明确错误，进程不带着坏配置启动

#### Scenario: 评论以服务身份代发

- **WHEN** infera 需要向上游 issue 写入审批结论或返工指令
- **THEN** 经任务源通道以服务身份代发评论（空内容在客户端就地拒绝），用户不直接进上游平台发评论

#### Scenario: 增量评论游标

- **WHEN** 轮询方以上轮游标（最后已交付评论 id + 其时间）拉取新评论
- **THEN** 只返回游标之后的新评论并给出推进后的游标，无新评论时游标原样返回（调用方只需存回；评论消费与去重语义归需求流转域）
- **AND** 锚点评论已在上游被删除（游标 id 不在本轮结果集）时，返回整个 since 窗口的评论并推进游标——宁可重发不漏发，重发由评论 id 去重兜住

## Requirement: 仓库检出与交付分支语义

workdir SHALL 是该交付独占的仓库检出：从项目默认分支 clone（幂等，已存在则复用；无仓库的绿地项目只建目录），各阶段共享同一目录，改动直接提交在当前分支。交付分支 SHALL 由引擎在 code_review 门禁固化时创建并推送，命名为 `infera/<交付 id 前 8 位>`；交付分支与 PR SHALL NOT 由 MCP 客户端或人工手工创建。git 访问凭据 SHALL 只出现在一次性命令行参数中，clone 完成后远端地址重置回原始 URL，secret SHALL NOT 落入 workdir 的 git 配置或错误日志。

#### Scenario: 检出约定

- **WHEN** 交付进入执行需要 workdir
- **THEN** 获得从默认分支检出的独占目录并记录 base commit，同一交付各阶段复用；并发获取同一交付的 workdir 收敛为一次 clone，不同交付互不阻塞

#### Scenario: 凭据不落盘

- **WHEN** 以 https + token 访问远端仓库
- **THEN** token 只注入一次性命令行 URL，clone 后 origin 地址重置为原始 URL，错误信息中的 token（含 URL 转义形态）被抹除；所有 git 子进程以非交互、忽略宿主 global/system 配置的方式运行

#### Scenario: 绿地与分支固化

- **WHEN** 项目未绑定仓库（绿地）
- **THEN** workdir 仅初始化本地 git 仓库（保证 HEAD 恒存在），产物只做本地 commit、不 push，分支为本地 main
- **WHEN** 项目绑定仓库且交付到达固化点
- **THEN** 产出提交并推送到 `infera/<id 前 8 位>` 分支后开 PR；push 失败视为整体失败（交付 blocked、不释放 workdir），仅 PR 创建失败不算失败（push 已固化产出，失败原因单独记录）

## Requirement: GitHub PR 代理面

对 GitHub 的全部访问 SHALL 由 infera 后端代理（PR 元数据、行级评审评论、diff 统计、合并），用户 SHALL NOT 需要访问 GitHub 页面；PR 深链仅作为逃生口。合并判定 SHALL 单看 merged 字段（state 只有 open/closed 两值，closed 既可能是合并成功也可能是驳回关闭）；「当前不可合并」类失败 SHALL 与鉴权 / 网络错误区分归类。

#### Scenario: PR 状态判定

- **WHEN** 拉取 PR 元数据用于收口判定
- **THEN** 以 merged 字段判定是否已合入（closed + merged=false 是驳回形态）；mergeable 为 null 时语义是「未知」，调用方应重查而不是误判冲突

#### Scenario: 评论与 diff 统计拉取

- **WHEN** 拉取 PR 的行级评审评论或文件级 diff 统计
- **THEN** 自动翻页（每页 100 条，页满继续）；超过翻页上限（2 万条）时报错拒绝继续，不静默截断

#### Scenario: 合并失败的归因分类

- **WHEN** 合并 PR 返回 405 / 409，或 HTTP 200 但 merged=false
- **THEN** 归因为「当前不可合并」类失败（可重试或转人工），与鉴权失败、网络错误区分；合并方法只接受 merge / squash / rebase，留空默认 merge commit

## Requirement: 本机 helper 拉起通道

常驻用户本机的 helper（`infera-link`）SHALL 把网页上的「在本地处理此阶段」按钮接到本机 CLI：收到 handle 请求后经 MCP `get_context` 取驾驶上下文，生成本次会话的 MCP 客户端配置与初始提示并落盘（`~/.infera/link/<delivery_id>/`，暂存目录权限 0700、目录内文件权限 0600——配置内含 token，可审计），然后在新的本机终端拉起 CLI 会话定位于交付 workdir；产出由 CLI 经 MCP `submit_stage_output` 交回，流水线自动推进。helper 自身的暴露面 SHALL 收紧：监听地址只允许本机回环（localhost / 127.0.0.1 / ::1，绑 0.0.0.0 / 局域网 / 空 host 在配置加载期即拒绝启动）；`/handle` SHALL 要求携带 Origin 且其主机为本机（无 Origin 的非浏览器客户端拒绝 403——比 CORS 回显更严，回显只挡读响应不挡发请求）；CORS 仅对本机来源回显 Origin，`delivery_id` 入路径前按白名单字符集校验以挡路径穿越。

#### Scenario: 按钮到本机会话

- **WHEN** 交付停在本机绑定节点，用户在网页点「在本地处理此阶段」
- **THEN** helper 返回该节点与 workdir，落盘 MCP 配置与初始提示，并在本机终端拉起带该配置与提示的 CLI 会话；helper 未运行或端口不符时按钮报可操作错误

#### Scenario: 暴露面收紧

- **WHEN** helper 以非回环 `--listen` 启动，或 `/handle` 请求不带 Origin / Origin 主机非本机，或 `delivery_id` 含路径穿越等非法字符
- **THEN** 分别在配置加载期拒绝启动 / 返回 403 / 返回 400，不拉起任何本机会话

#### Scenario: 两种拉起形态的初始提示

- **WHEN** 分别从停车节点（pending_local）与门禁预审（code_review 审查角色绑本机）拉起
- **THEN** 前者初始提示为与引擎发给绑定 agent 同源的角色 prompt + 交回契约；后者引导 `get_gate` 查看门禁详情（含真 diff）并用 `approve_gate` / `reject_gate` 裁定

#### Scenario: 配置与健康检查

- **WHEN** helper 以 flag / 环境变量 / 默认值（依次降级）配置服务地址、token、监听地址、CLI 与拉起方式
- **THEN** `/healthz` 返回健康信息且不回显 token；无头环境可用「不执行、只把命令打到日志」的拉起方式；端到端链路只在 claude CLI 上验证过，codex 形态按 MCP 配置契约生成
