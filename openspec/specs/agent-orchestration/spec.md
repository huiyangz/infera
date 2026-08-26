# agent-orchestration（Agent 编排与执行）

## Purpose

管理交付（delivery）的执行引擎与 agent 编排：交付沿 11 节点阶段图（intake → spec → spec_approval → [design → design_approval → tasks → tasks_approval] → test_gen → code_gen → unit_test → code_review → DONE，含复杂度分岔、unit_test 失败回环与 blocked 语义）串行推进；agent 先登记、再被项目绑定到可绑定节点，以可替换运行时（本机子进程 / 一次性容器 / HTTP 服务 / 本机交互）执行；workdir 是一体化前置资源，全程共享、终态后延迟清理；产物固化为本地提交、推送分支与 PR。本域只管执行编排本身：需求大节点状态机（flow 包）与交付卡的呈现归 task-management，门禁的裁定面、审查意见的裁定与终审合并归 gates-approvals，MCP 工具面与任务源同步归 mcp-integration，跨项目时序聚合与统计口径归 statistics，页面结构归 web-routing。

## Requirement: 维护交付阶段图

系统 SHALL 以静态阶段图驱动每条交付：同一交付内节点严格串行、一次只处于一个节点；节点分三类——agent 节点（产出产物）、人工门禁节点（暂停等待裁定）、命令节点（系统执行）；推进到 DONE 即交付完成。（REST 面：api/deliveries.go；图定义 engine/graph.go）

#### Scenario: 按图推进直到门禁

- **WHEN** 一条 active 交付被驱动执行
- **THEN** 引擎从其 current_stage 起逐节点执行：agent 节点成功后写入该节点约定 kind 的产物并前进到 Next 节点，命令节点执行后前进，直到遇到人工门禁节点挂起等待

#### Scenario: intake 仅是流程标志

- **WHEN** 交付处于 intake 节点
- **THEN** 引擎不做任何业务执行直接放行到下一节点

#### Scenario: 未知节点即终态

- **WHEN** 交付的 current_stage 不在阶段图中
- **THEN** 交付转 blocked 并产生 `delivery_blocked` 事件（reason 写明该未知阶段名）

## Requirement: 按复杂度分岔阶段链

系统 SHALL 在规格审批时确定复杂度并据此分岔阶段链：small（含历史空值）跳过 design / design_approval / tasks / tasks_approval 四节点直达 test_gen；large 走完整 11 节点链。复杂度取值由 spec_approval 门的裁定决定——批准不带选项时自动解析规格产物的 `infera-complexity` 建议块、无有效建议默认 small（裁定面语义见 gates-approvals「规格门裁定交付模式」）。

#### Scenario: 批准为小需求

- **WHEN** spec_approval 被批准且复杂度为 small
- **THEN** 下一节点为 test_gen，design 与 tasks 相关四节点被跳过，事件记录 complexity_set

#### Scenario: 批准为大需求

- **WHEN** spec_approval 被批准且复杂度为 large
- **THEN** 下一节点为 design，其后依次经过设计门、任务节点与任务门再进入 test_gen

#### Scenario: 历史空值按小需求

- **WHEN** 交付的复杂度为空字符串（历史数据）
- **THEN** 分岔行为与 small 完全一致

## Requirement: 登记 Agent 及其运行时配置

系统 SHALL 维护全局 agent 注册表：每个 agent 由全局唯一的名称与运行时类型（cli / http / docker / local）标识，配置按类型校验（cli 需字符串数组 command、http 需 url、docker 需 image、local 无必填项）；仍被绑定引用的 agent SHALL NOT 被删除。（REST 面：api/orchestration.go）

#### Scenario: 创建与校验

- **WHEN** 以合法的名称、runner 与配套 config 创建 agent
- **THEN** 创建成功并返回该 agent
- **AND** 名称缺失、runner 不在枚举内、或 config 类型不符时拒绝并给出字段级错误

#### Scenario: 名称冲突与陈旧版本

- **WHEN** 以已存在的名称创建 agent，或以过期版本修改 agent
- **THEN** 均以冲突（409）拒绝，提示刷新后重试

#### Scenario: 删除被引用的 agent

- **WHEN** 删除仍被某项目某节点绑定引用的 agent
- **THEN** 以冲突（409）拒绝，并列出全部引用位置（项目名 / 节点）

## Requirement: 以可替换运行时执行 Agent

系统 SHALL 通过统一运行时契约执行 agent 调用——输入角色、提示词与 workdir，输出合并的标准输出文本，运行时可替换；本机交互（local）类型不直接执行。单次 agent 调用超过 30 分钟 SHALL 判为超时失败。（agent/ 包）

#### Scenario: 本机子进程运行时

- **WHEN** 节点绑定的 agent runner 为 cli
- **THEN** 引擎在交付 workdir 下以 config.command 起子进程，注入 INFERA_ROLE / INFERA_WORKDIR / INFERA_PROMPT 环境变量并把提示词同时写入 stdin，stdout 与 stderr 合并作为产出，非零退出判为失败

#### Scenario: 容器与 HTTP 运行时

- **WHEN** runner 为 docker
- **THEN** 每次调用创建一次性容器，workdir 绑定挂载为 /work，提示词作为命令末位参数，容器用后强制删除
- **AND** runner 为 http 时改为 POST config.url 携带 {role, prompt, workdir}，返回 200 且含 output 视为成功，客户端超时 10 分钟

#### Scenario: 调用超时

- **WHEN** 一次 agent 调用耗时超过 30 分钟
- **THEN** 判为失败，交付转 blocked 并产生 stage_failed 事件

## Requirement: 解析项目级节点绑定

节点绑定 SHALL 只有项目级一种来源：六个必需节点（spec / test_gen / code_gen / code_review / spec_conformance / code_quality）必须全部有指向存在 agent 的有效绑定，否则交付阻断；design 与 tasks 为可选节点，缺省时回落到进程默认 runner。保存绑定为单事务全量替换。（orchestration/ 包）

#### Scenario: 可绑定节点集由服务端单一事实源决定

- **WHEN** 前端渲染绑定编辑表（web features/pipeline/binding-editor）
- **THEN** 只渲染服务端下发的可绑定节点清单（八个 agent 节点），门禁类节点（spec_approval 等）不出现在可绑定集——组件不自行列节点，也不做取数与保存（调用方负责）

#### Scenario: 缺必需绑定即阻断

- **WHEN** 交付进入某必需节点而所属项目缺该节点的有效绑定，或绑定指向不存在的 agent
- **THEN** 产生写明缺哪个节点的 stage_failed 事件，交付转 blocked，系统不自动重试

#### Scenario: 可选节点缺省回落

- **WHEN** design 或 tasks 节点未绑定
- **THEN** 引擎回落到构造期默认 runner 继续推进，交付不因此阻断

#### Scenario: 绑定保存的原子性

- **WHEN** 保存的项目绑定中含不可绑定节点、不存在的 agent 或配置非法的 agent
- **THEN** 整体拒绝（400），既有绑定保持原样不产生半写；提交空绑定对象则清空该项目的全部绑定

## Requirement: 在人工门禁暂停并接受裁定

交付到达门禁节点时 SHALL 暂停并挂起等待人工裁定：批准沿该门的语义前进（可按门携带复杂度 / 拆分 / 任务清单覆盖选项），打回回到该门的 RejectTo 节点并把理由作为一次性执行反馈注入。裁定入口 SHALL 只有引擎单一入口（HTTP 与 MCP 复用）。

#### Scenario: 批准前进

- **WHEN** 挂起中的门禁被批准
- **THEN** 清除挂起、记录 gate_approved，并按该门语义前进；批准请求携带的选项（complexity / split / tasks）只对当前门合法

#### Scenario: 选项与门不匹配

- **WHEN** 批准请求携带当前门不支持的选项
- **THEN** 拒绝该请求（400）且门不被消费，交付仍挂起在原门

#### Scenario: 打回回环

- **WHEN** 挂起中的门禁被打回并附理由
- **THEN** current_stage 置回该门的 RejectTo 节点、记录 gate_rejected；理由只在下一次执行该节点时注入一次后即清空，不重复累积

## Requirement: 按任务清单逐条实现

携带任务清单产物的交付 SHALL 在 code_gen 按清单逐条实现：每个剩余任务一次 agent 调用（提示词附带「当前任务 i/N」与任务详情），完成即写入持久的 task_done 标记；重入只执行剩余任务，进度不因回环丢失。（engine/tasks.go）

#### Scenario: 逐条执行并留痕

- **WHEN** code_gen 带非空任务清单执行
- **THEN** 按清单顺序对每个任务发起一次独立 agent 调用，每完成一项写入一条 task_done 产物并产生 task_done 事件

#### Scenario: 回环只跑剩余任务

- **WHEN** unit_test 失败回环或门禁打回后 code_gen 重入
- **THEN** 已有 task_done 标记的任务被跳过，只对剩余任务发起调用；若全部完成但存在执行反馈，则发起一次整体修补调用以避免死锁

#### Scenario: 无清单整体实现

- **WHEN** 交付没有任务清单产物
- **THEN** code_gen 退化为一次整体实现调用，agent 原始输出全文即落为 summary 产物（任务清单摘要形式仅出现在带清单路径）

## Requirement: 运行 unit_test 并回环

unit_test 命令节点 SHALL 在交付 workdir 内执行配置的测试命令（本机脚本或容器内命令），以退出码判定通过与否，输出 SHALL 始终留档为 test_output 产物；失败回环到 code_gen 并注入截断后的测试输出作为反馈，连续 3 次失败转 blocked；通过即清零失败计数前进。（testrunner/ 包）

#### Scenario: 通过前进

- **WHEN** unit_test 命令以退出码 0 结束
- **THEN** 失败计数清零，产出 test_output 产物后前进到 code_review

#### Scenario: 失败回环并携带反馈

- **WHEN** unit_test 命令非零退出且失败计数未达上限
- **THEN** 产生 test_failed 事件（含输出），交付停在 code_gen，下一轮 code_gen 的提示词注入上一轮测试输出（截断保护）

#### Scenario: 连续失败转终态

- **WHEN** unit_test 连续第 3 次失败
- **THEN** 交付转 blocked 并写明连续失败次数已达上限，不再自动回环

## Requirement: 留痕阶段产物与事件

系统 SHALL 以只追加方式留痕每条交付的阶段产物与事件：产物按 kind 追加保存、读取时最新一条生效（历史全部保留供审计）；每次状态变化产生的事件既持久化又推送到该交付的实时事件流。

#### Scenario: 产物最新生效且历史保留

- **WHEN** 同一 kind 的产物被再次写入（如打回重跑后再审）
- **THEN** 读取方取到最新一条作为当前生效内容，此前所有历史条目仍可追溯

#### Scenario: 事件持久并实时推送

- **WHEN** 引擎产生任一事件（阶段开始、门禁挂起、任务完成、固化完成、合并进展等）
- **THEN** 事件落库并推送到该交付的事件流，前端时间线据此呈现

#### Scenario: 事件流驱动界面刷新

- **WHEN** 前端已订阅某交付的事件流并收到任一消息
- **THEN** 交付详情、门禁与项目列表等关联查询被失效并重新拉取（节流合并，不做轮询）；断线时按退避重连，鉴权类断开码则立即停止不再重试

## Requirement: 失败转终态并保留救援现场

除 unit_test 回环外，任何执行失败（agent 失败或超时、workdir 获取失败、审查道失败、固化失败、未知节点）SHALL 立即转 blocked 终态并写明原因，系统 SHALL NOT 自动重试；blocked 默认释放 workdir，但固化失败 SHALL 保留 workdir 作为救援现场，且启动期清扫不得清除任何非 completed 交付的 workdir。

#### Scenario: 单次失败即终态

- **WHEN** 一次 agent 调用失败或超时（非 unit_test 命令失败）
- **THEN** 交付立即转 blocked 并产生写明原因的 stage_failed 事件，系统不自动重试，需人工重新驱动

#### Scenario: 固化失败保留现场

- **WHEN** 产物固化（提交 / 推送）失败导致 blocked
- **THEN** workdir 不按常规路径清理而予保留，产出仍在 workdir 内可供人工救援

#### Scenario: 启动清扫保护

- **WHEN** 服务启动执行孤儿 workdir 清扫
- **THEN** 非 completed 状态（含 active / queued / blocked）交付的 workdir 一律保留，仅回收超过保留期且无人认领的目录

## Requirement: 管理交付 workdir 生命周期

系统 SHALL 为每条交付分配唯一 workdir 作为一体化前置资源：启动时获取——项目绑定仓库则浅克隆默认分支并记录 base_commit 作为整条流水线的快照基准，绿地项目仅建目录；workdir 全程共享给所有阶段；终态后延迟清理（默认 30 分钟）；获取失败 SHALL 清除半成品目录以保证可重试。workdir 之间互不阻塞。（workspace/ 包）

#### Scenario: 获取与快照基准

- **WHEN** 一条交付首次被驱动且项目绑定了仓库
- **THEN** 浅克隆该仓库默认分支到 <根目录>/<交付id>，记录克隆时 HEAD 为 base_commit，产生 workspace_ready 事件；此后所有阶段共享同一 workdir 与同一快照

#### Scenario: 绿地项目

- **WHEN** 项目未绑定仓库（绿地）
- **THEN** 仅创建 workdir 目录，base_commit 为空，交付照常推进

#### Scenario: 延迟清理与窗口内重取

- **WHEN** 交付进入终态
- **THEN** workdir 在保留期（默认 30 分钟）后才被清理；保留期内若同一交付被重新获取（重启恢复或重试），该 workdir 不被删除；克隆失败时半成品目录被清除，后续重试从头开始

## Requirement: 固化产物为提交、分支与 PR

到达 code_review 门时系统 SHALL 先固化再审查（审查员看到的是已提交状态）：提交 workdir 内全部变更、推送分支 infera/<交付 id 前 8 位>（强制推送）、并仅在仓库为 github.com 且配置了 token 时创建 PR；PR 创建失败 SHALL NOT 阻断交付。（persist/ 包）

#### Scenario: 固化先于审查

- **WHEN** 交付推进到 code_review 门
- **THEN** 先提交并推送产出分支、创建 PR，落 diff 与 pr 产物并产生 persist_done 事件，然后才运行审查道并挂起人工门

#### Scenario: 零产出与重复固化跳过

- **WHEN** workdir 相对基准无任何变更，或远端同名分支已指向当前提交
- **THEN** 跳过推送（及 PR），不视为错误；分支缺失本身即作为「该子交付无改动」的信号供合并环使用

#### Scenario: PR 失败不阻断

- **WHEN** 推送成功但 PR 创建被 GitHub 拒绝（如已存在同名 PR）
- **THEN** 仅产生 pr_failed 事件，交付继续走审查与门禁；diff 产物超过上限时截断并标注完整内容所在分支

## Requirement: 呈现双道审查意见

code_review 人工门前 SHALL 编排两道独立 agent 审查——规格符合性（有任务清单时逐项核验）与代码质量——解析输出末尾的 infera-findings 结构化块并容错；两道都产出才挂人工门；意见 SHALL 只呈现不参与流转判定。双道之外门禁还有一道前置预审（code_review 角色 agent，先于双道执行并落 agent_output 产物，失败同 agent 失败约定转 blocked；挂起时序见 gates-approvals「挂起交付门禁并按裁定流转」）。（engine/reviews.go）

#### Scenario: 两道产出后挂门

- **WHEN** 两道审查均已运行
- **THEN** 门禁挂起，门禁响应携带两道审查报告（恒定顺序：规格符合性 → 代码质量）与真实代码 diff

#### Scenario: 解析容错

- **WHEN** 某道审查输出缺少结构化块、块内 JSON 畸形或字段越界
- **THEN** 不报错：意见归为空、未知严重度归为提示级、负任务序号归为整体意见，原始输出完整保留供人工兜底阅读

#### Scenario: 某道失败或缺绑定

- **WHEN** 某道审查 agent 失败或该节点缺绑定
- **THEN** 产生写明是哪一道的 stage_failed 事件并转 blocked；若某道绑定到本机交互运行时则跳过该道（本机交互者即审查员），门禁照常挂起、该道显示未产出

## Requirement: 拆分需求并按波次调度

批准并拆分时系统 SHALL 为每条子方案创建子交付并标记波次：父交付跳过 tasks / tasks_approval / test_gen 停在 code_gen 等待合并；同波次子交付并行启动，某波次全部完成且合并后下一波次才启动；子交付按（波次、创建时间）顺序增量合并进父（完成一个合一个，不等齐），合并进度以持久标记留存、重启不丢；拆分要求项目绑定仓库，绿地项目（未绑定仓库）SHALL 拒绝拆分（选项校验见 gates-approvals「门禁裁定选项按门校验」）。（engine/split.go）

#### Scenario: 拆分落库与父停驻

- **WHEN** 设计门以非空拆分方案批准
- **THEN** 父交付标记为拆分模式并停在 code_gen，跳过 tasks / tasks_approval / test_gen；每条子方案各建一条子交付（含标题、描述、波次），波次缺省归一为 1，空标题被拒绝

#### Scenario: 同波并行、跨波串行

- **WHEN** 某波次到达启动条件
- **THEN** 该波次全部排队中的子交付同时转为执行中并各自被驱动（完整流水线并行跑），产生 wave_started 事件；仅当更低波次的子交付全部完成且均已合并后才启动下一波次

#### Scenario: 增量合并与进度持久

- **WHEN** 某条子交付完成
- **THEN** 其分支被合并进父 workdir 并记录持久的合并标记与 merge_done 事件；父对每条子交付只合并一次，重启后依标记续作；全部子交付完成且合并后父写入合并摘要、进入 unit_test 走正常审查与交付

## Requirement: 处理合并冲突与人工恢复

合并子分支发生冲突时系统 SHALL 把父置为 conflict 并给出可复制的本地 git 指引（列出全部相关分支，含已合并的，以免人工重建丢工作）；冲突期间其它子交付 SHALL 继续执行、其完成只入队不合并（每子至多一条入队事件）；人工推送解决分支后经恢复动作继续合并队列。（engine/split.go）

#### Scenario: 冲突停驻与指引

- **WHEN** 合并某条子交付分支产生冲突
- **THEN** 父置为 conflict，产生 merge_conflict 事件，携带冲突子交付标识与标题、全部相关分支名、以及给人工的完整 git 命令文本

#### Scenario: 冲突期间其它子交付继续

- **WHEN** 父处于 conflict 状态时另一条子交付完成
- **THEN** 该子交付照常执行与完成，仅产生一次 merge_queued 事件入队等待，不合并也不阻塞

#### Scenario: 人工恢复与无改动子交付

- **WHEN** 人工解决冲突并推送 infera/<父 id 前 8 位> 分支后触发恢复
- **THEN** 系统校验父确为拆分且处于 conflict，fetch 该分支并硬重置父 workdir、清除冲突、依次合并仍在排队的已完成子交付后继续推进；非拆分或非冲突状态的恢复请求被拒绝。子交付分支在远端缺失时视为无改动，记为已合并并继续

## Requirement: 互斥驱动同一交付

系统 SHALL 以每交付一把互斥锁串行化全部驱动入口（HTTP 面与 MCP 面共用同一实例），读操作 SHALL NOT 加锁；单次驱动内的引擎调用次数有上限以防病态循环；进程重启后恢复全部 active 交付且恢复并发受限。（deliverylock/ 包）

#### Scenario: 并发裁定排队

- **WHEN** 同一交付上两个变更动作（批准 / 打回 / 交回 / 恢复）并发到达
- **THEN** 它们在同一把锁上排队串行执行，后到者等待在途驱动到达停止点（门禁或终态）后再进入；读取该交付详情不受锁影响

#### Scenario: 驱动次数上限

- **WHEN** 一次驱动中交付反复处于可继续状态
- **THEN** 单次驱动最多执行有限次引擎调用后让出，防止病态循环占住锁

#### Scenario: 重启恢复

- **WHEN** 服务进程重启
- **THEN** 全部 active 交付被重新驱动（挂起在门禁的不产生额外引擎调用），恢复并发受上限约束；拆分父若停在合并等待则直接进入合并推进

## Requirement: 停驻本机绑定节点并接受交回

绑定到本机交互（local）运行时的节点 SHALL 停驻：不调用任何 runner，产生一次 local_stage_pending 事件（仅在交付确实移动过后才重播，避免重复打扰），等待外部交回产出；交回按节点契约落产物并前进，不合法交回 SHALL NOT 改变交付状态。（engine/local.go）

#### Scenario: 停驻等待

- **WHEN** 交付进入某绑定 local 运行时的节点
- **THEN** 不执行任何 agent 调用，该阶段记为完成并产生 local_stage_pending 事件，交付停驻；此后仅当交付发生实际移动后才再次播报该事件

#### Scenario: 交回产出

- **WHEN** 外部为停驻中的交付交回产出
- **AND** 停驻点是 local 绑定的 agent 节点
- **THEN** 按该节点契约落产物（任务节点解析清单、实现节点补全任务完成标记等）并前进，产生 local_stage_submitted 事件；若停驻点是 local 绑定的门禁审查角色，则只落审查意见产物，门禁保持挂起由人工裁定

#### Scenario: 拒绝不合法交回

- **WHEN** 交回针对的交付非执行中、停驻节点并非 local 绑定、或为拆分父停驻点
- **THEN** 拒绝该交回且不产生任何状态变更
