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
// INFERA-228 / L202608241931-1-T01 复核冻结左侧列表口径：父子层级（顶层行即
// 父任务，子任务嵌于 stages）、stage（子行 stage=wave + current_stage，父行
// current_stage）、status（父子行均带）三类信息本响应已齐备，零接口改动；
// 见 TestProjectTaskGroupsLeftListContract 回归钉。

// taskChild 子任务行：阶段组内的展示字段（stage=所属阶段：拆分子任务=批次
// wave 1..N，任务同步镜像子任务=其 stage；status/current_stage/pending_gate
// 驱动行内徽标）。
type taskChild struct {
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Stage            int         `json:"stage"`
	Status           string      `json:"status"`
	CurrentStage     string      `json:"current_stage"`
	PendingGate      string      `json:"pending_gate"`
	ExternalIssueID  string      `json:"external_issue_id"`
	ExternalIssueKey string      `json:"external_issue_key"`
	Assignee         string      `json:"assignee"`
	Priority         string      `json:"priority"`
	Labels           []labelJSON `json:"labels"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
}

// taskStageGroup 一个阶段（批次）下的子任务集合：tasks 按创建时间升序。
// stage=0 表示「无阶段」组（任务同步镜像无 stage 的子任务），排在编号
// 阶段之后；JSON 形状不变，前端按值渲染分组标题。
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
	Labels         []labelJSON      `json:"labels"`
	Stages         []taskStageGroup `json:"stages"`
}

// handleProjectTaskGroups 按项目返回「父任务 + 子任务按阶段分组」数据：
// 顶层行 = parent_id 为空的交付（按创建时间升序），其子任务按 wave 归组
// （编号阶段升序，无阶段 wave 0 分组垫底）。子任务不重复出现在顶层。
// 每个交付行（顶层与子任务）都带挂的标签（INFERA-218 冻结形状 name+color），
// 一次批量查询装配，免 N+1。
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
	byID, err := s.st.LabelsByDeliveryID(r.Context(), deliveryIDs(ds))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务标签失败")
		return
	}
	writeJSON(w, http.StatusOK, buildTaskGroups(ds, byID))
}

// buildTaskGroups 由项目交付平表（created_at 升序）装配分组视图：
// 子任务先按父归集，再按 wave 分桶（桶内保持创建时间升序），编号阶段升序
// 输出，无阶段（wave 0）分组垫底。labels 为空时输出空数组（非 null）。
func buildTaskGroups(ds []store.Delivery, labels map[string][]store.Label) []taskGroupRow {
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
		row := taskGroupRow{Delivery: d, Stages: []taskStageGroup{}, Labels: labelsJSON(labels[d.ID])}
		kids := children[d.ID]
		buckets := make(map[int][]taskChild)
		for _, k := range kids {
			buckets[k.Wave] = append(buckets[k.Wave], taskChild{
				ID: k.ID, Title: k.Title, Stage: k.Wave, Status: k.Status,
				CurrentStage: k.CurrentStage, PendingGate: k.PendingGate,
				ExternalIssueID: k.ExternalIssueID, ExternalIssueKey: k.ExternalIssueKey,
				Assignee: k.Assignee, Priority: k.Priority,
				Labels:    labelsJSON(labels[k.ID]),
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
		// 编号阶段升序；无阶段（wave 0）视为最大值垫底——朴素升序会把它排到
		// 阶段 1 之前，无阶段子任务混进编号序列头部。
		slices.SortFunc(waves, func(a, b int) int {
			switch {
			case a == 0 && b != 0:
				return 1
			case a != 0 && b == 0:
				return -1
			default:
				return a - b
			}
		})
		for _, w := range waves {
			row.Stages = append(row.Stages, taskStageGroup{Stage: w, Tasks: buckets[w]})
		}
		rows = append(rows, row)
	}
	return rows
}
