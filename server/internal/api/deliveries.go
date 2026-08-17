package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

// maxStarts 单次驱动调用里 Start 的上限：unit_test 失败回环每轮消耗一次，
// 正常路径 1-2 次即稳定；上限防止 active-无门禁 的病态循环空转。
const maxStarts = 6

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
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
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "项目不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取项目失败")
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
	id := chi.URLParam(r, "id")
	if s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine 未装配")
		return
	}
	if _, err := s.st.GetDelivery(r.Context(), id); err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	mu := s.lockFor(id)
	mu.Lock()
	err := s.engine.Approve(r.Context(), id)
	mu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Approve 只做门禁簿记即返回（不同步跑 agent）；后续推进在后台驱动，
	// goroutine 自取 per-delivery 锁（driveLocked 约定调用方持锁、内部用 background ctx）。batch 2 会重构为显式 Continue。
	go func() {
		mu := s.lockFor(id)
		mu.Lock()
		defer mu.Unlock()
		s.driveLocked(id)
	}()
	s.writeDeliveryNow(w, r, id)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.engine == nil {
		writeError(w, http.StatusServiceUnavailable, "engine 未装配")
		return
	}
	if _, err := s.st.GetDelivery(r.Context(), id); err != nil {
		writeStoreErr(w, err, "交付不存在", "读取交付失败")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	mu := s.lockFor(id)
	mu.Lock()
	err := s.engine.Reject(r.Context(), id, body.Reason)
	mu.Unlock()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Reject 停车不重跑：回退阶段的重跑（带驳回意见反馈）在后台驱动。
	go func() {
		mu := s.lockFor(id)
		mu.Lock()
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

// runDelivery 异步驱动（创建时调用）：拿 per-delivery 锁后推进到稳定。
func (s *Server) runDelivery(deliveryID string) {
	mu := s.lockFor(deliveryID)
	mu.Lock()
	defer mu.Unlock()
	s.driveLocked(deliveryID)
}

// driveLocked 驱动引擎直到稳定（门禁 / 终态 / 出错），处理 unit_test 失败后的停车重试。
// 调用方必须已持有该 delivery 的锁。先查状态再驱动：已稳定（门禁/终态）时零调用。
func (s *Server) driveLocked(deliveryID string) {
	for i := 0; i < maxStarts; i++ {
		if s.engine == nil {
			return
		}
		d, err := s.st.GetDelivery(context.Background(), deliveryID)
		if err != nil || d.Status != "active" || d.PendingGate != "" {
			return // 门禁/终态：停
		}
		if err := s.engine.Start(context.Background(), deliveryID); err != nil {
			log.Printf("engine start %s: %v", deliveryID, err)
			return // 引擎报错：blocked/completed/agent 失败都由状态承载
		}
		// Start 后若仍 active 且无门禁 = unit_test 失败停车在 code_gen → 再 Start 重试
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
