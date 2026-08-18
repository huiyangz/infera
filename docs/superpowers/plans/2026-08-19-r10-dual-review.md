# R10 双道审查（规格符合性 + 代码质量）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** code_review 人工门前编排两道独立 agent 审查（spec_conformance / code_quality），产出结构化 findings 呈现给人工——人从「审代码」升级为「审审查意见」。

**Architecture:** 阶段图不动（结构冻结）：`code_review` Node 增加 `FindingsReviews` 元数据字段，step() 挂门前在既有 Persist + ReviewRole 预审之后逐道跑审查。findings 契约 = `store.Finding`/`store.FindingsReport`（agent 输出 ```infera-findings fenced block → 引擎容错解析 → JSON 存 artifact，kind=`<review>_findings`）。存 artifact 而非独立 store 表：`LatestArtifact(kind)` 天然给出「最新一次审查生效」（打回重跑后再审自动覆盖），append-only 保留历史，与 tasks artifact（R8）同一先例；独立表需扩 Store 接口 + pg 迁移，无对应收益。orchestration.BindableNodes 扩两个节点（Resolve/PUT 默认编排即要求全覆盖；升级后旧默认绑定需重 PUT——已知约定）。API gate 响应带 reviews+diff；仅 gate 页消费。

**Tech Stack:** Go（chi, testify, memory store 替身）/ React 19 + TanStack Query + shadcn/ui（pnpm）。

**测试纪律:** 会话专属 pg 测试库（不用共享 infera-postgres-test）；`go test -p 1`；前端 `pnpm build`。

**关键决策（已冻结）:**
- 既有 code_review 预审（ReviewRole→agent_output）保留不动；两道审查是「加」不是「替」。
- 某道审查绑定为 local runner → 跳过该道（本机交互=人工即审查员），门禁照常挂起；缺绑定 / agent 失败 → stage_failed（写明哪道）+ blocked。
- findings 不参与流转判定（只呈现不拦截）；空清单/畸形块容错为空意见，原始输出留档 Raw 字段。

---

### Task 1: findings 契约类型（store）

**Files:**
- Modify: `server/internal/store/store.go`

- [ ] **Step 1: 在 TaskSpec 之后加契约类型 + artifact kind 常量**

```go
// Finding 单条结构化审查意见（R10 双道审查契约，由本文件冻结）：
// 审查 agent 在输出末尾附 ```infera-findings fenced block（JSON 数组），
// 引擎容错解析（无块/坏 JSON → 空意见），报告 JSON 存 findings artifact。
type Finding struct {
	TaskIndex int    `json:"task_index"` // 关联任务序号（1-based；0=整体意见，不关联具体任务）
	Severity  string `json:"severity"`   // critical|major|minor|info（未知值归一为 info）
	Message   string `json:"message"`    // 意见内容（结论+理由）
	Evidence  string `json:"evidence"`   // 证据引用（file:line / 函数名 / 代码片段）
}

// FindingsReport 一道门禁前置审查的结构化产出（findings artifact 的 content 形状）。
type FindingsReport struct {
	Review    string    `json:"review"`     // spec_conformance|code_quality
	TaskBased bool      `json:"task_based"` // 规格符合性是否按任务清单逐项核验
	Findings  []Finding `json:"findings"`   // 结构化意见（空=无意见）
	Raw       string    `json:"raw"`        // agent 原始输出（畸形块时人工兜底阅读）
}

// findings artifact kind 约定（道名 + "_findings"）：引擎落盘与 API/前端读取共用。
const (
	KindSpecConformanceFindings = "spec_conformance_findings"
	KindCodeQualityFindings     = "code_quality_findings"
)
```

- [ ] **Step 2: `go build ./...` 通过**

### Task 2: 审查角色 prompt 模板（agent）

**Files:**
- Modify: `server/internal/agent/runner.go`

- [ ] **Step 1: Prompts 加两个角色 + Role 注释更新**

```go
	"spec_conformance": "你是规格符合性审查员。审查当前仓库中相对基线的全部改动（系统已提交到当前分支，可用 git diff/git log 查看），逐项核验是否实现了规格（以及任务清单，若随 prompt 附上）的要求。\n需求：{description}\n规格：\n{spec}\n只报告「要求与实现的偏差」：未实现 / 部分实现 / 实现与要求不符；不评价代码风格质量（另有代码质量审查）。在最后另起一行附审查意见 fenced block（JSON 数组；完全符合时输出空数组 []）：\n```infera-findings\n[{\"task_index\":1,\"severity\":\"major\",\"message\":\"任务 1 只实现了 X，缺少 Y\",\"evidence\":\"path/file.go:42\"}]\n```\n字段约定：task_index=关联任务序号（1 起；无任务清单或不关联具体任务用 0）；severity=critical（需求未达成）|major（部分实现或与要求不符）|minor（偏差但不影响验收）|info（提示）；message=结论与理由；evidence=证据位置（file:line 或代码引用）。",
	"code_quality": "你是代码质量审查员。审查当前仓库中相对基线的全部改动（系统已提交到当前分支，可用 git diff/git log 查看），从代码质量角度评估：可读性、健壮性、错误处理、边界条件、测试覆盖、安全与性能。不评估功能是否符合规格（另有规格符合性审查）。\n需求：{description}\n规格：\n{spec}\n在最后另起一行附审查意见 fenced block（JSON 数组；无质量问题输出空数组 []）：\n```infera-findings\n[{\"task_index\":0,\"severity\":\"minor\",\"message\":\"缺少空指针防护\",\"evidence\":\"path/file.go:17\"}]\n```\n字段约定：task_index=0（质量意见不关联具体任务）；severity=critical（会引发故障或安全问题）|major（明显缺陷）|minor（可改进）|info（提示）；message=结论与理由；evidence=证据位置（file:line 或代码引用）。",
```

Request.Role 注释追加 `| spec_conformance | code_quality`。

- [ ] **Step 2: `go test ./internal/agent/` 通过**

### Task 3: 引擎双道审查（reviews.go + graph/step 接线）

**Files:**
- Create: `server/internal/engine/reviews.go`
- Modify: `server/internal/engine/graph.go`（Node.FindingsReviews 字段 + code_review 节点）
- Modify: `server/internal/engine/engine.go`（step() KindGate 分支）
- Test: `server/internal/engine/reviews_test.go`

- [ ] **Step 1: 写失败测试**（findingsRunner 替身 + TestParseFindingsBlock 表驱动 + TestDualReviewsAtCodeReviewGate / TestSpecConformanceTaskBased / TestDualReviewMissingBindingBlocks / TestDualReviewAgentFailureBlocks / TestDualReviewLocalRunnerSkips —— 完整代码见实现，断言要点：两道齐全挂门+artifact+review_findings 事件；large 路径 sc 的 prompt 含编号任务清单且 report task_based=true；缺绑定/失败 → blocked+stage_failed 写明道名；local → 跳过该道门禁照常挂）
- [ ] **Step 2: 跑测试确认失败**（compile error / 断言失败）
- [ ] **Step 3: 实现**（graph.go Node 加 `FindingsReviews []string`，code_review 节点配 `[]string{"spec_conformance", "code_quality"}`；reviews.go：`findingsRe`/`ParseFindingsBlock`（容错同 tasks：无块/坏 JSON→nil，空消息过滤，severity 未知→info，负 index→0）/`stepFindingsReviews` 循环/`stepFindingsReview`（解析 runner：local→跳过+local_stage_pending，错误→stage_failed 写明道名+blocked；跑 agent 失败同约定；成功→report JSON 落 `<review>_findings` artifact + review_findings 事件）/`reviewPrompt`（BuildPrompt + spec_conformance 注入编号任务清单，返回 taskBased）；engine.go step() 在 stepGateReview 后加 FindingsReviews 分支）
- [ ] **Step 4: 跑包测试 + 修既有角色断言**（engine_test.go:216、stages_test.go:103、tasks_test.go:97、bindings_test.go:45/133 的 roles 追加 `"spec_conformance", "code_quality"`）
- [ ] **Step 5: Commit**

### Task 4: BindableNodes 扩展（orchestration）

**Files:**
- Modify: `server/internal/orchestration/orchestration.go`
- Test: 既有 orchestration/api 编排测试按需补两个节点

- [ ] **Step 1:** `BindableNodes = []string{"spec", "test_gen", "code_gen", "code_review", "spec_conformance", "code_quality"}` + 注释（升级加节点后旧默认绑定需重 PUT）。跑 orchestration + api 包测试，修 TestResolve*/TestDefaultPipelinePut 等的节点清单。main.go seed 日志文案补双道。
- [ ] **Step 2: Commit**

### Task 5: API 门禁响应（reviews + diff）

**Files:**
- Modify: `server/internal/api/deliveries.go`
- Test: `server/internal/api/api_test.go`

- [ ] **Step 1: 失败测试** TestGateCodeReviewReturnsReviewsAndDiff（seedGate code_review + diff/spec_conformance_findings 产物 → GET gate：diff 全文、reviews[0].present+findings+task_based、reviews[1] absent）
- [ ] **Step 2: 实现** handleGate 循环顺带取最新 diff；code_review case：`resp["diff"]`、`resp["reviews"]=gateReviews(arts)`（gateReview{review,present,task_based,artifact_id,findings,raw}，kind=`review+"_findings"`，坏 JSON→raw 兜底）
- [ ] **Step 3: 包测试通过 + Commit**

### Task 6: 前端类型 + 门禁页呈现

**Files:**
- Modify: `apps/web/src/lib/infera-types.ts`（Finding/GateReview/GateInfo.diff/reviews + STAGE_META 两节点 label）
- Modify: `apps/web/src/features/deliveries/gate.tsx`（仅 code_review 分支：审查意见×2 与 diff 并列 grid；severity 单色 badge 分级；任务#N chip；evidence chip→PR 跳转；raw details 折叠；容器 code_review 加宽 max-w-6xl）

- [ ] **Step 1:** types + api client 无需改（getGate 返回泛型 GateInfo）
- [ ] **Step 2:** gate.tsx 实现 ReviewCard（REVIEW_META/SEVERITY_META 单色：critical=bg-primary、major=bg-secondary、minor=outline、info=muted）
- [ ] **Step 3:** `pnpm build` 通过
- [ ] **Step 4:** 手测说明（docs：本地起服 → seed/绑定 → 造 delivery 到 code_review 门 → 断言要点）
- [ ] **Step 5: Commit**

### Task 7: e2e + 全量验证

- [ ] bindings_e2e_test.go PUT 绑定补两节点；会话专属库跑 `server/test`（`go test -p 1`）；`go test ./...` 全绿；`pnpm build` 绿
- [ ] Commit（单 attempt commit，由编排者合并）

---

## Self-Review

- 规格覆盖：引擎编排✓ findings 契约（artifact 方案+理由）✓ BindableNodes✓ API reviews/内容✓ 门禁页呈现（仅此页）✓ 两道齐全挂门/缺道失败 blocked+事件✓ tasks artifact 逐项核验、无则整体✓ 不自动拦截✓ 阶段图结构/MCP/其他页面不动✓（例外：EVENT_LABEL 一行补新事件中文标签、STAGE_META 两行补节点 label——新契约的最小配套，非页面改动）
- 类型一致性：kind 命名统一 `<review>_findings`（store 常量 = engine map = api 拼接）；Finding 字段跨端对齐。
- 已知升级注记：旧默认绑定缺两新节点 → 交付在任一 agent 阶段 blocked（既有「全覆盖」约定），重 PUT pipeline 即恢复。
