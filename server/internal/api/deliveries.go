package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/tokfinity/infera/internal/store"
)

// maxStarts 单次驱动调用里引擎调用的上限：unit_test 失败回环每轮消耗一次，
// 正常路径 1-2 次即稳定；上限防止 active-无门禁 的病态循环空转。
const maxStarts = 6

func (s *Server) handleGetDelivery(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	d, err := s.st.GetDelivery(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	timeline, err := s.st.ListEvents(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时间线失败")
		return
	}
	artifacts, err := s.st.ListArtifacts(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取产物失败")
		return
	}
	if timeline == nil {
		timeline = []store.Event{}
	}
	if artifacts == nil {
		artifacts = []store.Artifact{}
	}
	dj, err := s.deliveryWithLabels(r, d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取标签失败")
		return
	}
	resp := map[string]any{
		"delivery":  dj,
		"timeline":  timeline,
		"artifacts": artifacts,
	}
	// 拆分父附子需求清单（前端自建树；普通交付不含该字段）。
	if d.SplitMode {
		children, err := s.st.ListChildDeliveries(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取子需求失败")
			return
		}
		if children == nil {
			children = []store.Delivery{}
		}
		// 批量取子需求标签（一次查询装配，免 N+1），逐行投影为冻结契约形态。
		byID, err := s.st.LabelsByDeliveryID(r.Context(), deliveryIDs(children))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取子需求标签失败")
			return
		}
		rows := make([]deliveryJSON, 0, len(children))
		for i := range children {
			rows = append(rows, deliveryJSON{Delivery: children[i], Labels: labelsJSON(byID[children[i].ID])})
		}
		resp["children"] = rows
	}
	writeJSON(w, http.StatusOK, resp)
}

// deliveryIDs 取交付平表的 ID 列表（批量标签装配用）。
func deliveryIDs(ds []store.Delivery) []string {
	ids := make([]string, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids
}

// gateArtifactKind 门禁 → 待展示产物 kind：spec/design/tasks 门禁看各自文档全文，
// 其余（code_review）看门禁前置 agent 的预审产物。
func gateArtifactKind(gate string) string {
	switch gate {
	case "spec_approval":
		return "spec"
	case "design_approval":
		return "design"
	case "tasks_approval":
		return "tasks"
	}
	return "agent_output"
}

func (s *Server) handleGate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	d, err := s.st.GetDelivery(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	if d.PendingGate == "" {
		writeError(w, http.StatusBadRequest, "当前没有待审批的门禁")
		return
	}
	kind := gateArtifactKind(d.PendingGate)
	agent, output, prURL, diff := d.PendingGate, "", "", ""
	arts, err := s.st.ListArtifacts(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取产物失败")
		return
	}
	found := false
	for i := len(arts) - 1; i >= 0; i-- { // 从最新往旧找
		if !found && arts[i].Kind == kind {
			agent, output = arts[i].Stage, arts[i].Content
			found = true
		}
		if prURL == "" && arts[i].Kind == "pr" {
			prURL = arts[i].Content
		}
		if diff == "" && arts[i].Kind == "diff" {
			diff = arts[i].Content
		}
	}
	resp := map[string]any{
		"delivery_id":  d.ID,
		"gate":         d.PendingGate,
		"agent_output": map[string]string{"agent": agent, "output": output},
		"pr_url":       prURL,
	}
	switch d.PendingGate {
	case "spec_approval":
		// spec 审批门附 AI 复杂度建议（spec 末尾的 infera-complexity fenced block；无/坏 = 空串，前端按 small 预选）。
		resp["complexity_suggestion"] = parseComplexitySuggestion(output)
	case "design_approval":
		// 设计审批门附 AI 拆分建议（design 末尾的 infera-split fenced block；无/坏 = nil）。
		resp["split_plan"] = parseSplitPlan(output)
	case "tasks_approval":
		// 任务审批门附可编辑清单（tasks artifact 为引擎解析后的清单 JSON；坏内容 = nil）。
		resp["tasks"] = parseTasksList(output)
	case "code_review":
		// R10 双道审查：findings 报告（引用 + 内容）与真 diff（Persist 已落盘）一起给门禁页。
		resp["diff"] = diff
		resp["reviews"] = gateReviews(arts)
	}
	writeJSON(w, http.StatusOK, resp)
}

// gateReview 门禁响应里的单道审查：findings 引用（artifact_id）+ 解析后内容。
type gateReview struct {
	Review     string          `json:"review"`
	Present    bool            `json:"present"` // 该道是否已产出（local 占位跳过时 false）
	TaskBased  bool            `json:"task_based"`
	ArtifactID string          `json:"artifact_id,omitempty"`
	Findings   []store.Finding `json:"findings"`
	Raw        string          `json:"raw,omitempty"`
}

// gateReviews 从产物里取两道审查的最新 findings 报告（kind=<道名>_findings，
// 与 engine 图 FindingsReviews / store.Kind*Findings 常量对齐）。无产物 → present=false；
// 坏 JSON 容错为原文（findings=null），不崩。
func gateReviews(arts []store.Artifact) []gateReview {
	reviews := []gateReview{{Review: "spec_conformance"}, {Review: "code_quality"}}
	for i := range reviews {
		kind := reviews[i].Review + "_findings"
		for j := len(arts) - 1; j >= 0; j-- { // 从最新往旧找
			if arts[j].Kind != kind {
				continue
			}
			reviews[i].Present = true
			reviews[i].ArtifactID = arts[j].ID
			var rep store.FindingsReport
			if err := json.Unmarshal([]byte(arts[j].Content), &rep); err == nil {
				reviews[i].TaskBased = rep.TaskBased
				reviews[i].Findings = rep.Findings
				reviews[i].Raw = rep.Raw
			} else {
				reviews[i].Raw = arts[j].Content // 历史脏数据：原文兜底展示
			}
			break
		}
	}
	return reviews
}

// complexityRe 从 spec 全文里提取 ```infera-complexity fenced block（AI 的复杂度建议）。
var complexityRe = regexp.MustCompile("```infera-complexity\\n([\\s\\S]*?)\\n```")

// parseComplexitySuggestion 解析 spec 文本里的复杂度建议（small|large）；无/坏块返回空串。
func parseComplexitySuggestion(spec string) string {
	m := complexityRe.FindStringSubmatch(spec)
	if m == nil {
		return ""
	}
	if c := strings.TrimSpace(m[1]); c == "small" || c == "large" {
		return c
	}
	return ""
}

// splitPlanRe 从 spec 全文里提取 ```infera-split fenced block（AI 的拆分建议）。
var splitPlanRe = regexp.MustCompile("```infera-split\\n([\\s\\S]*?)\\n```")

// parseSplitPlan 解析 spec 文本里的拆分建议；无 block / 解析失败返回 nil（按无建议处理）。
func parseSplitPlan(spec string) []store.ChildSpec {
	m := splitPlanRe.FindStringSubmatch(spec)
	if m == nil {
		return nil
	}
	var plan []store.ChildSpec
	if err := json.Unmarshal([]byte(m[1]), &plan); err != nil {
		return nil
	}
	return plan
}

// parseTasksList 从 tasks artifact（清单 JSON）解析任务清单；坏内容返回 nil。
func parseTasksList(content string) []store.TaskSpec {
	var tasks []store.TaskSpec
	if err := json.Unmarshal([]byte(content), &tasks); err != nil {
		return nil
	}
	return tasks
}

// handleApprove 通过当前门禁（引擎 Approve 单入口透传）。body 可选：
// `{"complexity":"small"|"large"}`（spec_approval 裁定复杂度，缺省取 AI 建议）；
// `{"split":[{title,description,wave}]}`（design_approval = 「批准并拆分」）；
// `{"tasks":[{title,detail}]}`（tasks_approval = 批准并覆盖任务清单）。
// 空/缺 body = 普通批准。wave-1 子需求的点火由 engine 的 OnStartDelivery 回调完成。
func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	var body struct {
		Complexity string            `json:"complexity"`
		Split      []store.ChildSpec `json:"split"`
		Tasks      []store.TaskSpec  `json:"tasks"`
	}
	// 手工读 body（空/缺 body = 普通批准，decode 不接受空 body），
	// 但必须与 decode 同款 1MiB 上限——MaxBytesReader 超限即报错，
	// 不得整包读入内存再解析（内存放大防护）。
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		// 读体失败（客户端断连/超限）必须拒绝：吞错按空 body 继续会把门禁批掉。
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			writeError(w, http.StatusBadRequest, "请求体不合法")
			return
		}
	}
	s.withGateAction(w, r, func(ctx context.Context) error {
		_, err := s.engine.Approve(ctx, id, store.ApproveOpts{
			Complexity: body.Complexity,
			Split:      body.Split,
			Tasks:      body.Tasks,
		})
		return err
	})
}

// handleMergeResume 拆分父的冲突恢复：人工解冲突推 infera/<父前8位> 分支后调用。
// ResumeMerge fetch+reset 并重跑合并队列（可能又停在等剩余子需求）；随后照常点火驱动
// （run() 对停在 code_gen 的拆分父路由 MaybeDriveParent，幂等无害）。
func (s *Server) handleMergeResume(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	if s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine 未装配")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mu := s.lockFor(id)
	mu.Lock()
	if err := s.engine.ResumeMerge(ctx, id); err != nil {
		mu.Unlock()
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "交付不存在")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 锁移交：与 approve 同款——后台驱动跑完才放锁，响应立即写回当前状态。
	go func() {
		defer mu.Unlock()
		s.driveLocked(id)
	}()
	s.writeDeliveryNow(w, r, id)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	if !validID(w, chi.URLParam(r, "id")) {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	s.withGateAction(w, r, func(ctx context.Context) error {
		return s.engine.Reject(ctx, chi.URLParam(r, "id"), body.Reason)
	})
}

// withGateAction 是 approve/reject 的共享流程：
// 校验 id → 持 per-delivery 锁 → 用与请求脱钩的有界 ctx 执行簿记
// （Approve/Reject 只改状态落盘，客户端断连不能杀掉持久化）。
// 簿记成功后把锁所有权移交给后台驱动 goroutine（推进到下一个停车点才放锁），
// 响应立即写回最新状态——GetDelivery 只读，不需要锁。
func (s *Server) withGateAction(w http.ResponseWriter, r *http.Request, action func(ctx context.Context) error) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	if s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine 未装配")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mu := s.lockFor(id)
	mu.Lock()
	err := action(ctx)
	if err != nil {
		mu.Unlock()
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "交付不存在")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 锁移交：驱动 goroutine 跑完 driveLocked 才解锁，handler 不等它。
	go func() {
		defer mu.Unlock()
		s.driveLocked(id)
	}()
	s.writeDeliveryNow(w, r, id)
}

// --- 驱动与锁 ---

// lockFor 取/建 per-delivery 锁：引擎自身无并发保护，
// 同一 delivery 同时只允许一个驱动者（create 的异步 driver / approve / reject
// / MCP 簿记互斥——与 MCP 面共享同一份注册表）。
func (s *Server) lockFor(deliveryID string) *sync.Mutex {
	return s.locks.For(deliveryID)
}

// runDelivery 异步驱动（创建/重启恢复时调用）：拿 per-delivery 锁后推进到稳定。
func (s *Server) runDelivery(deliveryID string) {
	mu := s.lockFor(deliveryID)
	mu.Lock()
	defer mu.Unlock()
	s.driveLocked(deliveryID)
}

// RunDelivery 是 runDelivery 的导出版本：engine 的 OnStartDelivery 回调
// （批次点火）由 main 组装注入，engine 不能反向 import api，所以由 main 闭包转接。
func (s *Server) RunDelivery(deliveryID string) {
	s.runDelivery(deliveryID)
}

// resumeParentTimeout 重启恢复驱动拆分父（合并队列，可能包含多次 fetch/merge）的时间上限。
const resumeParentTimeout = 30 * time.Minute

// maxResumeConcurrency ResumeActive 的恢复驱动并发上限：重启时上百 active 交付
// 同时点火会打爆 agent 后端/DB；信号量排队放行（每交付仍各自 per-delivery 锁串行）。
const maxResumeConcurrency = 8

// ResumeActive 服务重启后的恢复：对所有 active 交付重新点火后台驱动（并发受限）。
// gate-parked 的被驱动循环的状态检查直接跳过（零引擎调用）；停在 code_gen 回环 /
// 中断在半路的从 CurrentStage 继续（workspace 未就绪则由 Start 重新 Acquire）。
// 拆分父（split_mode 且停在 code_gen）例外：那是"等子需求/合并"语义，
// runDelivery 的 Continue 路径虽已被引擎 run() 守卫兜底，但恢复时直接走
// MaybeDriveParent（合并/批次调度推进），不绕 agent 驱动循环。
func (s *Server) ResumeActive(ctx context.Context) {
	if s.engine == nil {
		return
	}
	ds, err := s.st.ListActiveDeliveries(ctx)
	if err != nil {
		log.Printf("resume: list active deliveries: %v", err)
		return
	}
	sem := make(chan struct{}, maxResumeConcurrency)
	for _, d := range ds {
		if d.SplitMode && d.CurrentStage == "code_gen" {
			id := d.ID
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				pctx, cancel := context.WithTimeout(context.Background(), resumeParentTimeout)
				defer cancel()
				if err := s.engine.MaybeDriveParent(pctx, id); err != nil {
					log.Printf("resume: drive split parent %s: %v", id, err)
				}
			}()
			continue
		}
		id := d.ID
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			s.runDelivery(id)
		}()
	}
	log.Printf("resume: resumed %d active deliveries", len(ds))
}

// driveLocked 驱动引擎直到稳定（门禁 / 终态 / 出错），处理 unit_test 失败后的停车重试。
// 调用方必须已持有该 delivery 的锁。先查状态再驱动：已稳定（门禁/终态）时零调用。
// 驱动用 background ctx：post-approve 的推进在响应返回后继续，不受请求生命周期影响。
func (s *Server) driveLocked(deliveryID string) {
	for i := 0; i < maxStarts; i++ {
		if s.engine == nil {
			return
		}
		d, err := s.st.GetDelivery(context.Background(), deliveryID)
		if err != nil || d.Status != "active" || d.PendingGate != "" {
			return // 门禁/终态：停
		}
		// workspace 未就绪（创建 / 重启恢复路径）用 Start 负责 Acquire；
		// 其余（Approve/Reject 后的重点火、unit_test 回环重试）用 Continue，不重复 ensure。
		drive := s.engine.Continue
		if !d.WorkspaceReady {
			drive = s.engine.Start
		}
		if err := drive(context.Background(), deliveryID); err != nil {
			log.Printf("engine drive %s: %v", deliveryID, err)
			return // 引擎报错：blocked/completed/agent 失败都由状态承载
		}
		// 驱动后仍 active 且无门禁 = unit_test 失败停车在 code_gen → 下一轮重试
	}
}

// writeDeliveryNow 返回引擎推进后的最新 delivery 状态（带挂的标签）。
func (s *Server) writeDeliveryNow(w http.ResponseWriter, r *http.Request, id string) {
	d, err := s.st.GetDelivery(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取交付失败")
		return
	}
	dj, err := s.deliveryWithLabels(r, d)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取标签失败")
		return
	}
	writeJSON(w, http.StatusOK, dj)
}

// writeStoreErr 把 store 错误映射为 404/500 并写出。
func writeStoreErr(w http.ResponseWriter, err error, notFoundMsg, serverMsg string) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, notFoundMsg)
		return
	}
	writeError(w, http.StatusInternalServerError, serverMsg)
}

// validID 校验路径 id 是合法 UUID：畸形 id 直接 404（资源不存在），
// 避免把它传进 store 后以数据库驱动错误泄漏成 500。
func validID(w http.ResponseWriter, id string) bool {
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusNotFound, "资源不存在")
		return false
	}
	return true
}
