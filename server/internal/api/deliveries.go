package api

import (
	"context"
	"errors"
	"log"
	"net/http"
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

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	ds, err := s.st.ListProjectDeliveries(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取交付列表失败")
		return
	}
	if ds == nil {
		ds = []store.Delivery{}
	}
	writeJSON(w, http.StatusOK, ds)
}

func (s *Server) handleCreateDelivery(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if strings.TrimSpace(body.Title) == "" {
		writeError(w, http.StatusBadRequest, "标题不能为空")
		return
	}
	d := &store.Delivery{
		ProjectID:    projectID,
		Title:        body.Title,
		Description:  body.Description,
		Status:       "active",
		CurrentStage: "intake",
	}
	if err := s.st.CreateDelivery(r.Context(), d); err != nil {
		writeError(w, http.StatusInternalServerError, "创建交付失败")
		return
	}
	_ = s.st.AppendEvent(r.Context(), &store.Event{
		DeliveryID: d.ID,
		Stage:      "intake",
		EventType:  "delivery_created",
		Payload:    []byte(`{}`),
	})
	// 异步驱动引擎：创建即返回，推进在后台进行（门禁/终态时 driver 停车）。
	go s.runDelivery(d.ID)
	writeJSON(w, http.StatusOK, d)
}

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
	writeJSON(w, http.StatusOK, map[string]any{
		"delivery":  d,
		"timeline":  timeline,
		"artifacts": artifacts,
	})
}

// gateArtifactKind 门禁 → 待展示产物 kind：spec 门禁看 spec 全文，其余看 agent 产物。
func gateArtifactKind(gate string) string {
	if gate == "spec_approval" {
		return "spec"
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
		writeError(w, http.StatusBadRequest, "no pending gate")
		return
	}
	kind := gateArtifactKind(d.PendingGate)
	agent, output, prURL := d.PendingGate, "", ""
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
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"delivery_id":  d.ID,
		"gate":         d.PendingGate,
		"agent_output": map[string]string{"agent": agent, "output": output},
		"pr_url":       prURL,
	})
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.withGateAction(w, r, func(ctx context.Context) error {
		return s.engine.Approve(ctx, chi.URLParam(r, "id"))
	})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	if !validID(w, chi.URLParam(r, "id")) {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
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
// 同一 delivery 同时只允许一个驱动者（create 的异步 driver / approve / reject 互斥）。
func (s *Server) lockFor(deliveryID string) *sync.Mutex {
	v, _ := s.locks.LoadOrStore(deliveryID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// runDelivery 异步驱动（创建/重启恢复时调用）：拿 per-delivery 锁后推进到稳定。
func (s *Server) runDelivery(deliveryID string) {
	mu := s.lockFor(deliveryID)
	mu.Lock()
	defer mu.Unlock()
	s.driveLocked(deliveryID)
}

// ResumeActive 服务重启后的恢复：对所有 active 交付重新点火后台驱动。
// gate-parked 的被驱动循环的状态检查直接跳过（零引擎调用）；停在 code_gen 回环 /
// 中断在半路的从 CurrentStage 继续（workspace 未就绪则由 Start 重新 Acquire）。
func (s *Server) ResumeActive(ctx context.Context) {
	if s.engine == nil {
		return
	}
	ds, err := s.st.ListActiveDeliveries(ctx)
	if err != nil {
		log.Printf("resume: list active deliveries: %v", err)
		return
	}
	for _, d := range ds {
		go s.runDelivery(d.ID)
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

// writeDeliveryNow 返回引擎推进后的最新 delivery 状态。
func (s *Server) writeDeliveryNow(w http.ResponseWriter, r *http.Request, id string) {
	d, err := s.st.GetDelivery(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取交付失败")
		return
	}
	writeJSON(w, http.StatusOK, d)
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
