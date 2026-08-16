package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tokfinity/infera/internal/agent"
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

// RejectLoopBack 人工驳回后的回退目标：gate → 需重跑的阶段。
var RejectLoopBack = map[string]string{
	"spec_approval": "spec",
	"code_review":   "code_gen",
}

// artifactKind 每个阶段产物的 kind。
var artifactKind = map[string]string{
	"spec":        "spec",
	"test_gen":    "tests",
	"code_gen":    "diff",
	"code_review": "agent_output",
}

// errStop 是内部信号：本轮推进到此为止（门禁暂停 / 失败回环 / 终态），不是错误。
var errStop = errors.New("engine: stop this run")

// Engine 阶段图执行器。只认识 Graph 的节点类型与下一跳，不认识具体业务。
type Engine struct {
	st store.Store
	ar agent.Runner
	ws Workspace
	tr TestRunner

	// Notify 可选：事件发生时回调（WS 推送，main 组装注入）。
	Notify func(deliveryID, stage, eventType string)
}

func New(st store.Store, ar agent.Runner, ws Workspace, tr TestRunner) *Engine {
	return &Engine{st: st, ar: ar, ws: ws, tr: tr}
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

// Approve 通过当前人工门禁并继续推进。
func (e *Engine) Approve(ctx context.Context, deliveryID string) error {
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
	e.emit(ctx, d, node.Stage, "gate_approved", nil)
	e.finishLatestRun(ctx, d.ID, node.Stage, "done")
	d.PendingGate = ""
	if err := e.advance(ctx, d, node.Next); err != nil {
		return err
	}
	return e.run(ctx, d)
}

// Reject 驳回当前人工门禁：按 RejectLoopBack 回退后暂停，
// 重跑由下一次 Start 驱动（便于把驳回意见带入下一轮，而非立刻原样重跑）。
func (e *Engine) Reject(ctx context.Context, deliveryID, reason string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil {
		return err
	}
	if d.PendingGate == "" {
		return fmt.Errorf("engine: delivery %s has no pending gate", d.ID)
	}
	e.emit(ctx, d, d.PendingGate, "gate_rejected", map[string]string{"reason": reason})
	e.finishLatestRun(ctx, d.ID, d.PendingGate, "failed")
	back, ok := RejectLoopBack[d.PendingGate]
	if !ok {
		return fmt.Errorf("engine: no reject loop-back for gate %q", d.PendingGate)
	}
	d.PendingGate = ""
	d.CurrentStage = back
	return e.st.UpdateDelivery(ctx, d)
}

// --- 内部推进 ---

// ensureWorkspace 首次进入时 Acquire 仓库并持久化 base_commit。
func (e *Engine) ensureWorkspace(ctx context.Context, d *store.Delivery) error {
	if d.BaseCommit != "" {
		return nil
	}
	proj, err := e.st.GetProject(ctx, d.ProjectID)
	if err != nil {
		return err
	}
	_, base, err := e.ws.Acquire(ctx, d.ID, proj.RepoURL, proj.DefaultBranch)
	if err != nil {
		return err
	}
	d.BaseCommit = base
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	e.emit(ctx, d, "intake", "workspace_ready", map[string]string{"base_commit": base})
	return nil
}

// run 推进直到停止条件（门禁 / 回环 / 终态 / 错误）。
func (e *Engine) run(ctx context.Context, d *store.Delivery) error {
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
	case KindTerminal:
		d.Status = StatusCompleted
		if err := e.st.UpdateDelivery(ctx, d); err != nil {
			return err
		}
		e.ws.Release(d.ID)
		e.emit(ctx, d, node.Stage, "delivery_completed", nil)
		return errStop
	default:
		return e.block(ctx, d, fmt.Errorf("engine: unknown node kind %v", node.Kind))
	}
}

// stepAgent 跑一个 agent 节点：带 spec 组 prompt、存产物、走 Next。
func (e *Engine) stepAgent(ctx context.Context, d *store.Delivery, node Node, run *store.StageRun) error {
	spec, err := e.latestSpec(ctx, d.ID)
	if err != nil {
		return err
	}
	res, err := e.ar.Run(ctx, agent.Request{
		Role:    node.Stage,
		Prompt:  agent.BuildPrompt(node.Stage, d.Description, spec),
		Workdir: e.ws.Path(d.ID),
		Inputs:  map[string]string{"spec": spec},
	})
	if err != nil {
		e.finishStageRun(ctx, run.ID, "failed")
		e.emit(ctx, d, node.Stage, "stage_failed", map[string]string{"error": err.Error()})
		return e.block(ctx, d, fmt.Errorf("stage %s: %w", node.Stage, err))
	}
	if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: d.ID,
		Stage:      node.Stage,
		Kind:       artifactKind[node.Stage],
		Content:    res.Output,
	}); err != nil {
		return err
	}
	e.finishStageRun(ctx, run.ID, "done")
	return e.advance(ctx, d, node.Next)
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
func (e *Engine) advance(ctx context.Context, d *store.Delivery, next string) error {
	if next == "DONE" {
		d.Status = StatusCompleted
		if err := e.st.UpdateDelivery(ctx, d); err != nil {
			return err
		}
		e.ws.Release(d.ID)
		e.emit(ctx, d, d.CurrentStage, "delivery_completed", nil)
		return nil
	}
	d.CurrentStage = next
	return e.st.UpdateDelivery(ctx, d)
}

// block 进入终态 blocked：释放 workspace、记 delivery_blocked 事件，返回 cause 供调用方决定是否上抛。
func (e *Engine) block(ctx context.Context, d *store.Delivery, cause error) error {
	d.Status = StatusBlocked
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	e.ws.Release(d.ID)
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
	arts, err := e.st.ListArtifacts(ctx, deliveryID)
	if err != nil {
		return "", err
	}
	for i := len(arts) - 1; i >= 0; i-- {
		if arts[i].Kind == "spec" {
			return arts[i].Content, nil
		}
	}
	return "", nil
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
