package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/store"
)

// validRunners runner 枚举（与 orchestration.RunnerFor 对齐）。
var validRunners = map[string]bool{"cli": true, "http": true, "docker": true, "local": true}

// --- agents CRUD ---

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.st.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 agent 列表失败")
		return
	}
	if agents == nil {
		agents = []store.Agent{}
	}
	writeJSON(w, http.StatusOK, agents)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string         `json:"name"`
		Runner string         `json:"runner"`
		Config map[string]any `json:"config"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "agent 名称不能为空")
		return
	}
	if !validRunners[body.Runner] {
		writeError(w, http.StatusBadRequest, "runner 必须是 cli/http/docker/local 之一")
		return
	}
	if body.Config == nil {
		body.Config = map[string]any{}
	}
	a := &store.Agent{Name: strings.TrimSpace(body.Name), Runner: body.Runner, Config: body.Config}
	if err := s.st.CreateAgent(r.Context(), a); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "同名 agent 已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建 agent 失败")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) patchAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	var body struct {
		Name   *string        `json:"name"`
		Runner *string        `json:"runner"`
		Config map[string]any `json:"config"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	a, err := s.st.GetAgent(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err, "agent 不存在", "读取 agent 失败")
		return
	}
	if body.Name != nil {
		if strings.TrimSpace(*body.Name) == "" {
			writeError(w, http.StatusBadRequest, "agent 名称不能为空")
			return
		}
		a.Name = strings.TrimSpace(*body.Name)
	}
	if body.Runner != nil {
		if !validRunners[*body.Runner] {
			writeError(w, http.StatusBadRequest, "runner 必须是 cli/http/docker/local 之一")
			return
		}
		a.Runner = *body.Runner
	}
	if body.Config != nil {
		a.Config = body.Config
	}
	if err := s.st.UpdateAgent(r.Context(), a); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "同名 agent 已存在")
			return
		}
		writeStoreErr(w, err, "agent 不存在", "更新 agent 失败")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	if err := s.st.DeleteAgent(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 列出引用位置：默认 + 各项目覆盖
			nodes, _ := s.bindingNodesFor(r, id)
			writeError(w, http.StatusConflict, "agent 仍被绑定引用: "+strings.Join(nodes, ", "))
			return
		}
		writeStoreErr(w, err, "agent 不存在", "删除 agent 失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// bindingNodesFor 扫出引用某 agent 的绑定位置（"默认/test_gen"、"项目名/code_gen"）。
func (s *Server) bindingNodesFor(r *http.Request, agentID string) ([]string, error) {
	ctx := r.Context()
	projs, err := s.st.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{"": "默认"}
	for _, p := range projs {
		names[p.ID] = p.Name
	}
	var out []string
	seen := map[string]bool{}
	appendRefs := func(bs []store.PipelineBinding) {
		for _, b := range bs {
			if b.AgentID != agentID {
				continue
			}
			loc := names[b.ProjectID] + "/" + b.Node
			if !seen[loc] {
				seen[loc] = true
				out = append(out, loc)
			}
		}
	}
	defs, err := s.st.ListBindings(ctx, "")
	if err != nil {
		return nil, err
	}
	appendRefs(defs)
	for _, p := range projs {
		ovs, err := s.st.ListBindings(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		appendRefs(ovs)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// --- 全局默认编排 ---

func (s *Server) getPipeline(w http.ResponseWriter, r *http.Request) {
	agents, err := s.st.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 agent 列表失败")
		return
	}
	defs, err := s.st.ListBindings(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取默认绑定失败")
		return
	}
	bindings := map[string]string{}
	for _, b := range defs {
		bindings[b.Node] = b.AgentID
	}
	if agents == nil {
		agents = []store.Agent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":    orchestration.BindableNodes,
		"agents":   agents,
		"bindings": bindings,
	})
}

func (s *Server) putPipeline(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bindings map[string]string `json:"bindings"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	// 全量替换默认：必须覆盖全部可绑定节点
	var missing []string
	for _, n := range orchestration.BindableNodes {
		if body.Bindings[n] == "" {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		writeError(w, http.StatusBadRequest, "默认编排必须覆盖全部节点，缺少: "+strings.Join(missing, ", "))
		return
	}
	// 节点合法性 + agent 存在性
	agents, err := s.st.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 agent 列表失败")
		return
	}
	exists := map[string]bool{}
	for _, a := range agents {
		exists[a.ID] = true
	}
	for node, id := range body.Bindings {
		if !bindable(node) {
			writeError(w, http.StatusBadRequest, "不可绑定的节点: "+node)
			return
		}
		if !exists[id] {
			writeError(w, http.StatusBadRequest, "节点 "+node+" 引用了不存在的 agent")
			return
		}
	}
	for _, n := range orchestration.BindableNodes {
		if err := s.st.UpsertBinding(r.Context(), &store.PipelineBinding{Node: n, AgentID: body.Bindings[n]}); err != nil {
			writeError(w, http.StatusInternalServerError, "写入绑定失败")
			return
		}
	}
	s.getPipeline(w, r)
}

// --- 项目编排 ---

func (s *Server) getProjectPipeline(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	defs, err := s.st.ListBindings(r.Context(), "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取默认绑定失败")
		return
	}
	ovs, err := s.st.ListBindings(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目绑定失败")
		return
	}
	defaults := map[string]string{}
	for _, b := range defs {
		defaults[b.Node] = b.AgentID
	}
	overrides := map[string]string{}
	for _, b := range ovs {
		overrides[b.Node] = b.AgentID
	}
	// effective：项目覆盖 ?? 默认（不报缺失——前端按缺省展示）
	effective := map[string]orchestration.Effective{}
	for _, n := range orchestration.BindableNodes {
		if id, ok := overrides[n]; ok {
			effective[n] = orchestration.Effective{Node: n, AgentID: id, From: "project"}
		} else if id, ok := defaults[n]; ok {
			effective[n] = orchestration.Effective{Node: n, AgentID: id, From: "default"}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":     orchestration.BindableNodes,
		"defaults":  defaults,
		"overrides": overrides,
		"effective": effective,
	})
}

func (s *Server) putProjectPipeline(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	var body struct {
		Bindings map[string]string `json:"bindings"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if body.Bindings == nil {
		body.Bindings = map[string]string{}
	}
	agents, err := s.st.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 agent 列表失败")
		return
	}
	exists := map[string]bool{}
	for _, a := range agents {
		exists[a.ID] = true
	}
	for node, id := range body.Bindings {
		if !bindable(node) {
			writeError(w, http.StatusBadRequest, "不可绑定的节点: "+node)
			return
		}
		if !exists[id] {
			writeError(w, http.StatusBadRequest, "节点 "+node+" 引用了不存在的 agent")
			return
		}
	}
	// 全量替换覆盖：旧的、不在新表里的逐个删
	current, err := s.st.ListBindings(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目绑定失败")
		return
	}
	for _, b := range current {
		if _, keep := body.Bindings[b.Node]; !keep {
			if err := s.st.DeleteBinding(r.Context(), projectID, b.Node); err != nil && !errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusInternalServerError, "删除绑定失败")
				return
			}
		}
	}
	for node, id := range body.Bindings {
		if err := s.st.UpsertBinding(r.Context(), &store.PipelineBinding{ProjectID: projectID, Node: node, AgentID: id}); err != nil {
			writeError(w, http.StatusInternalServerError, "写入绑定失败")
			return
		}
	}
	s.getProjectPipeline(w, r)
}

func bindable(node string) bool {
	for _, n := range orchestration.BindableNodes {
		if n == node {
			return true
		}
	}
	return false
}
