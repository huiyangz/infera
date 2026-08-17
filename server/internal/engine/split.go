// 拆分交付：spec 审批拆出子需求（批次调度）→ 子需求各自跑完整流水线 →
// 完成一个合一个进父 workdir（增量合并）→ 全部合并后父走 unit_test → 审查交付。
// 冲突时父暂停合并队列（其它子需求继续跑），人工解冲突推 infera/<父前8位> 分支，
// ResumeMerge fetch+reset 后恢复队列。
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
)

const (
	// StatusQueued 拆分子需求的未启动态：还没轮到自己的批次（区别于 active：
	// ResumeActive 不驱动 queued，避免未来批次被提前点火）。
	StatusQueued = "queued"
	// MergeStateConflict 父 merge_state：合并冲突，暂停队列等人工解决。
	MergeStateConflict = "conflict"
	// mergedChildKind 已合并子需求的 durable 标记（append-only artifact，
	// content = childID；重启后"哪些已合并"仍可恢复）。
	mergedChildKind = "merged_child"
)

// ApproveWithSplit 通过门禁；split 非空 = 「批准并拆分」（仅 spec_approval）：
// 父置 split_mode、直接停在 code_gen（跳过 test_gen——父的实现就是子需求分支的合并），
// 为每条规格建子 delivery（全部 queued），wave 1 立即启动。返回创建的子需求。
func (e *Engine) ApproveWithSplit(ctx context.Context, deliveryID string, split []store.ChildSpec) ([]store.Delivery, error) {
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
	if len(split) == 0 {
		return nil, e.approvePlain(ctx, d, node)
	}
	if d.PendingGate != "spec_approval" {
		return nil, fmt.Errorf("engine: split is only allowed at spec_approval, not %q", d.PendingGate)
	}
	specs, maxWave, err := normalizeSplit(split)
	if err != nil {
		return nil, err
	}
	proj, err := e.st.GetProject(ctx, d.ProjectID)
	if err != nil {
		return nil, err
	}
	if proj.RepoURL == "" {
		return nil, fmt.Errorf("engine: split requires a project repo (greenfield not supported)")
	}

	gate := d.PendingGate
	d.PendingGate = ""
	d.SplitMode = true
	d.CurrentStage = "code_gen"
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return nil, err
	}
	e.finishLatestRun(ctx, d.ID, gate, "done")
	e.emit(ctx, d, gate, "gate_approved", nil)
	e.emit(ctx, d, gate, "split", map[string]int{"count": len(specs), "waves": maxWave})

	children := make([]store.Delivery, 0, len(specs))
	for _, spec := range specs {
		child := &store.Delivery{
			ProjectID:    d.ProjectID,
			Title:        spec.Title,
			Description:  spec.Description,
			Status:       StatusQueued,
			CurrentStage: "intake",
			ParentID:     d.ID,
			Wave:         spec.Wave,
		}
		if err := e.st.CreateDelivery(ctx, child); err != nil {
			return nil, err
		}
		e.emit(ctx, child, "intake", "delivery_created", nil)
		children = append(children, *child)
	}
	merged := map[string]bool{}
	e.startDueWaves(ctx, children, merged)
	return children, nil
}

// approvePlain 普通批准：原 Approve 主体（清 gate、簿记、推进 node.Next）。
func (e *Engine) approvePlain(ctx context.Context, d *store.Delivery, node Node) error {
	gate := d.PendingGate
	d.PendingGate = ""
	if err := e.st.UpdateDelivery(ctx, d); err != nil {
		return err
	}
	e.finishLatestRun(ctx, d.ID, gate, "done")
	e.emit(ctx, d, gate, "gate_approved", nil)
	return e.advance(ctx, d, node.Next)
}

// normalizeSplit 校验并规范化拆分方案：标题非空；wave <1 归一为 1。返回最大批次号。
func normalizeSplit(split []store.ChildSpec) ([]store.ChildSpec, int, error) {
	out := make([]store.ChildSpec, 0, len(split))
	maxWave := 0
	for i, s := range split {
		if strings.TrimSpace(s.Title) == "" {
			return nil, 0, fmt.Errorf("engine: split spec %d has empty title", i)
		}
		if s.Wave < 1 {
			s.Wave = 1
		}
		maxWave = max(maxWave, s.Wave)
		out = append(out, s)
	}
	return out, maxWave, nil
}

// MaybeDriveParent 子需求完成后的父推进入口（advance DONE 时异步调用，也可显式调用）。
// per-parent 互斥串行化：多个子需求并行完成时，合并与批次调度不会交错。
// 错误不上抛（异步上下文）：merge_failed 事件已记录，下一次子需求完成会重试。
func (e *Engine) MaybeDriveParent(ctx context.Context, parentID string) {
	unlock := e.lockParent(parentID)
	defer unlock()
	parent, err := e.st.GetDelivery(ctx, parentID)
	if err != nil || !parent.SplitMode {
		return
	}
	_ = e.mergeLoop(ctx, parent)
}

// ResumeMerge 冲突恢复：校验 split 父且 conflict → fetch 人工解决后的
// infera/<父前8位> 分支 → reset --hard 父 workdir → 清 conflict → 重跑合并循环。
func (e *Engine) ResumeMerge(ctx context.Context, parentID string) error {
	unlock := e.lockParent(parentID)
	defer unlock()
	parent, err := e.st.GetDelivery(ctx, parentID)
	if err != nil {
		return err
	}
	if !parent.SplitMode {
		return fmt.Errorf("engine: delivery %s is not a split parent", parentID)
	}
	if parent.MergeState != MergeStateConflict {
		return fmt.Errorf("engine: delivery %s is not in conflict (merge_state %q)", parentID, parent.MergeState)
	}
	proj, err := e.st.GetProject(ctx, parent.ProjectID)
	if err != nil {
		return err
	}
	dir := e.ws.Path(parentID)
	if err := e.g.Fetch(ctx, dir, proj.RepoURL, childBranch(parentID)); err != nil {
		return fmt.Errorf("fetch resolved branch: %w", err)
	}
	if err := e.g.ResetHard(ctx, dir, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("reset --hard: %w", err)
	}
	parent.MergeState = ""
	if err := e.st.UpdateDelivery(ctx, parent); err != nil {
		return err
	}
	e.emit(ctx, parent, "code_gen", "merge_resumed", nil)
	return e.mergeLoop(ctx, parent)
}

// mergeLoop 增量合并循环（maybeDriveParent 与 ResumeMerge 共用）：
// 每轮先做批次调度，再合并下一个未合并的已完成子需求；直到
// 无可合并（等剩余子需求）、冲突暂停、或全部完成（父收尾推进）。
func (e *Engine) mergeLoop(ctx context.Context, parent *store.Delivery) error {
	proj, err := e.st.GetProject(ctx, parent.ProjectID)
	if err != nil {
		return err
	}
	dir := e.ws.Path(parent.ID)
	// 父 workdir 可能在拆分后尚未 Acquire（子需求先完成）：幂等确保就绪。
	if err := e.ensureWorkspace(ctx, parent); err != nil {
		return err
	}
	for {
		children, err := e.st.ListChildDeliveries(ctx, parent.ID)
		if err != nil {
			return err
		}
		merged, err := e.mergedChildren(ctx, parent.ID)
		if err != nil {
			return err
		}
		e.startDueWaves(ctx, children, merged)

		allDone := len(children) > 0
		nextIdx := -1
		for i := range children {
			c := &children[i]
			if c.Status != StatusCompleted {
				allDone = false
				continue
			}
			if !merged[c.ID] && nextIdx == -1 {
				nextIdx = i // ListChildDeliveries 已按 wave, created_at 排序
			}
		}
		if nextIdx == -1 {
			if allDone {
				return e.finalizeParent(ctx, parent, children)
			}
			return nil // 等待未完成的子需求
		}
		if parent.MergeState == MergeStateConflict {
			// 冲突暂停：只记排队事件（每轮每子需求一条，简单可见）。
			for i := range children {
				c := &children[i]
				if c.Status == StatusCompleted && !merged[c.ID] {
					e.emit(ctx, parent, "code_gen", "merge_queued", map[string]string{"child_id": c.ID, "child_title": c.Title})
				}
			}
			return nil
		}

		child := children[nextIdx]
		err = e.g.Fetch(ctx, dir, proj.RepoURL, childBranch(child.ID))
		if err != nil {
			if strings.Contains(err.Error(), "couldn't find remote ref") {
				// 子需求完成但没推分支（无变更，persist 跳过 push）：无可合并，记标记继续。
				if serr := e.saveMergedChild(ctx, parent.ID, &child); serr != nil {
					return serr
				}
				e.emit(ctx, parent, "code_gen", "merge_skipped", map[string]string{"child_id": child.ID, "child_title": child.Title, "reason": "child branch not found (no changes)"})
				merged[child.ID] = true
				continue
			}
			e.emit(ctx, parent, "code_gen", "merge_failed", map[string]string{"child_id": child.ID, "error": err.Error()})
			return err
		}
		if err := e.g.Merge(ctx, dir, "merge: 子需求 "+child.Title); err != nil {
			if !errors.Is(err, git.ErrMergeConflict) {
				e.emit(ctx, parent, "code_gen", "merge_failed", map[string]string{"child_id": child.ID, "error": err.Error()})
				return err
			}
			parent.MergeState = MergeStateConflict
			if uerr := e.st.UpdateDelivery(ctx, parent); uerr != nil {
				return uerr
			}
			e.emit(ctx, parent, "code_gen", "merge_conflict", map[string]any{
				"child_id":     child.ID,
				"child_title":  child.Title,
				"branches":     completedBranches(children),
				"instructions": conflictInstructions(proj, parent.ID, children),
			})
			return nil
		}
		if serr := e.saveMergedChild(ctx, parent.ID, &child); serr != nil {
			return serr
		}
		merged[child.ID] = true
		e.emit(ctx, parent, "code_gen", "merge_done", map[string]string{"child_id": child.ID, "child_title": child.Title})
	}
}

// finalizeParent 全部子需求完成且合并后的父收尾：写 code_gen 合并摘要 artifact、
// 推进 unit_test，并继续驱动（unit_test → code_review 固化/persist → 停门禁）。
// 注意：跑在子需求的驱动 goroutine 里（per-parent 互斥内），父自身无并发驱动。
func (e *Engine) finalizeParent(ctx context.Context, parent *store.Delivery, children []store.Delivery) error {
	titles := make([]string, 0, len(children))
	for _, c := range children {
		titles = append(titles, c.Title)
	}
	if err := e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: parent.ID,
		Stage:      "code_gen",
		Kind:       "summary",
		Content:    fmt.Sprintf("已合并 %d 个子需求分支：%s", len(children), strings.Join(titles, "、")),
	}); err != nil {
		return err
	}
	fresh, err := e.st.GetDelivery(ctx, parent.ID) // 最新副本，避免覆盖并发变更
	if err != nil {
		return err
	}
	fresh.CurrentStage = "unit_test"
	if err := e.st.UpdateDelivery(ctx, fresh); err != nil {
		return err
	}
	if err := e.ensureWorkspace(ctx, fresh); err != nil {
		return err
	}
	return e.run(ctx, fresh)
}

// startDueWaves 批次调度：最小的 queued 批次，若其所有更低批次都已 completed+merged，
// 则启动该批次全部 queued 子需求（status→active + OnStartDelivery 点火）。
func (e *Engine) startDueWaves(ctx context.Context, children []store.Delivery, merged map[string]bool) {
	nextWave := 0
	for i := range children {
		c := &children[i]
		if c.Status == StatusQueued && (nextWave == 0 || c.Wave < nextWave) {
			nextWave = c.Wave
		}
	}
	if nextWave == 0 {
		return
	}
	for i := range children {
		c := &children[i]
		if c.Wave < nextWave && (c.Status != StatusCompleted || !merged[c.ID]) {
			return // 前序批次未全部完成并合并，还不能启动
		}
	}
	for i := range children {
		c := &children[i]
		if c.Wave == nextWave && c.Status == StatusQueued {
			c.Status = StatusActive
			if err := e.st.UpdateDelivery(ctx, c); err != nil {
				continue
			}
			e.emit(ctx, c, "intake", "wave_started", map[string]int{"wave": c.Wave})
			if e.OnStartDelivery != nil {
				e.OnStartDelivery(c.ID)
			}
		}
	}
}

// saveMergedChild 落 durable 合并标记（append-only；重启后仍知哪些子需求已进父）。
func (e *Engine) saveMergedChild(ctx context.Context, parentID string, child *store.Delivery) error {
	return e.st.SaveArtifact(ctx, &store.Artifact{
		DeliveryID: parentID,
		Stage:      "code_gen",
		Kind:       mergedChildKind,
		Content:    child.ID,
	})
}

// mergedChildren 从 artifact 恢复已合并子需求集合（kind=merged_child，content=childID）。
func (e *Engine) mergedChildren(ctx context.Context, parentID string) (map[string]bool, error) {
	arts, err := e.st.ListArtifacts(ctx, parentID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, a := range arts {
		if a.Kind == mergedChildKind {
			out[a.Content] = true
		}
	}
	return out, nil
}

// childBranch 子需求/父的固化分支名（与 persist.Local 一致：infera/<id 前 8 位>）。
func childBranch(deliveryID string) string {
	if len(deliveryID) > 8 {
		return "infera/" + deliveryID[:8]
	}
	return "infera/" + deliveryID
}

// completedBranches 已完成子需求的分支名列表（含已合并的——人工从默认分支
// 重建父分支时，之前合并过的子需求也必须合入，否则 reset 后会丢工作量）。
func completedBranches(children []store.Delivery) []string {
	out := make([]string, 0)
	for _, c := range children {
		if c.Status == StatusCompleted {
			out = append(out, childBranch(c.ID))
		}
	}
	sort.Strings(out)
	return out
}

// conflictInstructions 给人工的完整本地解冲突指引（父详情页展示，可复制）。
func conflictInstructions(proj *store.Project, parentID string, children []store.Delivery) string {
	parentBranch := childBranch(parentID)
	var b strings.Builder
	b.WriteString("父分支合并在服务器上遇到冲突，请在本地解决后推送，再回页面点「继续」：\n\n")
	fmt.Fprintf(&b, "git clone %s infera-resolve\n", proj.RepoURL)
	b.WriteString("cd infera-resolve\n")
	fmt.Fprintf(&b, "git checkout -b %s origin/%s\n", parentBranch, proj.DefaultBranch)
	for _, c := range children {
		if c.Status == StatusCompleted {
			fmt.Fprintf(&b, "git merge origin/%s\n", childBranch(c.ID))
		}
	}
	b.WriteString("# 解决冲突后：\ngit add -A\ngit commit\ngit push origin " + parentBranch)
	return b.String()
}

// lockParent 取 per-parent 互斥锁（sync.Map 惰性创建），返回解锁函数。
func (e *Engine) lockParent(parentID string) func() {
	v, _ := e.parentLocks.LoadOrStore(parentID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
