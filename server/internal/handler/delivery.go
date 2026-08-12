package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/service"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryHandler struct {
	svc  *service.DeliveryService
	pool *pgxpool.Pool
}

func NewDeliveryHandler(pool *pgxpool.Pool) *DeliveryHandler {
	return &DeliveryHandler{svc: service.New(pool), pool: pool}
}

type createDeliveryReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
}

func (h *DeliveryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createDeliveryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, err := h.svc.Create(r.Context(), service.CreateInput{
		Title: req.Title, Description: req.Description, RepoURL: req.RepoURL, Branch: req.Branch,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (h *DeliveryHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := generated.New(h.pool)
	items, err := q.ListDeliveries(r.Context(), generated.ListDeliveriesParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DeliveryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q := generated.New(h.pool)
	d, err := q.GetDelivery(r.Context(), parseUUID(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	tl, _ := q.ListTimelineEvents(r.Context(), d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"delivery": d, "timeline": tl})
}

// parseUUID 把路径参数解析为 pgtype.UUID；非法输入返回 Valid=false 的零值，
// 使后续查询命中 no rows → handler 返回 404。
func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}
	}
	return u
}

// Advance 推进 delivery 到下一 stage（或完成）。
func (h *DeliveryHandler) Advance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := h.svc.Advance(r.Context(), parseUUID(id))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
