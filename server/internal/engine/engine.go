package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/persist"
	"github.com/tokfinity/infera/internal/store"
)

// Workspace 是引擎对 workspace.Manager 的最小依赖（便于测试替身）。
type Workspace interface {
	Acquire(ctx context.Context, deliveryID, repoURL, branch string) (string, string, error)
	Path(deliveryID string) string
	Release(deliveryID string)
}

// TestRunner 是 unit_test 命令节点的执行器（在 workdir 里跑仓库测试）。
type TestRunner interface {
	RunTests(ctx context.Context, workdir string) (pass bool, output string, err error)
}

// delivery 状态。
const (
	StatusActive    = "active"
	StatusCompleted = "completed"
	StatusBlocked   = "blocked"
)

// errStop 是内部信号：本轮推进到此为止（门禁暂停 / 失败回环 / 终态），不是错误。
var errStop = errors.New("engine: stop this run")

// parentDriveTimeout 子需求完成后的异步父推进（合并循环）时间上限。
const parentDriveTimeout = 30 * time.Minute

// agentRunTimeout 单次 agent 调用的总时限：CLI 卡死（等输入）/ 远端挂起时，
// 交付不能无限占用 per-delivery 驱动。var 而非 const：测试注入短时限。
var agentRunTimeout = 30 * time.Minute

// runAgent 带总超时跑一次 agent（全部引擎侧 agent 调用的唯一出口）。
// 超时按 agent 失败约定收场：错误包装 timeout 说明（stage_failed 事件可区分），
// 调用方走 agentFailed → blocked。ctx 截止而 runner 返回的是派生错误
// （如被杀进程的 "signal: killed"）也识别为超时。
func runAgent(ctx context.Context, ar agent.Runner, req agent.Request) (agent.Result, error) {
	actx, cancel := context.WithTimeout(ctx, agentRunTimeout)
	defer cancel()
	res, err := ar.Run(actx, req)
	if err != nil && actx.Err() != nil && errors.Is(actx.Err(), context.DeadlineExceeded) {
		return res, fmt.Errorf("agent run timeout (>%s): %w", agentRunTimeout, err)
	}
	return res, err
}

// Engine 阶段图执行器。只认识 Graph 的节点类型与下一跳，不认识具体业务。
type Engine struct {
	st store.Store
	ar agent.Runner
	ws Workspace
	tr TestRunner

	// ps 可选：产出固化器（Persist=true 的门禁到达时 commit/push/PR + diff artifact）。
	// nil = 不固化（旧测试行为）。
	ps persist.Persister

	// g 合并用 git 实例（拆分父增量 merge / 冲突恢复）。
	g *git.Git

	// OnStartDelivery 可选：子需求批次启动时点火回调（api 层 spawn driver）。
	OnStartDelivery func(deliveryID string)

	// parentLocks per-parent 互斥（split 父的合并与批次调度串行化）。
	parentLocks sync.Map

	// Notify 可选：事件发生时回调（WS 推送，main 组装注入）。
	Notify func(deliveryID, stage, eventType string)

	// ResolveRunner 可选：按项目+节点解析执行器（agent 编排绑定）。
	// 未设置 / 返回 (nil,nil) → 回退构造时的 ar（向后兼容）；
	// 返回 ErrLocalRunner → 本机交互占位：停车在该节点（local_stage_pending 事件）；
	// 其余错误（绑定缺失等）→ stage_failed + blocked。
	ResolveRunner func(ctx context.Context, projectID, node string) (agent.Runner, error)
}

func New(st store.Store, ar agent.Runner, ws Workspace, tr TestRunner) *Engine {
	return &Engine{st: st, ar: ar, ws: ws, tr: tr, g: git.New()}
}

// WithGit 注入合并用 git 实例（带 token；默认无 token 的裸实例）。
func (e *Engine) WithGit(g *git.Git) *Engine {
	e.g = g
	return e
}

// WithPersister 注入产出固化器。
func (e *Engine) WithPersister(p persist.Persister) *Engine {
	e.ps = p
	return e
}

// Start 驱动 delivery 从当前阶段前进，直到门禁 / 失败回环 / 终态 / 错误。
// 仓库是前置资源：首次调用负责 Acquire（clone + 记 base_commit），全程共享同一 workdir。
func (e *Engine) Start(ctx context.Context, deliveryID string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if d.Status != StatusActive {
		return fmt.Errorf("engine: delivery %s is %s, not active", d.ID, d.Status)
	}
	if err := e.ensureWorkspace(ctx, d); err != nil {
		return err
	}
	return e.run(ctx, d)
}

// Approve 通过当前人工门禁（唯一入口）：只做门禁簿记（清 gate、按门分发选项、推进、
// 落盘）后立即返回，不驱动 agent——后续推进由调用方异步 Continue（API 层后台 goroutine）。
// opts 按当前门校验：spec_approval 裁定 Complexity（空=取 spec 的 infera-complexity 建议，
// 再空=small，老数据 ” 同语义）；design_approval 非空 Split=「批准并拆分」（见 approveSplit）；
// tasks_approval 非空 Tasks=「批准并覆盖任务清单」（见 approveTasksOverride）。
// 选项给错门 / 取值非法 → 报错且不消费门禁。拆分批准返回创建的子需求，其余返回 nil。
func (e *Engine) Approve(ctx context.Context, deliveryID string, opts store.ApproveOpts) ([]store.Delivery, error) {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	if d.PendingGate == "" {
		return nil, fmt.Errorf("engine: delivery %s has no pending gate", d.ID)
	}
	node, ok := Graph[d.PendingGate]
	if !ok {
		return nil, fmt.Errorf("engine: unknown gate stage %q", d.PendingGate)
	}
	if len(opts.Split) > 0 {
		return e.approveSplit(ctx, d, opts.Split)
	}
	if len(opts.Tasks) > 0 {
		if err := e.approveTasksOverride(ctx, d, opts.Tasks); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if opts.Complexity != "" {
		if d.PendingGate != "spec_approval" {
			return nil, fmt.Errorf("engine: complexity is only allowed at spec_approval, not %q", d.PendingGate)
		}
		if opts.Complexity != ComplexitySmall && opts.Complexity != ComplexityLarge {
			return nil, fmt.Errorf("engine: invalid complexity %q (want small|large)", opts.Complexity)
		}
		d.Complexity = opts.Complexity
	} else if d.PendingGate == "spec_approval" {
		c, err := e.suggestedComplexity(ctx, d)
		if err != nil {
			return nil, err
		}
		d.Complexity = c
	}
	gate := d.PendingGate
	next := node.Next
	if gate == "spec_approval" {
		next = nextAfterGate(d, node)
	}
	if err := e.approveGate(ctx, d, next); err != nil {
		return nil, err
	}
	if gate == "spec_approval" {
		e.emit(ctx, d, gate, "complexity_set", map[string]string{"complexity": d.Complexity})
	}
	return nil, nil
}

// suggestedComplexity spec_approval 无人工裁定时的复杂度裁定：
// spec 末尾的 infera-complexity fenced block 给建议；无/坏块默认 small。
func (e *Engine) suggestedComplexity(ctx context.Context, d *store.Delivery) (string, error) {
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return "", err
	}
	if c := ParseComplexitySuggestion(spec); c != "" {
		return c, nil
	}
	return ComplexitySmall, nil
}

// complexityRe 从 spec 全文里提取 ```infera-complexity fenced block（AI 的复杂度建议）。
var complexityRe = regexp.MustCompile("```infera-complexity\\n([\\s\\S]*?)\\n```")

// ParseComplexitySuggestion 解析 spec 文本里的复杂度建议（small|large）；无/坏块返回空串。
func ParseComplexitySuggestion(spec string) string {
	m := complexityRe.FindStringSubmatch(spec)
	if m == nil {
		return ""
	}
	if c := strings.TrimSpace(m[1]); c == ComplexitySmall || c == ComplexityLarge {
		return c
	}
	return ""
}

// Continue 从当前状态推进到下一个停车点（门禁 / 失败回环 / 终态 / 错误）。
// 与 Start 的区别：不重复 ensureWorkspace（Approve/Reject 后的重新点火场景）。
// 已停在门禁或非 active 时是安全 no-op（非 active 报错，与 Start 一致）。
func (e *Engine) Continue(ctx context.Context, deliveryID string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if d.Status != StatusActive {
		return fmt.Errorf("engine: delivery %s is %s, not active", d.ID, d.Status)
	}
	return e.run(ctx, d)
}

// Reject 驳回当前人工门禁：按节点 RejectTo 回退并记录驳回意见，停车不重跑。
// 重跑由下一次 Start/Continue 驱动——stageFeedback 会把驳回意见注入回退阶段首轮 prompt（消费一次后清空）。
func (e *Engine) Reject(ctx context.Context, deliveryID, reason string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if d.PendingGate == "" {
		return fmt.Errorf("engine: delivery %s has no pending gate", d.ID)
	}
	node, ok := Graph[d.PendingGate]
	if !ok {
		return fmt.Errorf("engine: unknown gate stage %q", d.PendingGate)
	}
	if node.RejectTo == "" {
		return fmt.Errorf("engine: no reject target for gate %q", d.PendingGate)
	}
	gate := d.PendingGate
	d.PendingGate = ""
	d.CurrentStage = node.RejectTo
	d.RejectReason = reason
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	e.finishLatestRun(ctx, d.ID, gate, "failed")
	e.emit(ctx, d, gate, "gate_rejected", map[string]string{"reason": reason})
	return nil
}

// --- 内部推进 ---

// ensureWorkspace 首次进入时 Acquire 仓库并持久化（WorkspaceReady 幂等标志）。
// 绿地项目 base_commit 为空，不能用 BaseCommit 判重——否则每次 Start 都重复 Acquire。
// 事件记 d.CurrentStage：普通交付首次进入时是 intake，拆分父在 mergeLoop 里
// 补 Acquire 时是 code_gen——固定记 intake 会把 mergeLoop 的失败标错阶段。
func (e *Engine) ensureWorkspace(ctx context.Context, d *store.Delivery) error {
	if d.WorkspaceReady {
		return nil
	}
	proj, err := e.st.GetProject(ctx, d.ProjectID)
	if err != nil {
		return err
	}
	_, base, err := e.ws.Acquire(ctx, d.ID, proj.RepoURL, proj.DefaultBranch)
	if err != nil {
		// workspace 获取失败同 agent 失败约定：记 stage_failed 事件 + blocked 终态，
		// 错误上抛（调用方决定记日志），避免 delivery 无痕迹卡死。
		e.emit(ctx, d, d.CurrentStage, "stage_failed", map[string]string{"error": err.Error()})
		return e.block(ctx, d, fmt.Errorf("workspace acquire: %w", err))
	}
	d.BaseCommit = base
	d.WorkspaceReady = true
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	e.emit(ctx, d, d.CurrentStage, "workspace_ready", map[string]string{"base_commit": base})
	return nil
}

// run 推进直到停止条件（门禁 / 回环 / 终态 / 错误）。
// 拆分父停在 code_gen 是"等子需求 / 合并"语义，不是 agent 执行：
// 路由到 MaybeDriveParent（幂等——mergeLoop 的 durable 合并标记保证不重复合并），
// 防止重启恢复 / approve 后的重点火误跑 code_gen AGENT 节点。
func (e *Engine) run(ctx context.Context, d *store.Delivery) error {
	if d.SplitMode && d.CurrentStage == "code_gen" && d.Status == StatusActive {
		return e.MaybeDriveParent(ctx, d.ID)
	}
	for d.Status == StatusActive && d.PendingGate == "" {
		if err := e.step(ctx, d); err != nil {
			if errors.Is(err, errStop) {
				return nil
			}
			return err
		}
	}
	return nil
}

// step 执行当前阶段的一次推进。
func (e *Engine) step(ctx context.Context, d *store.Delivery) error {
	node, ok := Graph[d.CurrentStage]
	if !ok {
		return e.block(ctx, d, fmt.Errorf("engine: unknown stage %q", d.CurrentStage))
	}
	run, err := e.startStageRun(ctx, d, node.Stage)
	if err != nil {
		return err
	}
	switch node.Kind {
	case KindAgent:
		return e.stepAgent(ctx, d, node, run)
	case KindGate:
		// 固化先于预审：审查员看到的是已提交状态，diff/pr artifact 也就位。
		if node.Persist && e.ps != nil {
			if err := e.persistAtGate(ctx, d, node, run); err != nil {
				return err
			}
		}
		if node.ReviewRole != "" {
			// 门禁前置 agent（code_review）：先预审产出 agent_output artifact，
			// 门禁页才有内容可审；失败同 agent 失败约定 blocked。
			if err := e.stepGateReview(ctx, d, node, run); err != nil {
				return err
			}
		}
		if len(node.FindingsReviews) > 0 {
			// R10 双道审查：结构化 findings 全部产出后门禁才挂起（缺道/失败 blocked）。
			if err := e.stepFindingsReviews(ctx, d, node, run); err != nil {
				return err
			}
		}
		d.PendingGate = node.Stage
		if err := e.st.UpdateDelivery(ctx, d); err != nil {
			return err
		}
		e.emit(ctx, d, node.Stage, "gate_pending", nil)
		return errStop
	case KindCommand:
		switch node.Stage {
		case "intake":
			// workspace 已在 Start 里 Acquire；intake 只是流程标志，直接放行。
			e.finishStageRun(ctx, run.ID, "done")
			return e.advance(ctx, d, node.Next)
		case "unit_test":
			return e.stepUnitTest(ctx, d, node, run)
		default:
			return e.block(ctx, d, fmt.Errorf("engine: unhandled command stage %q", node.Stage))
		}
	default:
		return e.block(ctx, d, fmt.Errorf("engine: unknown node kind %v", node.Kind))
	}
}

// runnerFor 按项目+节点解析执行器：ResolveRunner 未设置/返回 (nil,nil) 时回退构造时的 ar。
func (e *Engine) runnerFor(ctx context.Context, d *store.Delivery, node string) (agent.Runner, error) {
	if e.ResolveRunner == nil {
		return e.ar, nil
	}
	r, err := e.ResolveRunner(ctx, d.ProjectID, node)
	if err != nil || r != nil {
		return r, err
	}
	return e.ar, nil
}

// stepAgent 跑一个 agent 节点：带 spec + 上一轮反馈组 prompt、存产物、走 Next。
// 节点绑定为 local runner（本机交互占位）时不跑 agent：停车发 local_stage_pending。
func (e *Engine) stepAgent(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	ar, err := e.runnerFor(ctx, d, node.Stage)
	if err != nil {
		// 绑定缺失 / agent 未知 → blocked（数据面错误，重试无意义）
		if !errors.Is(err, orchestration.ErrLocalRunner) {
			e.finishStageRun(ctx, run.ID, "failed")
			e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{"error": err.Error()})
			return e.block(ctx, d, fmt.Errorf("stage %s: %w", node.Stage, err))
		}
		// local 占位：交付停在当前节点等本机交互（MCP / infera-link 通道）。
		// 事件幂等：见 emitLocalPending（重驱动不刷屏，新停车照常广播）。
		e.finishStageRun(ctx, run.ID, "done")
		e.emitLocalPending(ctx, d, node.Stage, node.Stage)
		return errStop
	}
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return err
	}
	feedback, err := e.stageFeedback(ctx, d, node.Stage)
	if err != nil {
		return err
	}
	if node.Stage == "code_gen" {
		// code_gen 有任务清单时逐任务实现（无则单次整体，见 stepCodeGen）。
		return e.stepCodeGen(ctx, d, node, run, ar, spec, feedback)
	}
	prompt := agent.BuildPrompt(node.Stage, d.Description, spec, feedback)
	addenda, err := e.promptAddenda(ctx, d, node.Stage)
	if err != nil {
		return err
	}
	res, err := runAgent(ctx, ar, agent.Request{
		Role:    node.Stage,
		Prompt:  prompt + addenda,
		Workdir: e.ws.Path(d.ID),
	})
	if err != nil {
		return e.agentFailed(ctx, d, node, run, err)
	}
	if node.Stage == "tasks" {
		// tasks 节点：产出先解析为清单 JSON 再落盘（畸形块容错为空清单）。
		if err := e.stepTasksAgent(ctx, d, res); err != nil {
			return err
		}
	} else if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       node.ArtifactKind,
		Content:    res.Output,
	}); err != nil {
		return err
	}
	e.finishStageRun(ctx, run.ID, "done")
	return e.advance(ctx, d, node.Next)
}

// persistAtGate 在 Persist=true 的门禁到达时固化产出（commit/push/PR）：
// 真 diff / pr 地址落为 artifact，审查页直接可用。
// commit/push 失败 = 数据安全事件：blocked 且不释放 workdir（人工救援），
// 产出只存在于 workdir，不能按常规路径延迟清理掉。
func (e *Engine) persistAtGate(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	proj, err := e.st.GetProject(ctx, d.ProjectID)
	if err != nil {
		return err
	}
	res, err := e.ps.Persist(ctx, persist.Input{
		DeliveryID: d.ID,
		RepoURL:    proj.RepoURL,
		BaseBranch: proj.DefaultBranch,
		BaseCommit: d.BaseCommit,
		Workdir:    e.ws.Path(d.ID),
		Title:      d.Title,
	})
	if err != nil {
		e.finishStageRun(ctx, run.ID, "failed")
		e.emit(ctx, d, node.Stage, "persist_failed", map[string]string{"error": err.Error()})
		return e.blockKeepWorkdir(ctx, d, fmt.Errorf("persist: %w", err))
	}
	if serr := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       "diff",
		Content:    capDiff(res.Diff),
	}); serr != nil {
		return serr
	}
	if res.PRURL != "" {
		if serr := e.st.SaveArtifact(ctx, &store.Artifact{
			DeliveryID: d.ID,
			Stage:      node.Stage,
			Kind:       "pr",
			Content:    res.PRURL,
		}); serr != nil {
			return serr
		}
	}
	e.emit(ctx, d, node.Stage, "persist_done", map[string]string{"branch": res.Branch, "pr_url": res.PRURL})
	if res.PRError != "" {
		// push 已成功、PR 没开出来（如已存在/权限不足）：不阻断，只留事件供排查。
		e.emit(ctx, d, node.Stage, "pr_failed", map[string]string{"error": res.PRError})
	}
	return nil
}

// maxDiffBytes diff artifact 的落库上限（字节）：大改动 diff 动辄数 MiB，
// 原样入库撑爆存储与时间线；截断后审查页仍有内容，完整 diff 在固化分支/PR 上。
const maxDiffBytes = 1 << 20 // 1 MiB

// capDiff 超过 maxDiffBytes 时截断并尾缀标记；截点回退到合法 UTF-8 边界
// （最多回退 3 字节，不切坏多字节字符）。
func capDiff(s string) string {
	if len(s) <= maxDiffBytes {
		return s
	}
	cut := s[:maxDiffBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + fmt.Sprintf("\n…（diff 超过 %d 字节已截断，完整内容见固化分支/PR）", maxDiffBytes)
}

// stepGateReview 跑门禁的前置 agent（预审），产出 agent_output artifact 供门禁展示。
// 只预审不推进——推进由人工 Approve 决定。
// 审查节点绑定为 local runner（本机交互占位）时跳过预审：门禁照常挂起等人工。
func (e *Engine) stepGateReview(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	ar, err := e.runnerFor(ctx, d, node.ReviewRole)
	if err != nil {
		if !errors.Is(err, orchestration.ErrLocalRunner) {
			e.finishStageRun(ctx, run.ID, "failed")
			e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{"error": err.Error()})
			return e.block(ctx, d, fmt.Errorf("stage %s: %w", node.Stage, err))
		}
		e.finishStageRun(ctx, run.ID, "done")
		e.emitLocalPending(ctx, d, node.Stage, node.ReviewRole)
		return nil
	}
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return err
	}
	res, err := runAgent(ctx, ar, agent.Request{
		Role:    node.ReviewRole,
		Prompt:  agent.BuildPrompt(node.ReviewRole, d.Description, spec, ""),
		Workdir: e.ws.Path(d.ID),
	})
	if err != nil {
		return e.agentFailed(ctx, d, node, run, err)
	}
	if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       "agent_output",
		Content:    res.Output,
	}); err != nil {
		return err
	}
	e.finishStageRun(ctx, run.ID, "done")
	return nil
}

// stageFeedback 组装带入本轮 prompt 的上一轮反馈（空串 = 无反馈）：
//   - spec/design/tasks：各自门禁的人打回意见（消费一次后清空持久化的 RejectReason）
//   - code_gen：code_review 门禁的人打回意见 + 上一轮 unit_test 失败输出（FailCount>0 时）
//
// 反馈只注入回退后该阶段的第一次重跑，避免每轮都背着旧意见。
func (e *Engine) stageFeedback(ctx context.Context, d *store.Delivery, stage string) (string, error) {
	switch stage {
	case "spec", "design", "tasks":
		return e.consumeRejectReason(ctx, d)
	case "code_gen":
		fb, err := e.consumeRejectReason(ctx, d)
		if err != nil {
			return "", err
		}
		if d.FailCount > 0 {
			out, err := e.latestTestOutput(ctx, d.ID)
			if err != nil {
				return "", err
			}
			if out != "" {
				if fb != "" {
					fb += "\n"
				}
				fb += "上一轮 unit_test 未过：" + out
			}
		}
		return fb, nil
	}
	return "", nil
}

// consumeRejectReason 取出并清空人打回意见（返回空串表示无）。
func (e *Engine) consumeRejectReason(ctx context.Context, d *store.Delivery) (string, error) {
	if d.RejectReason == "" {
		return "", nil
	}
	fb := "人打回：" + d.RejectReason
	d.RejectReason = ""
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return "", err
	}
	return fb, nil
}

// stepUnitTest 跑测试：失败回环 code_gen（连续 MaxFail 次 blocked），通过清零计数走 Next。
func (e *Engine) stepUnitTest(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	pass, output, err := e.tr.RunTests(ctx, e.ws.Path(d.ID))
	if serr := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       "test_output",
		Content:    output,
	}); serr != nil {
		return serr
	}
	if err == nil && pass {
		e.finishStageRun(ctx, run.ID, "done")
		d.FailCount = 0 // 连续失败计数在通过时清零
		return e.advance(ctx, d, node.Next)
	}
	e.finishStageRun(ctx, run.ID, "failed")
	failErr := ""
	if err != nil {
		failErr = err.Error()
	}
	e.emit(ctx, d, node.Stage, "test_failed", map[string]string{"error": failErr, "output": output})
	d.FailCount++
	if d.FailCount >= MaxFail {
		// 设计内终态：blocked 是正常业务结果，不作为错误上抛。
		_ = e.block(ctx, d, fmt.Errorf("unit_test failed %d consecutive times (max %d)", d.FailCount, MaxFail))
		return errStop
	}
	// 回环 code_gen 修复后暂停，重跑由下一次 Start 驱动。
	if err := e.advance(ctx, d, node.OnFail); err != nil {
		return err
	}
	return errStop
}

// advance 推进到 next；DONE 表示终态：completed + 释放 workspace。
// 拆分子需求完成时异步驱动父（合并/批次调度），不阻塞子需求自己的收尾。
func (e *Engine) advance(ctx context.Context, d *store.Delivery, next string) error {
	if next == "DONE" {
		d.Status = StatusCompleted
		if err := e.st.UpdateDelivery(ctx, d); err != nil {
			return err
		}
		e.ws.Release(d.ID)
		e.emit(ctx, d, d.CurrentStage, "delivery_completed", nil)
		if d.ParentID != "" {
			parentID := d.ParentID
			go func() {
				// 独立 ctx：子需求驱动 ctx 可能随请求结束被取消，父推进不受影响。
				pctx, cancel := context.WithTimeout(context.Background(), parentDriveTimeout)
				defer cancel()
				e.MaybeDriveParent(pctx, parentID)
			}()
		}
		return nil
	}
	d.CurrentStage = next
	return e.st.UpdateDelivery(ctx, d)
}

// block 进入终态 blocked：释放 workspace、记 delivery_blocked 事件，返回 cause 供调用方决定是否上抛。
func (e *Engine) block(ctx context.Context, d *store.Delivery, cause error) error {
	return e.blockOpt(ctx, d, cause, true)
}

// agentFailed agent 调用失败的共同处理：stage_failed 事件 + blocked（设计内终态）。
func (e *Engine) agentFailed(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun, err error) error {
	e.finishStageRun(ctx, run.ID, "failed")
	e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{"error": err.Error()})
	return e.block(ctx, d, fmt.Errorf("stage %s: %w", node.Stage, err))
}

// blockKeepWorkdir 同 block 但不释放 workspace：persist 失败时产出只存在于
// workdir（push 没上去），必须原样保留供人工救援——这是数据安全不变量。
func (e *Engine) blockKeepWorkdir(ctx context.Context, d *store.Delivery, cause error) error {
	return e.blockOpt(ctx, d, cause, false)
}

func (e *Engine) blockOpt(ctx context.Context, d *store.Delivery, cause error, release bool) error {
	d.Status = StatusBlocked
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	if release {
		e.ws.Release(d.ID)
	}
	e.emit(ctx, d, d.CurrentStage, "delivery_blocked", map[string]string{"reason": cause.Error()})
	return cause
}

// --- 记录与事件 ---

// startStageRun 记录一次阶段执行（attempt 递增）并广播 stage_started。
func (e *Engine) startStageRun(ctx context.Context, d *store.Delivery, stage string) (*store.StageRun, error) {
	attempt := 1
	if prev, err := e.st.LatestStageRun(ctx, d.ID, stage); err == nil {
		attempt = prev.Attempt + 1
	}
	r := &store.StageRun{DeliveryID: d.ID, Stage: stage, Attempt: attempt, Status: "running"}
	if err := e.st.StartStageRun(ctx, r); err != nil {
		return nil, err
	}
	e.emit(ctx, d, stage, "stage_started", map[string]int{"attempt": attempt})
	return r, nil
}

func (e *Engine) finishStageRun(ctx context.Context, runID, status string) {
	_ = e.st.FinishStageRun(ctx, runID, status)
}

// finishLatestRun 结束某阶段最近一次 StageRun（门禁被 Approve/Reject 时收尾）。
func (e *Engine) finishLatestRun(ctx context.Context, deliveryID, stage, status string) {
	if r, err := e.st.LatestStageRun(ctx, deliveryID, stage); err == nil {
		_ = e.st.FinishStageRun(ctx, r.ID, status)
	}
}

// latestSpec 取最近一条 kind=spec 的产物内容（无则空串）。
func (e *Engine) latestSpec(ctx context.Context, deliveryID string) (string, error) {
	return e.latestArtifactContent(ctx, deliveryID, "spec")
}

// latestArtifactContent 取最近一条指定 kind 的产物内容（无则空串）。
func (e *Engine) latestArtifactContent(ctx context.Context, deliveryID, kind string) (string, error) {
	a, err := e.st.LatestArtifact(ctx, deliveryID, kind)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return a.Content, nil
}

// promptAddenda 角色模板之外的附加注入段（R9 / 任务层）：
//   - spec：子需求（parent_id 非空）注入父的 spec + design artifact 作为约束参考；
//   - tasks：大需求路径注入已批准的设计文档作为约束参考。
//
// 不适用 / 无可注入内容时返回空串。
func (e *Engine) promptAddenda(ctx context.Context, d *store.Delivery, stage string) (string, error) {
	switch stage {
	case "spec":
		return e.parentContext(ctx, d)
	case "tasks":
		design, err := e.latestArtifactContent(ctx, d.ID, "design")
		if err != nil {
			return "", err
		}
		if design == "" {
			return "", nil
		}
		return "\n\n设计文档（约束参考）：\n" + design, nil
	}
	return "", nil
}

// parentContext 子需求的父上下文注入段：父的 spec + design artifact
// （"作为约束参考，不要重写"）。无父 / 父暂无产物 → 空串不注入。
func (e *Engine) parentContext(ctx context.Context, d *store.Delivery) (string, error) {
	if d.ParentID == "" {
		return "", nil
	}
	spec, err := e.latestArtifactContent(ctx, d.ParentID, "spec")
	if err != nil {
		return "", err
	}
	design, err := e.latestArtifactContent(ctx, d.ParentID, "design")
	if err != nil {
		return "", err
	}
	if spec == "" && design == "" {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("\n\n以下是父需求规格/设计，作为约束参考，不要重写：")
	if spec != "" {
		b.WriteString("\n\n父需求规格：\n")
		b.WriteString(spec)
	}
	if design != "" {
		b.WriteString("\n\n父需求设计：\n")
		b.WriteString(design)
	}
	return b.String(), nil
}

// latestTestOutput 取最近一条 unit_test 输出（截断 maxTestOutputRunes；无则空串）。
func (e *Engine) latestTestOutput(ctx context.Context, deliveryID string) (string, error) {
	a, err := e.st.LatestArtifact(ctx, deliveryID, "test_output")
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return truncateRunes(a.Content, maxTestOutputRunes), nil
}

// maxTestOutputRunes 反馈里测试输出的截断上限（rune 计），控制 prompt 体积。
const maxTestOutputRunes = 2000

// truncateRunes 按 rune 截断（避免切坏多字节字符），超长时尾缀截断标记。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…（截断）"
}

// emit 追加事件并触发 Notify（事件失败不阻断流水线）。
func (e *Engine) emit(ctx context.Context, d *store.Delivery, stage, eventType string, payload any) {
	var data []byte
	if payload != nil {
		data, _ = json.Marshal(payload)
	}
	_ = e.st.AppendEvent(ctx, &store.Event{
		DeliveryID: d.ID,
		Stage:      stage,
		EventType:  eventType,
		Payload:    data,
	})
	if e.Notify != nil {
		e.Notify(d.ID, stage, eventType)
	}
}

// emitLocalPending 广播本机停车事件（幂等）：交付已停在 node 等本机交回、
// 且从上次广播后未动过（无交回 / 无门禁动作）时跳过——重复驱动（重启恢复、
// driveLocked 回环）不再刷屏；真正离开后再停车（新停车）照常广播。
func (e *Engine) emitLocalPending(ctx context.Context, d *store.Delivery, eventStage, node string) {
	if e.localParkAnnounced(ctx, d.ID, node) {
		return
	}
	e.emit(ctx, d, eventStage, "local_stage_pending", map[string]string{"node": node})
}

// localParkAnnounced 交付是否已广播过 node 的本机停车（事件流末尾回溯）：
// 先遇到 local_stage_pending{node}（其后没有该节点的 local_stage_submitted、
// 也没有任何门禁 approve/reject——即交付从停车点未动过）→ 已广播。
// 事件读失败按未广播处理（可见性优先，宁可重发）。
func (e *Engine) localParkAnnounced(ctx context.Context, deliveryID, node string) bool {
	evs, err := e.st.ListEvents(ctx, deliveryID)
	if err != nil {
		return false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		switch evs[i].EventType {
		case "gate_approved", "gate_rejected":
			return false // 门禁动过 = 已进入新一轮，停车是新停车
		case "local_stage_submitted", "local_stage_pending":
			var p struct {
				Node string `json:"node"`
			}
			if err := json.Unmarshal(evs[i].Payload, &p); err != nil || p.Node != node {
				continue
			}
			return evs[i].EventType == "local_stage_pending"
		}
	}
	return false
}
