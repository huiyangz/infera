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
	if err := decode(w, r, &body); err != nil {
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
	// 配置预校验：cli 必有命令、http 必有 URL、docker 必有镜像（提前于交付期暴露）
	if err := orchestration.ValidateConfig(body.Runner, body.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
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
	writeJSON(w, http.StatusCreated, a)
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
	if err := decode(w, r, &body); err != nil {
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
	// 合并结果预校验（runner 与 config 匹配；换 runner 未补 config 在这里拦下）
	if err := orchestration.ValidateConfig(a.Runner, a.Config); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.st.UpdateAgent(r.Context(), a); err != nil {
		if errors.Is(err, store.ErrConflict) {
			// 同名冲突或读-改-写窗口内被并发修改（乐观锁）——都要求刷新后重试。
			writeError(w, http.StatusConflict, "同名 agent 已存在，或已被并发修改——请刷新后重试")
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

// bindingNodesFor 扫出引用某 agent 的绑定位置（"项目名/code_gen"）。
// 绑定用 ListAllBindings 一次全量带回（项目逐个查是 N+1）。
func (s *Server) bindingNodesFor(r *http.Request, agentID string) ([]string, error) {
	ctx := r.Context()
	projs, err := s.st.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, p := range projs {
		names[p.ID] = p.Name
	}
	all, err := s.st.ListAllBindings(ctx)
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, b := range all {
		if b.AgentID != agentID {
			continue
		}
		loc := names[b.ProjectID] + "/" + b.Node
		if !seen[loc] {
			seen[loc] = true
			out = append(out, loc)
		}
	}
	return out, nil
}

// --- 项目编排（项目绑定是唯一绑定来源——全局默认编排已删除）---

func (s *Server) getProjectPipeline(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	if !validID(w, projectID) {
		return
	}
	if _, err := s.st.GetProject(r.Context(), projectID); err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	ovs, err := s.st.ListBindings(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目绑定失败")
		return
	}
	bindings := map[string]string{}
	for _, b := range ovs {
		bindings[b.Node] = b.AgentID
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":    orchestration.BindableNodes,
		"bindings": bindings,
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
	if err := decode(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if body.Bindings == nil {
		body.Bindings = map[string]string{}
	}
	// 校验 + 单事务全量替换覆盖（空集合 = 清空；无半写）
	if err := orchestration.SaveBindings(r.Context(), s.st, projectID, body.Bindings); err != nil {
		saveBindingsErr(w, err)
		return
	}
	s.getProjectPipeline(w, r)
}

// saveBindingsErr 统一映射 SaveBindings 错误：校验失败 → 400（原因可直接展示），其余 → 500。
func saveBindingsErr(w http.ResponseWriter, err error) {
	var invalid *orchestration.ErrInvalidBinding
	if errors.As(err, &invalid) {
		writeError(w, http.StatusBadRequest, invalid.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "写入绑定失败")
}
