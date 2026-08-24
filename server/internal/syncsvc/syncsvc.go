// Package syncsvc 编排上游任务平台 → infera 的一次性全量同步：
// 拉取（T01 client 面 ListProjects/ListIssues/ListProjectResources）→ 映射
// （T01 纯映射 MapProject/MapIssue + 资源→repo_url 解析 resolveRepoURL）→
// 幂等导入（T02 upsert 面 UpsertProjectByExternalID/UpsertDeliveryByExternalID）。
// 只消费两张已冻结的面，不另立入口。
package syncsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/tasksource"
)

// Fetcher 是 T01 拉取面的最小依赖（*tasksource.Client 天然满足）。
type Fetcher interface {
	ListProjects(ctx context.Context) ([]tasksource.Project, error)
	ListIssues(ctx context.Context) ([]tasksource.Issue, error)
	ListLabels(ctx context.Context) ([]tasksource.Label, error)
	ListProjectResources(ctx context.Context, projectID string) ([]tasksource.ProjectResource, error)
}

// ErrSyncRunning 已有同步在进行（POST 触发互斥；GET 可看 running 状态）。
var ErrSyncRunning = errors.New("syncsvc: 已有同步在进行")

// skipReason 取值。
const (
	skipSmoke      = "smoke"        // 标题含 [infera-e2e] 的自动化冒烟单
	skipNoProject  = "no_project"   // issue 未挂项目（deliveries.project_id NOT NULL，无处可落）
	skipParentLoop = "parent_cycle" // 父子关系成环，无法排出导入顺序
)

// Skip 一条被跳过的 issue（不落库，但计数可审计）。
type Skip struct {
	ExternalIssueID string `json:"external_issue_id"`
	IssueKey        string `json:"issue_key"`
	Reason          string `json:"reason"`
}

// Result 一轮同步的结果（GET /api/task-sync 的载荷形状，T04/T05 消费面）。
// Error 非空 = 本轮中途失败（拉取或落库错误）；此时计数为已完成的部份值。
type Result struct {
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	ProjectsImported int       `json:"projects_imported"`
	IssuesImported   int       `json:"issues_imported"`
	IssuesSkipped    int       `json:"issues_skipped"`
	LabelsImported   int       `json:"labels_imported"` // 标签库本轮镜像的标签数（幂等：重复轮同值）
	Skips            []Skip    `json:"skips"`
	Error            string    `json:"error"`
}

// Service 同步编排器。一次构造多处复用；SyncNow 串行（运行中再触发 →
// ErrSyncRunning），Last 返回最近一轮结果（进程内存，重启即空——同步结果
// 不落库，存储面归 T02 冻结契约，无同步运行表）。
type Service struct {
	fetch Fetcher
	st    store.Store

	runMu   sync.Mutex   // TryLock 串行化 SyncNow
	running atomic.Bool  // GET 面的运行中标志（与 runMu 同进同出）
	lastMu  sync.RWMutex // last 的读写锁
	last    *Result
}

// New 构造同步服务。凭据不进 Service：client（含 token）由装配方从 env 配置
// 构造后以 Fetcher 注入，本包不接触、不存储、不输出凭据。
func New(f Fetcher, st store.Store) *Service {
	return &Service{fetch: f, st: st}
}

// SyncNow 执行一轮全量同步：拉全部项目与 issue → 映射 → 按父先子后导入。
// 跳过（smoke/no_project/parent_loop）计入 Result.Skips，不中断；拉取或
// 落库失败立即中止整轮（半轮结果也记录为最近结果）并返回错误。
func (s *Service) SyncNow(ctx context.Context) (Result, error) {
	if !s.runMu.TryLock() {
		return Result{}, ErrSyncRunning
	}
	s.running.Store(true)
	defer func() {
		s.running.Store(false)
		s.runMu.Unlock()
	}()

	res := Result{StartedAt: time.Now().UTC()}
	// record 收口本轮：无论成败都留 FinishedAt 与最近结果，错误带回调用方。
	record := func(err error) (Result, error) {
		res.FinishedAt = time.Now().UTC()
		if err != nil {
			res.Error = err.Error()
		}
		s.lastMu.Lock()
		cp := res
		s.last = &cp
		s.lastMu.Unlock()
		return cp, err
	}

	projects, err := s.fetch.ListProjects(ctx)
	if err != nil {
		return record(fmt.Errorf("拉取上游项目失败: %w", err))
	}
	issues, err := s.fetch.ListIssues(ctx)
	if err != nil {
		return record(fmt.Errorf("拉取上游 issue 失败: %w", err))
	}
	labels, err := s.fetch.ListLabels(ctx)
	if err != nil {
		return record(fmt.Errorf("拉取上游标签库失败: %w", err))
	}

	// 项目导入：落上游标题 + 仓库绑定（资源解析见 resolveRepoURL）。
	// store.Project 无 description/status/lead 列；default_branch/pinned 归
	// infera 侧配置，冲突分支不覆盖；repo_url 按覆写契约随绑定走（INFERA-175）。
	projInternal := make(map[string]string, len(projects)) // 上游项目 id → infera 项目 id
	for _, p := range projects {
		snap := tasksource.MapProject(p)
		resources, err := s.fetch.ListProjectResources(ctx, snap.ExternalID)
		if err != nil {
			return record(fmt.Errorf("拉取上游项目 %s 资源失败: %w", snap.ExternalID, err))
		}
		sp := &store.Project{Name: snap.Title, RepoURL: resolveRepoURL(resources), ExternalProjectID: snap.ExternalID}
		if err := s.st.UpsertProjectByExternalID(ctx, sp); err != nil {
			return record(fmt.Errorf("导入上游项目 %s 失败: %w", snap.ExternalID, err))
		}
		projInternal[snap.ExternalID] = sp.ID
		res.ProjectsImported++
	}

	// issue 映射 + 冒烟单过滤。
	snaps := make(map[string]tasksource.IssueSnapshot, len(issues))
	order := make([]string, 0, len(issues)) // 保输入序，结果可复现
	for _, i := range issues {
		if strings.Contains(i.Title, "[infera-e2e]") {
			res.Skips = append(res.Skips, Skip{ExternalIssueID: i.ID, IssueKey: i.Identifier, Reason: skipSmoke})
			res.IssuesSkipped++
			continue
		}
		snap := tasksource.MapIssue(i)
		snaps[snap.ExternalID] = snap
		order = append(order, snap.ExternalID)
	}

	// 标签库镜像：按上游标签 id 幂等 upsert（T01 冻结的幂等键），名称+颜色
	// 与上游一致。先于交付导入——挂标要拿 标签外部 id → infera 标签 id 的映射。
	labelInternal, err := s.importLabels(ctx, &res, labels, snaps)
	if err != nil {
		return record(err)
	}

	// 父先子后排序：顶层/父不在导入集/父已定（导入或跳过）→ 可处理；
	// 一轮下来无人可放置（互相等父）= 成环，环上成员跳过。
	internal := make(map[string]string, len(order)) // 上游 issue id → infera delivery id（已导入的）
	decided := make(map[string]bool, len(order))    // 已处理（导入或跳过）——子单等的是父的"定"，不是父的"导入"
	remaining := order
	for len(remaining) > 0 {
		var next []string
		progressed := false
		for _, id := range remaining {
			parent := snaps[id].ParentExternalID
			_, parentInSet := snaps[parent]
			if parent != "" && parentInSet && !decided[parent] {
				next = append(next, id)
				continue
			}
			if err := s.importIssue(ctx, &res, snaps[id], projInternal, internal, labelInternal); err != nil {
				return record(err)
			}
			decided[id] = true
			progressed = true
		}
		remaining = next
		if !progressed {
			break // 剩下的互相等待父单 = 环
		}
	}
	for _, id := range remaining {
		res.Skips = append(res.Skips, Skip{ExternalIssueID: id, IssueKey: snaps[id].Identifier, Reason: skipParentLoop})
		res.IssuesSkipped++
	}
	return record(nil)
}

// importIssue 导入单条 issue 快照；无项目可落 → 计入 Skips 后返回（不视为
// 错误）。父未导入（不在 internal）→ 折叠为顶层（wave=0、无父）。
func (s *Service) importIssue(ctx context.Context, res *Result, snap tasksource.IssueSnapshot,
	projInternal, internal, labelInternal map[string]string) error {
	pid, ok := projInternal[snap.ProjectExternalID]
	if !ok {
		res.Skips = append(res.Skips, Skip{ExternalIssueID: snap.ExternalID, IssueKey: snap.Identifier, Reason: skipNoProject})
		res.IssuesSkipped++
		return nil
	}
	d := &store.Delivery{
		ProjectID:        pid,
		Title:            snap.Title,
		Description:      snap.Description,
		Status:           translateStatus(snap.Status),
		ExternalIssueID:  snap.ExternalID,
		ExternalIssueKey: snap.Identifier,
		Assignee:         actorDisplay(snap.Assignee),
		Priority:         snap.Priority,
	}
	if parentInt := internal[snap.ParentExternalID]; snap.ParentExternalID != "" && parentInt != "" {
		d.ParentID = parentInt
		// 子任务的 stage 沿用原生多阶段表示（拆分批次 wave）：取上游 stage
		// 原值。字段约定：0 = 无阶段（父/普通需求同值域），编号阶段 1..N；无
		// stage 子任务 0 原样落库，显示层（任务组 API/前端）归入「无阶段」分组，
		// 引擎批次调度跳过 wave<=0。
		d.Wave = snap.Stage
	}
	if err := s.st.UpsertDeliveryByExternalID(ctx, d); err != nil {
		return fmt.Errorf("导入上游 issue %s 失败: %w", snap.ExternalID, err)
	}
	internal[snap.ExternalID] = d.ID
	res.IssuesImported++
	// 镜像交付挂标：与上游逐 issue 标签对齐（缺的挂上、上游已摘的同步摘除）。
	if err := s.reconcileDeliveryLabels(ctx, d.ID, snap.Labels, labelInternal); err != nil {
		return fmt.Errorf("镜像 issue %s 标签失败: %w", snap.ExternalID, err)
	}
	return nil
}

// importLabels 把上游 workspace 标签库镜像进 infera 标签库：按 T01 冻结的
// 幂等键（上游标签 id）upsert，名称+颜色与上游一致，重复轮不产生重复行。
// issue 引用了库拉取面未见过的标签时（两端点数据不一致），按 issue 内嵌的
// 标签对象兜底 upsert——同一 id 幂等命中同一行，不 fatal。返回 上游标签 id
// → infera 标签 id 的映射（挂标定位用）。
func (s *Service) importLabels(ctx context.Context, res *Result, library []tasksource.Label,
	snaps map[string]tasksource.IssueSnapshot) (map[string]string, error) {
	byExternal := make(map[string]tasksource.Label, len(library))
	for _, l := range library {
		if l.ID == "" {
			continue // 半截条目（空 id）：不是合法标签，跳过不硬造
		}
		byExternal[l.ID] = l
	}
	// 库外引用兜底：库条目优先（标签库端点是标签面的权威来源）。
	for _, snap := range snaps {
		for _, ref := range snap.Labels {
			if _, ok := byExternal[ref.ExternalID]; !ok {
				byExternal[ref.ExternalID] = tasksource.Label{ID: ref.ExternalID, Name: ref.Name, Color: ref.Color}
			}
		}
	}
	labelInternal := make(map[string]string, len(byExternal))
	for _, l := range byExternal {
		sl := &store.Label{Name: l.Name, Color: l.Color, ExternalLabelID: l.ID}
		if err := s.st.UpsertLabelByExternalID(ctx, sl); err != nil {
			return nil, fmt.Errorf("导入上游标签 %s(%s) 失败: %w", l.ID, l.Name, err)
		}
		labelInternal[l.ID] = sl.ID
	}
	res.LabelsImported = len(byExternal)
	return labelInternal, nil
}

// reconcileDeliveryLabels 全量镜像语义的挂标对齐：desired = 快照逐 issue 标签
// 对应的 infera 标签；current 限定在镜像域（同步来源的标签，ExternalLabelID
// 非空）——infera 侧手工挂的本地标签（外部 id 空）不归同步管，绝不摘除。
// 缺的挂上（AttachLabel 幂等）、上游已摘的摘掉；两轮之间只做差集，重复同步
// 不积累。DetachLabel 撞 ErrNotFound 视为已达目的（并发路径已摘）。
func (s *Service) reconcileDeliveryLabels(ctx context.Context, deliveryID string,
	refs []tasksource.LabelRef, labelInternal map[string]string) error {
	desired := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if internal, ok := labelInternal[ref.ExternalID]; ok {
			desired[internal] = true
		}
	}
	current, err := s.st.ListDeliveryLabels(ctx, deliveryID)
	if err != nil {
		return err
	}
	for _, l := range current {
		if l.ExternalLabelID == "" || desired[l.ID] {
			continue // 本地标签不动；上游仍挂着的也不动
		}
		if err := s.st.DetachLabel(ctx, deliveryID, l.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	for id := range desired {
		if err := s.st.AttachLabel(ctx, deliveryID, id); err != nil {
			return err
		}
	}
	return nil
}

// Running 是否有同步正在进行。
func (s *Service) Running() bool { return s.running.Load() }

// Last 最近一轮同步结果；从未同步过返回 nil。
func (s *Service) Last() *Result {
	s.lastMu.RLock()
	defer s.lastMu.RUnlock()
	if s.last == nil {
		return nil
	}
	cp := *s.last
	return &cp
}

// translateStatus 把 上游 issue 状态翻译为 infera 需求状态（翻译语义归
// 本消费方，T01 不发明对照表）。铁律：任何输入都不翻出 active——active 意味
// "引擎正在驱动"（重启恢复 ResumeActive 会对全部 active 交付点火后台驱动），
// 同步镜像被点火等于替镜像跑引擎。非终态一律 queued（镜像只排队不驱动）。
func translateStatus(externalStatus string) string {
	switch externalStatus {
	case "done", "cancelled": // cancelled 无 infera 对应词，按终态折叠
		return engine.StatusCompleted
	case "blocked":
		return engine.StatusBlocked
	default: // todo/backlog/in_progress/in_review/未知词
		return engine.StatusQueued
	}
}

// actorDisplay 负责人引用 → assignee 展示串（"type:id"，如 agent:<uuid>）。
// 拉取面不带姓名，解析展示归前端/后续消费方；无负责人 = 空串。
func actorDisplay(a tasksource.ActorRef) string {
	if a.Type == "" || a.ID == "" {
		return ""
	}
	return a.Type + ":" + a.ID
}

// resolveRepoURL 把上游项目资源解析为 repo_url（INFERA-175 冻结契约）：
// github_repo → 其 URL；local_directory → 其 local_path（git 可克隆本地路径，
// intake 按 clone 语义处理，不带 worktree/daemon 特殊模式）。择一：github_repo
// 优先于 local_directory；同类型取 position 最小；目标值为空的条目跳过。
// 解析不出（无资源/无可用条目）返回空串——由 upsert 冲突分支保留 infera 侧
// 现值，不清空。
func resolveRepoURL(resources []tasksource.ProjectResource) string {
	var ghURL, dirPath string
	ghPos, dirPos := 0, 0
	for _, r := range resources {
		switch r.ResourceType {
		case "github_repo":
			if r.Ref.URL == "" || (ghURL != "" && r.Position >= ghPos) {
				continue
			}
			ghURL, ghPos = r.Ref.URL, r.Position
		case "local_directory":
			if r.Ref.LocalPath == "" || (dirPath != "" && r.Position >= dirPos) {
				continue
			}
			dirPath, dirPos = r.Ref.LocalPath, r.Position
		}
	}
	if ghURL != "" {
		return ghURL
	}
	return dirPath
}
