# R10 双道审查：规格符合性 + 代码质量（code_review 人工门前置）

code_review 人工门前编排两道独立 agent 审查，结构化意见呈现给人工——人从「审代码」升级为「审审查意见」。阶段图结构不动：`code_review` Node 只增加 `FindingsReviews` 元数据（既有 ReviewRole 预审与 Persist 固化保留）。

## findings 契约（本任务定义并冻结）

审查 agent 在输出末尾附 fenced block（与 infera-complexity / infera-split / infera-tasks 同一协议家族）：

~~~
```infera-findings
[{"task_index":1,"severity":"major","message":"结论与理由","evidence":"path/file.go:42"}]
```
~~~

| 字段 | 语义 |
|---|---|
| `task_index` | 关联任务序号（1-based；`0` = 整体意见，不关联具体任务） |
| `severity` | `critical` \| `major` \| `minor` \| `info`（未知值引擎归一为 `info`） |
| `message` | 意见内容（结论 + 理由） |
| `evidence` | 证据引用（`file:line` / 函数名 / 代码片段） |

引擎解析容错（畸形输出不崩）：无块 / 坏 JSON / 非数组 → 空意见；空消息条目过滤；负 `task_index` 归一为 `0`。原始输出完整留档在报告 `raw` 字段供人工兜底阅读。

报告形状（`store.FindingsReport`，api/engine/前端三方共用）：

```json
{"review":"spec_conformance","task_based":true,"findings":[…],"raw":"agent 原始输出"}
```

**存储决策：artifact，不建独立 store 表。** kind = `<道名>_findings`（`store.KindSpecConformanceFindings` / `store.KindCodeQualityFindings`，stage=code_review）。理由：`LatestArtifact(kind)` 天然给出「最新一次审查生效」——打回重跑后再审自动覆盖展示，append-only 保留全部历史审计；与 tasks artifact（R8）同一存储先例；独立表需扩 Store 接口 + pg 迁移 + memory/pg 双实现，无对应收益。两道 kind 独立，互不覆盖。

## 编排与流转语义

- `orchestration.BindableNodes` 扩为 6 节点：`spec / test_gen / code_gen / code_review / spec_conformance / code_quality`。默认编排 PUT 与 Resolve 照旧要求全覆盖。
- 挂门条件：两道**都产出**才挂人工门。缺绑定 / 某道 agent 失败 → `stage_failed`（payload 写明哪道）+ `blocked`。
- 某道绑定 local runner → 跳过该道（本机交互 = 人工即审查员，`local_stage_pending` 事件），门禁照常挂起，门禁页该道显示「未产出」。
- 规格符合性审查：有 tasks artifact（非空清单）→ prompt 注入编号任务清单逐项核验（`task_based=true`）；无 → 按规格整体核验。代码质量意见不关联任务（`task_index=0`）。
- **意见只呈现不拦截**：findings 不参与流转判定，批准 / 打回由人工决定（零意见也不自动放行）。
- 事件：每道产出 `review_findings` `{review, count, task_based}`；顺序 `persist_done → review_findings×2 → gate_pending`。
- API：`GET /api/deliveries/{id}/gate` 在 code_review 门附 `diff`（真 diff 全文）与 `reviews`（两道，恒定顺序 spec_conformance → code_quality；`present=false` = 未产出；坏 JSON 容错为原文 `raw`）。

**升级注记**：旧默认绑定缺两个新节点 → 交付在 agent 阶段按既有「全覆盖」约定 blocked（`stage_failed` 写明缺哪些）；重 PUT `/api/pipeline` 补齐即恢复。全新安装由 seed 自动绑定全部 6 节点。

## code_review 门页手测说明

前端仅改 code_review 门页（`apps/web/src/features/deliveries/gate.tsx`）：两道审查意见卡片与代码 diff 并列（xl 双栏，容器加宽至 max-w-6xl）；严重度单色分级 badge（严重=黑底 / 重要=灰底 / 轻微=描边 / 提示=弱化，遵循 DESIGN.md 无信号色约定）；`任务 #N` chip 标关联任务；证据引用为 mono 虚线下划线，有 PR 时点击跳转 PR；「原始输出」折叠兜底。

手工验证步骤（echo 假 agent 即可，~5 分钟）：

1. `docker compose up -d`（postgres）；准备假 agent 脚本（`spec` 角色输出规格 + `infera-complexity` 块；`spec_conformance` / `code_quality` 角色各输出一段结论文本 + `infera-findings` 块，见下方样例）。
2. `AGENT_CMD=/path/to/agent.sh ./run-dev.sh`（全新库自动 seed 6 节点默认绑定，日志出现「双道审查→default-cli」）。
3. 建项目（绑本地裸仓库）→ 提交需求 → spec 门批准（默认 small）。
4. 流水线停在 code_review 门 → 打开门禁页，核对：
   - 「规格符合性审查」「代码质量审查」两张卡片各显示 N 条意见：严重度 badge、消息、证据引用（mono、可跳 PR）；「原始输出」可展开；
   - 右侧「代码 diff」为 Persist 落盘的真 diff（无改动则为空）；
   - 顶部「Reviewer 意见」（既有预审 agent_output）与 PR 链接仍在；
   - 意见为零时卡片显示「未发现规格偏差 / 未发现质量问题」；
5. 把某道 agent 输出改成无 fenced block 的纯文本 → 门禁照常挂起，findings 为空、原始输出可见（容错）。
6. 删掉默认编排里 `spec_conformance` 绑定（PUT 只给 5 节点会被拒——需直接改库或用 API 之外的方式制造缺绑定；单测已覆盖该路径）→ 新交付 blocked，时间线 `stage_failed` 写明缺 `spec_conformance`。

假 agent 样例（两道审查输出）：

```sh
spec_conformance) printf '逐项核验：实现与规格一致。\n\n```infera-findings\n[{"task_index":0,"severity":"minor","message":"未覆盖负数输入","evidence":"add.go:12"}]\n```\n' ;;
code_quality)     printf '质量核验：一处命名建议。\n\n```infera-findings\n[{"task_index":0,"severity":"info","message":"变量名可更语义化","evidence":"add.go:8"}]\n```\n' ;;
```

> 本地全栈冒烟已按上述 1-5 步跑通（2026-08-19，会话库）：gate 响应 `reviews` 两道 `present=true`、findings 解析正确、事件序 `persist_done → review_findings×2 → gate_pending`。
