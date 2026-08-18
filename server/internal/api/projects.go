package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

// maxBodyBytes 请求体上限：所有 decode 的 JSON body 不得超过 1MiB——
// 超限在读入时即截断报错（内存放大防护），不整包进内存再解析。
const maxBodyBytes = 1 << 20

// decode 统一 JSON 解码入口：http.MaxBytesReader 限长后解码。
// 超限/畸形 body 返回 error，调用方统一 400。
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v)
}

// validRepoURL repo_url 白名单：https、ssh（含 git@ scp 形态）与本地绝对路径
// （开发/自托管）。其余 scheme（http/file/ftp/git 等）在 LsRemote 可达性校验之前
// 直接拒绝——服务端不得被指去请求任意识别名/协议（SSRF-adjacent）。
func validRepoURL(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "https://"),
		strings.HasPrefix(raw, "ssh://"),
		strings.HasPrefix(raw, "git@"),
		strings.HasPrefix(raw, "/"):
		return true
	}
	return false
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
	if err := decode(w, r, &body); err != nil {
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
	// 暂不支持绿地项目：必须绑定仓库。
	if strings.TrimSpace(body.RepoURL) == "" {
		writeError(w, http.StatusBadRequest, "必须绑定 Git 仓库（暂不支持绿地项目）")
		return
	}
	body.RepoURL = strings.TrimSpace(body.RepoURL)
	// scheme 白名单先于可达性校验：服务端不得被指去请求任意识/协议（SSRF-adjacent）。
	if !validRepoURL(body.RepoURL) {
		writeError(w, http.StatusBadRequest, "仓库地址必须是 https、ssh 或本地绝对路径")
		return
	}
	// 注入了 git checker 时做毫秒级可达性校验。原始错误只进日志（可能含
	// 服务器本地路径等内部信息），客户端收固定文案。
	if s.g != nil {
		if err := s.g.LsRemote(r.Context(), body.RepoURL); err != nil {
			log.Printf("create project %q: ls-remote %s: %v", body.Name, body.RepoURL, err)
			writeError(w, http.StatusBadRequest, "仓库不可达或无权限（地址与访问凭据是否正确？）")
			return
		}
	}
	p := &store.Project{Name: body.Name, RepoURL: body.RepoURL, DefaultBranch: body.DefaultBranch}
	if err := s.st.CreateProject(r.Context(), p); err != nil {
		writeError(w, http.StatusInternalServerError, "创建项目失败")
		return
	}
	writeJSON(w, http.StatusCreated, p)
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
	if err := decode(w, r, &body); err != nil {
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
