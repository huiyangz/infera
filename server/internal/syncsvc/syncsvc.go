// Package syncsvc 编排 Multica → infera 的一次性全量同步：
// 拉取（T01 client 面 ListProjects/ListIssues）→ 映射（T01 纯映射 MapProject/
// MapIssue）→ 幂等导入（T02 upsert 面 UpsertProjectByMulticaID/
// UpsertDeliveryByMulticaID）。只消费两张已冻结的面，不另立入口。
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
	"github.com/tokfinity/infera/internal/multica"
	"github.com/tokfinity/infera/internal/store"
)

// Fetcher 是 T01 拉取面的最小依赖（*multica.Client 天然满足）。
type Fetcher interface {
	ListProjects(ctx context.Context) ([]multica.Project, error)
	ListIssues(ctx context.Context) ([]multica.Issue, error)
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
	MulticaIssueID string `json:"multica_issue_id"`
	IssueKey       string `json:"issue_key"`
	Reason         string `json:"reason"`
}

// Result 一轮同步的结果（GET /api/multica/sync 的载荷形状，T04/T05 消费面）。
// Error 非空 = 本轮中途失败（拉取或落库错误）；此时计数为已完成的部份值。
type Result struct {
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	ProjectsImported int       `json:"projects_imported"`
	IssuesImported   int       `json:"issues_imported"`
	IssuesSkipped    int       `json:"issues_skipped"`
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
		return record(fmt.Errorf("拉取 multica 项目失败: %w", err))
	}
	issues, err := s.fetch.ListIssues(ctx)
	if err != nil {
		return record(fmt.Errorf("拉取 multica issue 失败: %w", err))
	}

	// 项目导入：仅落 multica 标题（store.Project 无 description/status/lead 列，
	// repo_url/default_branch/pinned 归 infera 侧配置，T02 冻结面不覆盖）。
	projInternal := make(map[string]string, len(projects)) // multica 项目 id → infera 项目 id
	for _, p := range projects {
		snap := multica.MapProject(p)
		sp := &store.Project{Name: snap.Title, MulticaProjectID: snap.ExternalID}
		if err := s.st.UpsertProjectByMulticaID(ctx, sp); err != nil {
			return record(fmt.Errorf("导入 multica 项目 %s 失败: %w", snap.ExternalID, err))
		}
		projInternal[snap.ExternalID] = sp.ID
		res.ProjectsImported++
	}

	// issue 映射 + 冒烟单过滤。
	snaps := make(map[string]multica.IssueSnapshot, len(issues))
	order := make([]string, 0, len(issues)) // 保输入序，结果可复现
	for _, i := range issues {
		if strings.Contains(i.Title, "[infera-e2e]") {
			res.Skips = append(res.Skips, Skip{MulticaIssueID: i.ID, IssueKey: i.Identifier, Reason: skipSmoke})
			res.IssuesSkipped++
			continue
		}
		snap := multica.MapIssue(i)
		snaps[snap.ExternalID] = snap
		order = append(order, snap.ExternalID)
	}

	// 父先子后排序：顶层/父不在导入集/父已定（导入或跳过）→ 可处理；
	// 一轮下来无人可放置（互相等父）= 成环，环上成员跳过。
	internal := make(map[string]string, len(order)) // multica issue id → infera delivery id（已导入的）
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
			if err := s.importIssue(ctx, &res, snaps[id], projInternal, internal); err != nil {
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
		res.Skips = append(res.Skips, Skip{MulticaIssueID: id, IssueKey: snaps[id].Identifier, Reason: skipParentLoop})
		res.IssuesSkipped++
	}
	return record(nil)
}

// importIssue 导入单条 issue 快照；无项目可落 → 计入 Skips 后返回（不视为
// 错误）。父未导入（不在 internal）→ 折叠为顶层（wave=0、无父）。
func (s *Service) importIssue(ctx context.Context, res *Result, snap multica.IssueSnapshot,
	projInternal, internal map[string]string) error {
	pid, ok := projInternal[snap.ProjectExternalID]
	if !ok {
		res.Skips = append(res.Skips, Skip{MulticaIssueID: snap.ExternalID, IssueKey: snap.Identifier, Reason: skipNoProject})
		res.IssuesSkipped++
		return nil
	}
	d := &store.Delivery{
		ProjectID:       pid,
		Title:           snap.Title,
		Description:     snap.Description,
		Status:          translateStatus(snap.Status),
		MulticaIssueID:  snap.ExternalID,
		MulticaIssueKey: snap.Identifier,
		Assignee:        actorDisplay(snap.Assignee),
		Priority:        snap.Priority,
	}
	if parentInt := internal[snap.ParentExternalID]; snap.ParentExternalID != "" && parentInt != "" {
		d.ParentID = parentInt
		d.Wave = 1 // 子需求 wave>=1（字段约定：父/普通需求=0）
	}
	if err := s.st.UpsertDeliveryByMulticaID(ctx, d); err != nil {
		return fmt.Errorf("导入 multica issue %s 失败: %w", snap.ExternalID, err)
	}
	internal[snap.ExternalID] = d.ID
	res.IssuesImported++
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

// translateStatus 把 multica issue 状态翻译为 infera 需求状态（翻译语义归
// 本消费方，T01 不发明对照表）。铁律：任何输入都不翻出 active——active 意味
// "引擎正在驱动"（重启恢复 ResumeActive 会对全部 active 交付点火后台驱动），
// 同步镜像被点火等于替镜像跑引擎。非终态一律 queued（镜像只排队不驱动）。
func translateStatus(multicaStatus string) string {
	switch multicaStatus {
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
func actorDisplay(a multica.ActorRef) string {
	if a.Type == "" || a.ID == "" {
		return ""
	}
	return a.Type + ":" + a.ID
}
