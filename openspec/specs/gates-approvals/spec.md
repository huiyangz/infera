# gates-approvals（审批卡与门禁）

## Purpose

管理流水线中的人工门禁与审批面：spec 审批、代码审查（含双道 agent 审查 findings 的呈现与裁定）、终审合并（GitHub 代理），以及跨项目待决策列表；含多驾驶面（Web / MCP）对同一交付操作的互斥约束。

## Requirement: 识别闸门评论并落卡

闸门轮询器 SHALL 增量拉取任务源父 issue 评论，按冻结的顶格前缀协议（顶格、区分大小写、不做 trim）把每条评论归类为审批卡（`待批准：`，全角冒号）、决策卡（`需要决策：`，全角冒号）或合并卡（`verdict:`，ASCII 冒号）并落库；不命中任何前缀的评论——含半角/全角冒号互换等相近变体——SHALL 落兜底「有新动态」卡，不得伪装成闸门。同一评论 SHALL 只产生一张卡（按评论 id 幂等去重，重复轮询不重发）；评论正文中出现的首个规范形 GitHub PR URL SHALL 记为该需求的 PR 引用，已有引用则不覆盖。（闸门协议冻结于 `server/internal/flow/gate.go`；落卡在 `server/internal/gatepoll`）

#### Scenario: 审批评论落审批卡

- **WHEN** 一条评论以「待批准：」（全角冒号）顶格开头
- **THEN** 系统为该需求落一张 pending 审批卡，正文为评论内容，且同一评论重复轮询不再次落卡

#### Scenario: 相近变体不命中

- **WHEN** 评论以「待批准:」（半角冒号）或其他变体开头
- **THEN** 系统不识别为审批事件，该评论落兜底「有新动态」卡

#### Scenario: 首个 PR 引用被提取

- **WHEN** 任一评论正文含规范形 `https://github.com/<owner>/<repo>/pull/<n>` 且该需求尚无 PR 引用
- **THEN** 该 URL 记为需求的 PR 引用，后续评论中的其他 PR URL 不覆盖既有引用

## Requirement: 轮询推进大节点并持久化游标

轮询器 SHALL 以固定间隔对全部在途需求轮询（间隔配置须落在 (0, 60s]，默认 30s——上游状态变化须 2 分钟内反映；进程启动即先跑一轮）：按任务源父 issue 状态推进需求大节点（主链只进不退、允许跨级），并把轮询游标（增量评论位置、上次状态、是否已见 verdict、PR 引用）与节点同批落库，重启后从游标续读、不重放已消费评论。写序 SHALL 先落卡后落游标：中途失败重跑宁可多弹卡（评论 id 去重兜住绝大多数）也不漏卡。单个需求轮询失败 SHALL 不阻断其余需求（失败进日志、下一轮自然重试）。

#### Scenario: 状态映射推进

- **WHEN** 父 issue 状态在一个轮询窗口内从 in_progress 连跳为 done
- **THEN** 需求节点跨级推进至 delivered

#### Scenario: 回退与不可映射不回退节点

- **WHEN** 父 issue 状态回退（如 in_review → in_progress），或为 blocked / 未知等不可映射状态
- **THEN** infera 侧大节点保持原节点不变（infera 是单一状态源，上游状态回退不导致节点回退）

#### Scenario: 重启续读不重放

- **WHEN** 服务重启后恢复轮询
- **THEN** 已消费过的评论不再产生新卡，增量评论从持久化游标之后继续

## Requirement: 兜底不漏合并闸门

系统 SHALL 以保守兜底防止漏掉合并闸门：识别不了前缀的评论一律落「有新动态」卡（卡面经所属需求行的任务源深链可一键查看完整时间线）；父 issue 状态跃入 in_review 但该需求从未见过任何 `verdict:` 评论时，SHALL 落一张中性「有新动态」卡提醒人工查看执行平台进展——首次轮询（无上次状态）视同跃入，停留在 in_review（状态未变）不算跃迁、不重复弹卡。

#### Scenario: 跃入待验收且无结论

- **WHEN** 需求状态从 in_progress 跃入 in_review 且从未见过 verdict 评论
- **THEN** 系统落一张中性「有新动态」卡，正文说明已进入待验收但尚未收到合并评审结论

#### Scenario: 同轮先见结论不误弹

- **WHEN** 同一轮轮询中 verdict 评论与「跃入 in_review」同时到达
- **THEN** 不弹中性卡（消费评论时先把已见结论标记置真）

#### Scenario: 停驻不重复弹

- **WHEN** 需求已停在 in_review 且仍无 verdict
- **THEN** 下一轮轮询不再重复弹中性卡

## Requirement: 审批卡等人工批复

审批卡 SHALL 以 pending 态等待人工批复：批准 SHALL 向任务源父 issue 代发固定文本 `approved`；驳回 SHALL 要求非空反馈并原样代发。动作 SHALL 先成功代发，再在单事务内收卡（resolved）并写审计（actor=user）；任务源代发失败时卡 SHALL 保持 pending、不写审计（失败动作不算动作），可重试。动作 SHALL 只适用于同类型的待处理卡：卡已处理或类型不符时以冲突拒绝。卡面呈现的任务源深链取自所属需求行的 `external_issue_url`（卡资源本身不携带链接字段）。（REST 面：`/api/requirements/{id}/cards/{cardID}/approve|reject`）

#### Scenario: 批准

- **WHEN** 用户对 pending 审批卡点批准
- **THEN** 系统向父 issue 代发 `approved`，卡置 resolved，审计记 actor=user、action=approve

#### Scenario: 驳回必带反馈

- **WHEN** 用户提交非空驳回反馈
- **THEN** 反馈文本原样代发，卡置 resolved，审计记 reject
- **AND** 反馈为空白时请求被拒绝（400），不代发不收卡

#### Scenario: 失败动作不算动作

- **WHEN** 任务源代发失败，或动作指向已 resolved 的卡 / 类型不符的卡
- **THEN** 卡不被收口、不留审计（代发失败时卡保持 pending 可重试），请求以上游错误或冲突（409）返回

## Requirement: 决策停驻与人工决策

识别到「需要决策：」评论时，系统 SHALL 在同一事务内落决策卡并把需求节点推进为 needs_decision（仅从活跃节点可进入）；停驻期间上游状态推进 SHALL 挂起——节点只经人工决策动作离开，轮询不越权解围。决策动作 SHALL 四选一：`retry`（重试）/ `skip`（跳过）/ `custom`（自定义文本原样代发）均为「继续执行」→ 节点回 in_progress；`abort`（中止）→ 节点直达 delivered（大节点集无 cancelled，中止即退出在途）。决策文本 SHALL 代发到父 issue；节点不在 needs_decision 的存量决策卡只代发收卡、不跃迁；未知选项或 custom 空文本 SHALL 被拒绝（400）。（`server/internal/reqservice/actions.go`）

#### Scenario: 继续类决策回执行

- **WHEN** 停在 needs_decision 的需求收到 retry / skip / custom 决策
- **THEN** 固定文本（重试/跳过）或自定义文本原样代发，节点回 in_progress，卡 resolved、审计记 decide

#### Scenario: 中止直达终态

- **WHEN** 决策为 abort
- **THEN** 代发「中止」，节点直达 delivered，需求退出在途集合

#### Scenario: 停驻期间不越权推进

- **WHEN** 需求停在 needs_decision 期间上游父 issue 状态发生变化
- **THEN** 轮询不推进该需求节点，保持等待人工决策

## Requirement: 合并卡人工终审

`verdict: PASS|FAIL` 评论 SHALL 落合并卡并标记该需求已见结论（结论词全词、大小写敏感，PASSING 不算 PASS）。合并卡 SHALL 等待人工裁定：合并 SHALL 经 GitHub API 合并该需求关联的 PR；拒绝并返工 SHALL 要求非空反馈并原样代发。人工合并成功 SHALL 收卡并写审计（actor=user），但 SHALL NOT 推进大节点——节点由轮询按上游父 issue 状态推进（单一状态源；自动合并档位的直达已交付是显式例外，见「按项目合并策略自动合并」）。需求尚无 PR 关联时合并 SHALL 以冲突拒绝；PR 当前不可合并（如 CI 未过）SHALL 归因为「稍后可重试」类错误且卡保持 pending。合并卡面呈现的 PR 深链取自所属需求行的 `pr_url`（卡资源本身不携带链接字段），并渲染 PR 的行级评审评论与 diff 概要（只读端点 `GET /api/requirements/{id}/pr-review`：不落卡、不动节点）。（`server/internal/github` 为 GitHub 代理）

#### Scenario: PASS 卡人工合并

- **WHEN** 用户对 verdict PASS 的合并卡点合并且 PR 可合并
- **THEN** 系统经 gh API 合并 PR，卡置 resolved，审计记 merge（actor=user），大节点不变

#### Scenario: 拒绝并返工

- **WHEN** 用户提交非空返工反馈
- **THEN** 反馈原样代发，卡置 resolved，审计记 rework；空反馈被拒绝（400）

#### Scenario: 合并受阻不收口

- **WHEN** 需求尚无 PR 关联，或 GitHub 以「当前不可合并」类拒绝（如 CI 未过）
- **THEN** 动作以冲突 / 可重试错误返回，卡保持 pending，等轮询提取 PR 引用或下一轮重试

## Requirement: 按项目合并策略自动合并

系统 SHALL 支持合并策略三档，档位经 `GET/PUT /api/projects/{id}/merge-policy` 按项目读写（UPSERT 以 project_id 为键）：`manual`（默认，未设置即手动档）——自动合并完全不动作，合并卡留待人点；`auto_pass`——verdict PASS 的 pending 合并卡每轮清扫时立即自动合并；`threshold`——PR diff 行数（additions+deletions）≤ 阈值自动合并，超过弹卡留人。**自动合并清扫解析档位时是部署级单行语义**：requirements 冻结 schema 无项目关联列，清扫器取 `project_settings` 中 `project_id` 最小的一行对全部需求生效——单项目部署下与「项目级」等价，多项目部署下后写的其它项目档位对自动合并不生效。这是冻结 schema 下的已知限定而非待修缺陷：按项目精确生效需先给 requirements 增加项目关联列（schema 变更），不在本 spec 范围。档位校验 SHALL 拒绝 manual/auto_pass 携带阈值、threshold 缺正阈值与未知档位；解析不出有效档位时 SHALL 回落手动档（自动合并是风险动作，数据异常取最保守行为）。自动合并成功 SHALL 一次性收口：卡 resolved、审计 actor=system、节点直达 delivered、游标落库。合并暂被阻塞 SHALL 保持卡 pending、下一轮自然重试；PR 已 closed 且未合并（被驳回关闭）SHALL NOT 视为了结——卡转人工，不误置已交付、不误记 merge 审计；PR 已 closed 且已合并（合并后收口丢失的崩溃窗口）SHALL 收敛收口。FAIL 结论 SHALL NOT 被自动动作（拒绝返工是人决策）。清扫中除「稍后可重试」类外的失败（diff 统计/PR 读取失败、鉴权或网络错误等硬失败）SHALL 只记服务端日志并保持卡 pending 留待人工——不落卡、不写审计、不动节点，自动合并对该卡静默停手。

#### Scenario: auto_pass 即时合并

- **WHEN** 生效档位为 auto_pass 且出现 verdict PASS 的 pending 合并卡
- **THEN** 轮询清扫时自动合并，卡 resolved、审计记 actor=system、节点直达 delivered

#### Scenario: threshold 按量分流

- **WHEN** 生效档位为 threshold（阈值 N）且存在 PASS 合并卡
- **THEN** PR diff 行数 ≤ N 时自动合并收口；> N 时合并卡保持 pending 留人处理

#### Scenario: 被驳回关闭的 PR 转人工

- **WHEN** PASS 合并卡对应的 PR 状态为 closed 且 merged=false
- **THEN** 卡保持 pending 转人工处理，节点不置 delivered、不写 merge 审计

#### Scenario: 硬失败静默停手

- **WHEN** 清扫自动合并时拉取 diff 统计或 PR 元数据失败，或合并返回鉴权/网络类硬失败
- **THEN** 卡保持 pending、不产生新卡与审计、节点不变，失败只进服务端日志，人工仍可对该卡手动合并

## Requirement: 挂起交付门禁并按裁定流转

交付流水线 SHALL 在四个人工门禁（spec_approval / design_approval / tasks_approval / code_review）暂停等人：推进到门禁节点时先执行前置——code_review 门先固化产出（commit、push 分支、开 PR、落 diff 产物），再跑门禁前置预审（code_review 角色 agent，产出 agent_output 产物）与双道审查——然后置 pending_gate、发 `gate_pending` 事件并停住。批准 SHALL 清 pending_gate、发 `gate_approved` 并前进到后继阶段（code_review 批准即交付完成、释放 workspace，但 SHALL NOT 自动合并 PR——交付 PR 的合并由人在 GitHub 完成）。打回 SHALL 清 pending_gate、把理由持久化在交付上、发 `gate_rejected`（含理由）并回退到该门的打回目标（spec_approval→spec、design_approval→design、tasks_approval→tasks、code_review→code_gen）；打回成功后两个驾驶面（Web / MCP）SHALL 立即由后台驱动重跑被打回的阶段，理由在该次重跑的 prompt 注入一次后清空（引擎裸调用 Reject 只回退停车不重点火，但产品入口都经后台驱动）；锁的交接方式按面区分——Web 面把锁所有权移交给后台驱动 goroutine（驱动跑完才放锁），MCP 面簿记后先放锁、由后台驱动自行重取同一把锁（放锁到重取之间另一面可先拿锁，安全：所有引擎入口各自做状态校验，锁只保证不并发进引擎）。交付无待审批门禁时，门禁读取 / 批准 / 打回 SHALL 被拒绝：Web 面报错（400）；MCP 工具面同一冲突以 `isError: true` 的工具结果表达，文本说明无挂起门禁及交付当前所停阶段。门禁事件 SHALL 全部进入交付时间线（append-only，按发生序）。

#### Scenario: code_review 门挂起时序

- **WHEN** 交付推进到 code_review 节点
- **THEN** 时间线依次出现产出固化（persist_done）、门禁预审产出（agent_output）、双道审查产出（review_findings）与 gate_pending，交付停在待审批

#### Scenario: 打回回退并注入理由

- **WHEN** 用户对 code_review 门提交打回理由 R
- **THEN** 交付回退到 code_gen，gate_rejected 记理由 R，后台驱动立即重跑 code_gen 且该次 prompt 注入「人打回：R」一次后清空

#### Scenario: 批准终审门即完成

- **WHEN** 用户批准 code_review 门
- **THEN** 发 gate_approved，交付状态转 completed，PR 保持开放等待人工在 GitHub 合并

## Requirement: 规格门裁定交付模式

spec_approval 门 SHALL 由人工裁定交付模式 `small|large`：规格产出的 `infera-complexity` 块给出建议，人工可改判（界面标记已改判），无有效建议时默认 small。批准后 SHALL 发 `complexity_set` 事件：large 走设计与任务拆解链路（11 阶段），small 直达测试生成（7 阶段）。complexity 选项 SHALL 只在 spec_approval 门被接受，非法值或错门携带 SHALL 被拒绝且门禁不被消耗。

#### Scenario: 批准小任务

- **WHEN** 规格门批准时未携带选项且无有效建议，或人工选定 small
- **THEN** 模式定为 small，流水线直达 test_gen（7 阶段链路）

#### Scenario: 改判大任务

- **WHEN** 建议为 small 而人工选定 large
- **THEN** 模式定为 large，流水线进入 design（11 阶段链路），时间线记 complexity_set

#### Scenario: 非法选项被拒

- **WHEN** complexity 值不属于 small|large，或在非 spec_approval 门携带 complexity
- **THEN** 请求被拒绝（400），门禁保持待审批

## Requirement: 门禁裁定选项按门校验

门禁批准附带的选项 SHALL 与门类型一一对应：拆分清单（split）只允许在 design_approval——批准并拆分时按清单创建子交付，绿地项目（未绑定仓库）不支持拆分，子项标题必填、波次小于 1 归一为 1；任务清单改写（tasks）只允许在 tasks_approval——以新清单覆盖生成任务产物并发 `tasks_overridden` 事件后放行。选项在错误的门携带 SHALL 被拒绝（400）且门禁不被消耗。

#### Scenario: 批准并拆分

- **WHEN** 在 design_approval 提交标题齐全的非空拆分清单
- **THEN** 门禁通过并按清单创建子交付，父交付转入拆分父模式

#### Scenario: 绿地不可拆分

- **WHEN** 项目未绑定仓库时在 design_approval 提交拆分清单
- **THEN** 请求被拒绝，门禁保持待审批

#### Scenario: 错门携带选项

- **WHEN** 在 code_review 或其他门携带 split / tasks
- **THEN** 请求被拒绝（400），门禁不被消耗

## Requirement: 双道审查意见只呈现不拦截

code_review 门前 SHALL 编排两道独立 agent 审查，按固定顺序执行：规格符合性（spec_conformance；有非空任务清单时逐项核验、task_based=true，否则按规格整体核验）与代码质量（code_quality；意见不关联具体任务）。两道各自把结构化意见（严重度 critical|major|minor|info、结论、证据引用、关联任务号；未知严重度归一 info、负任务号归一 0、空消息过滤）存为独立 artifact（append-only，最新一次生效——打回重跑后再审自动覆盖展示），并各发一条 `review_findings` 事件。引擎对畸形输出 SHALL 容错：无 `infera-findings` 块 / 坏 JSON / 非数组归一为零意见，原始输出完整留档（raw）供兜底阅读，不因解析失败崩溃。意见 SHALL 只呈现不参与流转判定：批准 / 打回由人工决定，零意见也不自动放行。某道缺绑定或 agent 失败 SHALL 发 `stage_failed`（payload 写明哪道）并把交付置 blocked，门禁不挂；某道绑定本机（local runner）时 SHALL 跳过该道（本机交互即人工审查员，发 `local_stage_pending`），门禁照常挂起、该道显示「未产出」。门禁读取 SHALL 恒定返回两道（固定顺序 spec_conformance → code_quality）及 present 标志，坏数据以原文兜底展示。（契约见 `docs/superpowers/specs/2026-08-19-r10-dual-review.md`）

#### Scenario: 两道产出后挂门等人

- **WHEN** 两道审查 agent 均产出
- **THEN** 门禁页呈现两道意见卡（严重度分级、关联任务号、证据引用、可展开原始输出）与代码 diff，交付停在 code_review 门等待人工裁定

#### Scenario: 缺绑定或审查失败不挂门

- **WHEN** 编排缺少某道审查的绑定，或某道 agent 执行失败
- **THEN** 交付发 stage_failed（写明缺失/失败的道）并置 blocked，pending_gate 不挂，已产出的另一道报告保留

#### Scenario: 本机绑定跳过该道

- **WHEN** 某道审查绑定 local runner
- **THEN** 该道无 findings 产出、发 local_stage_pending 事件，门禁照常挂起，门禁页该道显示未产出（present=false）

## Requirement: 提供跨项目需要决策列表

系统 SHALL 提供跨项目的「需要决策」列表（`GET /api/pending-decisions`，需登录）：列出全部停在人工门禁（pending_gate 非空且状态非 completed）的交付，按 updated_at 降序；行为纯展示——点击进入对应交付详情处理，不在此页直接裁定。每行 SHALL 带任务、项目、待决策门（未知门 slug 原样显示不崩溃）、更新时间，以及来源（source）：沿交付 parent 链爬到链根，用链根 external_issue_id 匹配源头需求的 requirements.source 透出（拆分子行透出链根来源）。无来源、本地需求、链根不可解析、需求服务未装配或读取失败 SHALL 一律降级为空串（前端显示「—」），列表可用性 SHALL NOT 依赖需求服务；空结果 SHALL 返回 `[]`。（INFERA-267）

#### Scenario: 停门行与来源透出

- **WHEN** 某交付停在 spec_approval 且其链根需求的 source 为 web
- **THEN** 列表出现该行（含来源 web 与门标签），点击任务进入该交付详情

#### Scenario: 拆分子行透链根来源

- **WHEN** 拆分子交付（自身无外部 issue）停在门禁
- **THEN** 其来源列取链根父需求的 source

#### Scenario: 来源解析降级

- **WHEN** 行为本地需求 / 无来源，或需求服务未装配 / 读取失败
- **THEN** source 为空串，页面来源列显示「—」，列表请求仍正常返回

## Requirement: 多驾驶面对同一交付互斥

Web API 与 MCP 两个驾驶面对同一交付的推进 / 裁定操作（批准、打回、合并恢复、本机产出交回与后台推进）SHALL 经同一把交付级锁串行，保证任一时刻至多一个操作进入引擎（引擎自身不做并发保护，各入口自行做状态校验，锁只保证不并发进引擎）；纯读操作（交付详情、门禁读取、MCP get_context / get_gate）SHALL NOT 加锁。MCP 的 approve_gate / reject_gate SHALL 与 Web 批准 / 打回走同一引擎单入口。

#### Scenario: 并发裁定串行化

- **WHEN** Web 批准与 MCP approve_gate 同时到达同一交付
- **THEN** 两操作被串行化，引擎内峰值并发为 1，门禁只被消耗一次，状态与时间线不乱序

#### Scenario: 读不加锁

- **WHEN** 交付正在后台推进时读取门禁 / 详情
- **THEN** 读取不被阻塞，返回当时已落库的状态
