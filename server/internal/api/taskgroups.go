package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"slices"

	"github.com/tokfinity/infera/internal/store"
)

// 本文件冻结「项目任务分组列表」契约（L202608221704-1-T01）：按项目 → 父任务
// → 子任务按阶段分组。前端项目任务页（L202608221704-2-T02）以本响应为唯一
// 数据源，不得静默变更形状。

// taskChild 子任务行：阶段组内的展示字段（stage=所属阶段：拆分子任务=批次
// wave 1..N，multica 同步镜像子任务=其 stage；status/current_stage/pending_gate
// 驱动行内徽标）。
type taskChild struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Stage           int       `json:"stage"`
	Status          string    `json:"status"`
	CurrentStage    string    `json:"current_stage"`
	PendingGate     string    `json:"pending_gate"`
	MulticaIssueID  string    `json:"multica_issue_id"`
	MulticaIssueKey string    `json:"multica_issue_key"`
	Assignee        string    `json:"assignee"`
	Priority        string    `json:"priority"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// taskStageGroup 一个阶段（批次）下的子任务集合：tasks 按创建时间升序。
type taskStageGroup struct {
	Stage int         `json:"stage"`
	Tasks []taskChild `json:"tasks"`
}

// taskGroupRow 父任务卡片：内联完整 Delivery（parent_id 空 = 顶层行）+
// 子任务分组摘要。stages 无子任务时为空数组（非 null）。
type taskGroupRow struct {
	store.Delivery
	ChildTotal     int              `json:"child_total"`
	ChildCompleted int              `json:"child_completed"`
	Stages         []taskStageGroup `json:"stages"`
}

// handleProjectTaskGroups 按项目返回「父任务 + 子任务按阶段分组」数据：
// 顶层行 = parent_id 为空的交付（按创建时间升序），其子任务按 wave 归组
// （阶段号升序）。子任务不重复出现在顶层。
func (s *Server) handleProjectTaskGroups(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	ds, err := s.st.ListProjectDeliveries(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务列表失败")
		return
	}
	writeJSON(w, http.StatusOK, buildTaskGroups(ds))
}

// buildTaskGroups 由项目交付平表（created_at 升序）装配分组视图：
// 子任务先按父归集，再按 wave 分桶（桶内保持创建时间升序），阶段号升序输出。
func buildTaskGroups(ds []store.Delivery) []taskGroupRow {
	children := make(map[string][]store.Delivery)
	for _, d := range ds {
		if d.ParentID != "" {
			children[d.ParentID] = append(children[d.ParentID], d)
		}
	}
	rows := make([]taskGroupRow, 0)
	for _, d := range ds {
		if d.ParentID != "" {
			continue
		}
		row := taskGroupRow{Delivery: d, Stages: []taskStageGroup{}}
		kids := children[d.ID]
		buckets := make(map[int][]taskChild)
		for _, k := range kids {
			buckets[k.Wave] = append(buckets[k.Wave], taskChild{
				ID: k.ID, Title: k.Title, Stage: k.Wave, Status: k.Status,
				CurrentStage: k.CurrentStage, PendingGate: k.PendingGate,
				MulticaIssueID: k.MulticaIssueID, MulticaIssueKey: k.MulticaIssueKey,
				Assignee: k.Assignee, Priority: k.Priority,
				CreatedAt: k.CreatedAt, UpdatedAt: k.UpdatedAt,
			})
			row.ChildTotal++
			if k.Status == "completed" {
				row.ChildCompleted++
			}
		}
		waves := make([]int, 0, len(buckets))
		for w := range buckets {
			waves = append(waves, w)
		}
		slices.Sort(waves)
		for _, w := range waves {
			row.Stages = append(row.Stages, taskStageGroup{Stage: w, Tasks: buckets[w]})
		}
		rows = append(rows, row)
	}
	return rows
}
