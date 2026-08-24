// 需求发现视图（INFERA-225 冻结契约）：
//
//	Get /api/discovery-tasks[?agent=mining|analysis]   两类 agent 任务列表
//
// agent 类型识别（最终采用字段）：上游两类 agent 的工作流是标签驱动的——
// 需求挖掘建【情报】卡、需求分析把有价值的转【候选】，且任务同步不落 creator，
// assignee 也无法区分两类（情报卡创建即指派给需求分析）。因此以 INFERA-217
// 已同步进 infera 的标签判定：mining=「情报」、analysis=「候选」。标签名按
// workspace 治理对象冻结为常量（同 syncsvc 的 autoLabelName 先例）。
//
// 响应复用现有任务查询模型：行内嵌 store.Delivery 全字段平铺（与 task-groups
// 顶层行同款），外加 agent_types（该卡命中的类型全集，固定 [mining, analysis]
// 序）、project_name（跨项目展示）、labels（INFERA-218 冻结形状 name+color）。
// 后续前端任务（INFERA-222 Layer 2）读本文件对齐，不得另开平行入口或类型。
package api

import (
	"net/http"

	"github.com/tokfinity/infera/internal/store"
)

// agent 类型取值（查询参数 agent 的合法词表）与判定标签名（上游 workspace
// 治理对象，按名引用不代建——同 syncsvc 的 autoLabelName 先例）。
const (
	agentTypeMining   = "mining"   // 需求挖掘
	agentTypeAnalysis = "analysis" // 需求分析
	miningLabelName   = "情报"       // 需求挖掘产出
	analysisLabelName = "候选"       // 需求分析晋级
)

// agentTypeByLabel 与 labelByAgentType：agent 类型 ↔ 判定标签名（双向冻结）。
var (
	agentTypeByLabel = map[string]string{
		miningLabelName:   agentTypeMining,
		analysisLabelName: agentTypeAnalysis,
	}
	labelByAgentType = map[string]string{
		agentTypeMining:   miningLabelName,
		agentTypeAnalysis: analysisLabelName,
	}
)

// discoveryAgentTypes 全部 agent 类型（固定序：响应 agent_types 与缺省合并
// 取回都按此序）。
var discoveryAgentTypes = []string{agentTypeMining, agentTypeAnalysis}

// discoveryTaskRow 需求发现列表行：store.Delivery 内嵌平铺（复用现有任务
// 查询模型，非平行投影）+ 视图装配字段。
type discoveryTaskRow struct {
	store.Delivery
	AgentTypes  []string    `json:"agent_types"`  // 该卡命中的 agent 类型全集（可两项）
	ProjectName string      `json:"project_name"` // 跨项目展示（JOIN 语义，装配期带出）
	Labels      []labelJSON `json:"labels"`
}

// handleDiscoveryTasks 按两类 agent 类型取任务列表：agent 省略 = 合并取回
// （两类并集）；可重复传参显式并集；未知取值 400。排序沿用存储面约定
// （updated_at 降序）。
func (s *Server) handleDiscoveryTasks(w http.ResponseWriter, r *http.Request) {
	seen := map[string]bool{}
	for _, v := range r.URL.Query()["agent"] {
		if v == "" {
			continue
		}
		if _, ok := labelByAgentType[v]; !ok {
			writeError(w, http.StatusBadRequest, "agent 只支持 mining|analysis")
			return
		}
		seen[v] = true
	}
	if len(seen) == 0 {
		for _, t := range discoveryAgentTypes {
			seen[t] = true // 缺省：合并取回
		}
	}
	names := make([]string, 0, len(discoveryAgentTypes))
	for _, t := range discoveryAgentTypes { // 固定序，查询入参可复现
		if seen[t] {
			names = append(names, labelByAgentType[t])
		}
	}

	ds, err := s.st.ListDeliveriesByLabelNames(r.Context(), names)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务列表失败")
		return
	}
	labels, err := s.st.LabelsByDeliveryID(r.Context(), deliveryIDs(ds))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务标签失败")
		return
	}
	projects, err := s.st.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目失败")
		return
	}
	projectName := make(map[string]string, len(projects))
	for _, p := range projects {
		projectName[p.ID] = p.Name
	}
	writeJSON(w, http.StatusOK, buildDiscoveryRows(ds, labels, projectName))
}

// buildDiscoveryRows 装配响应行：agent_types 按行上标签重新计算（含未被
// 本次过滤的类型——前端分组/筛选需要全集而非仅命中项）；labels 恒为数组。
func buildDiscoveryRows(ds []store.Delivery, labels map[string][]store.Label, projectName map[string]string) []discoveryTaskRow {
	rows := make([]discoveryTaskRow, 0, len(ds))
	for _, d := range ds {
		hit := make(map[string]bool, len(discoveryAgentTypes))
		for _, l := range labels[d.ID] {
			if t, ok := agentTypeByLabel[l.Name]; ok {
				hit[t] = true
			}
		}
		types := make([]string, 0, len(discoveryAgentTypes))
		for _, t := range discoveryAgentTypes {
			if hit[t] {
				types = append(types, t)
			}
		}
		rows = append(rows, discoveryTaskRow{
			Delivery:    d,
			AgentTypes:  types,
			ProjectName: projectName[d.ProjectID],
			Labels:      labelsJSON(labels[d.ID]),
		})
	}
	return rows
}
