# 需求流转（infera 作为唯一前台）

INFERA-11：用户只在 infera 里工作——发起需求、审批计划、处理异常、终审合并；
Multica（执行编排）与 GitHub（PR/评审）全部隐身为后台基础设施，仅保留深链
逃生口。infera 侧不内嵌任何 AI：状态机、协议解析、卡片动作全部是确定性代码。

## 大节点状态机（单一状态源）

每个需求在 infera 侧只有一个业务大节点（`requirements.node`），由闸门轮询器按
Multica 父 issue 状态推进；infera 是状态源，Multica 只承载执行态：

| infera 节点 | Multica 父 issue 状态 | 说明 |
|---|---|---|
| `intake` 需求受理 | （未建卡） | 本地行，未派发 |
| `dispatched` 已派发 | `todo` | 已建父 issue 并指派 Tech Lead |
| `in_progress` 执行中 | `in_progress` | |
| `in_review` 待验收 | `in_review` | |
| `delivered` 已交付 | `done` | 终态（自动合并档位可直达） |
| `needs_decision` ⚠️ 需决策 | （异常节点） | 闸门检测触发，状态推进挂起 |

规则：主链只进不退、允许跨级；Multica 侧状态回退不导致节点回退。`needs_decision`
由「需要决策：」评论进入，停驻期间 Multica 状态推进挂起，只经用户决策动作离开
（重试/跳过/自定义 → 回执行中；中止 → 直达已交付）。

## 闸门协议（冻结）

轮询器增量拉取父 issue 评论，按**顶格、区分大小写、不做 trim** 的固定前缀识别
三类闸门，识别不了的评论一律落兜底「有新动态」卡：

| 前缀 | 卡片 | 用户动作（infera 代发到 Multica） |
|---|---|---|
| `待批准：`（全角冒号） | 审批卡 | 批准 → `approved`；驳回并反馈 → 文本原样代发 |
| `需要决策：`（全角冒号） | 决策卡 | 重试/跳过/中止（固定文本）/自定义回复 |
| `verdict: PASS\|FAIL`（ASCII 冒号） | 合并卡 | 合并（infera 经 gh API 合 PR）/ 拒绝并返工（反馈代发） |
| （无上述前缀） | 「有新动态」兜底卡 | 查看深链 |

半角/全角冒号互换等相近变体刻意不命中（防 agent 格式跑偏伪装成闸门）。另有
兜底规则二：父 issue 跃入 `in_review` 但从未见过 verdict 评论 → 弹中性
「有新动态」卡（防漏合并闸门）。

## 合并卡的评审渲染（GitHub 隐身，FR-4/FR-7）

合并卡除 verdict 正文外，还渲染 PR 的行级评审评论（path/line/side/author/
body）与 diff 概要（文件数与 +/- 行数），数据经只读端点
`GET /api/requirements/{id}/pr-review` 由后端经 gh API 拉取（T09 加法扩展，
不改既有路由形状）。端点为纯读：不落卡、不落审计、不动大节点；需求缺 PR
关联返回 409，github 故障 502，未装配需求服务 503——均沿用既有错误码约定。

## 配置项

`.env`（模板见 [.env.example](../.env.example)）。**六键全部留空 = 未接入**
（需求 API 返回 503、闸门轮询器不启动）；**任一键出现即视为尝试接入**，缺项在
启动期由构造器显式报错，不做静默降级：

| 键 | 说明 |
|---|---|
| `MULTICA_SERVER_URL` | 本地 Multica 地址（如 `http://localhost:8088`）。必须显式给出；指向云端 `multica.ai` 视为误配，构造期报错 |
| `MULTICA_TOKEN` | 服务 token（`mul_*` 用户 / `mat_*` agent 均可）。代发评论、状态轮询、增量评论拉取都以这个身份执行 |
| `MULTICA_WORKSPACE_ID` | workspace id（每个请求注入 `X-Workspace-Id` 头） |
| `MULTICA_PROJECT_ID` | 派发目标 Multica 项目 id（父 issue 固定归属） |
| `MULTICA_TECH_LEAD_AGENT_ID` | 派发指派的 Tech Lead agent id（指派即置 `todo` 唤醒） |
| `MULTICA_WORKSPACE_SLUG` | workspace slug（深链工作区段，如 `infera`） |
| `GITHUB_TOKEN` | 合并动作的 GitHub PAT（与仓库导入共用；接入流转时必填） |
| `GITHUB_API_URL` | 可选，GitHub Enterprise API 入口覆盖 |
| `GATE_POLL_INTERVAL` | 闸门轮询间隔。默认 30s；(0, 60s] 之外报错——状态变化须 2 分钟内反映 |

装配落点：`server/cmd/infera/flow.go`（`assembleFlow`）。multica/github 薄
client、`reqservice`（聚合根：派发/读取/代理动作/审计/策略设置）与 `gatepoll`
（轮询器，`SettingsPolicy` 读表解析合并策略）的构造契约以各包代码为准。

## 合并策略三档（项目级，`project_settings` 表）

`PUT /api/projects/{id}/merge-policy` 设档（`GET` 读档；未设置 = 手动档）：

| 档位 | 语义 |
|---|---|
| `manual`（默认） | 合并卡出现，人点合并。自动合并完全不动作 |
| `auto_pass` | Reviewer verdict PASS 的合并卡立即自动合并（gh API），节点直达已交付，审计记 `actor=system` |
| `threshold` + `diff_line_threshold` | PR diff 行数（additions+deletions）≤ 阈值自动合并，超过弹卡留人 |

部署级单行语义：本期单租户，`SettingsPolicy` 取表中唯一一行作为部署策略；
读不出有效策略（无行 / 直写 SQL 绕过校验的损坏档位）一律回落手动档——自动
合并是风险动作，数据异常时选最保守的行为。合并被 GitHub 拒绝（CI 未过等
「暂不可合并」类）保持卡 pending，下一轮自然重试；硬失败转人工（合并卡本身
就是人的逃生口）。

## 深链逃生口

每张卡与需求行都带 Multica 深链（`multica_issue_url`），形如
`{MULTICA_SERVER_URL}/{MULTICA_WORKSPACE_SLUG}/issues/{issue-id}`；合并卡另带
GitHub PR 深链（`pr_url`）。默认不打扰，排查时一键直达完整时间线。

## 运维注意

- **轮询器启停**：随 server 进程生命周期（启动即先跑一轮，再按
  `GATE_POLL_INTERVAL` 周期执行）。SIGINT/SIGTERM 优雅停止：HTTP 先排空在途
  请求，轮询器等在途一轮收口后退出，连接池最后关闭。轮询单需求失败不阻断
  其余需求，错误进日志、下一轮重试。
- **审计表**：`audit_log` 只增不改——代理动作（approve/reject/decide/rework/
  merge）记 `actor=user`，自动合并记 `actor=system`。查询走
  `GET /api/requirements/{id}/audit`。删除带审计历史的需求会因 FK 报错（刻意：
  不悄悄抹掉轨迹）。
- **重启恢复**：轮询游标（增量评论位置、上次状态、是否见过 verdict）持久化在
  `requirements` 行上，重启续读不重放；自动合并成功后的崩溃窗口由「pending
  合并卡每轮清扫 + PR 已 closed 且 merged=true 收敛」兜住。closed 但未合并
  （被驳回关闭）不是已了结：卡保持待处理转人工，不误置已交付、不误记
  merge 审计。
- **数据表**：`requirements` / `gate_cards` / `project_settings`（migration
  0007）；`audit_log` 见上。

## 端到端实测（本地环境）

`server/test/flow_e2e_test.go` 对本地环境实测 AC-1～AC-4（零跳出全程、闸门
不漏含兜底卡与决策节点进出、状态 2 分钟内映射、三档合并策略各验一遍）。
门禁与环境变量说明见该文件头注释；环境缺失时显式 skip，裸 `go test ./...`
保持绿。合并动作用仓库内一次性测试分支/PR（标题带 `test/e2e` 标识，验完
关闭），全部经 GitHub API。
