package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// projectRow 是列表行：内联 Project 字段 + 可选 stats。
type projectRow struct {
	store.Project
	Stats *store.ProjectStats `json:"stats"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.st.ListProjects(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取项目失败")
		return
	}
	withStats := r.URL.Query().Get("include") == "stats"
	rows := make([]projectRow, len(projects))
	for i, p := range projects {
		rows[i] = projectRow{Project: p}
		if withStats {
			st, err := s.st.ProjectStats(r.Context(), p.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "读取统计失败")
				return
			}
			rows[i].Stats = &st
		}
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		writeError(w, http.StatusBadRequest, "项目名不能为空")
		return
	}
	if body.DefaultBranch == "" {
		body.DefaultBranch = "main"
	}
	// repo_url 非空且注入了 git checker 时，先做毫秒级可达性校验。
	if body.RepoURL != "" && s.g != nil {
		if err := s.g.LsRemote(r.Context(), body.RepoURL); err != nil {
			writeError(w, http.StatusBadRequest, "仓库不可达或无权限: "+err.Error())
			return
		}
	}
	p := &store.Project{Name: body.Name, RepoURL: body.RepoURL, DefaultBranch: body.DefaultBranch}
	if err := s.st.CreateProject(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "创建项目失败")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	p, err := s.st.GetProject(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validID(w, id) {
		return
	}
	var body struct {
		Pinned bool `json:"pinned"`
	}
	if err := decode(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不合法")
		return
	}
	if err := s.st.PatchProjectPinned(r.Context(), id, body.Pinned); err != nil {
		writeStoreErr(w, err, "项目不存在", "更新项目失败")
		return
	}
	p, err := s.st.GetProject(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err, "项目不存在", "读取项目失败")
		return
	}
	writeJSON(w, http.StatusOK, p)
}
