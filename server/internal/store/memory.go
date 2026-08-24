package store

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is an in-memory Store implementation for tests.
type Memory struct {
	mu             sync.Mutex
	projects       map[string]*Project
	deliveries     map[string]*Delivery
	events         map[string][]*Event
	artifacts      map[string][]*Artifact
	stageRuns      map[string][]*StageRun
	agents         map[string]*Agent
	bindings       map[string]*PipelineBinding // key: projectID + "\x00" + node
	labels         map[string]*Label
	deliveryLabels map[string]map[string]time.Time // deliveryID → labelID → 挂标时间
}

func NewMemory() *Memory {
	return &Memory{
		projects:       map[string]*Project{},
		deliveries:     map[string]*Delivery{},
		events:         map[string][]*Event{},
		artifacts:      map[string][]*Artifact{},
		stageRuns:      map[string][]*StageRun{},
		agents:         map[string]*Agent{},
		bindings:       map[string]*PipelineBinding{},
		labels:         map[string]*Label{},
		deliveryLabels: map[string]map[string]time.Time{},
	}
}

// projects

func (m *Memory) CreateProject(ctx context.Context, p *Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	cp := *p
	m.projects[cp.ID] = &cp
	return nil
}

func (m *Memory) ListProjects(ctx context.Context) ([]Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Project, 0, len(m.projects))
	for _, p := range m.projects {
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b Project) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

func (m *Memory) GetProject(ctx context.Context, id string) (*Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *Memory) PatchProjectPinned(ctx context.Context, id string, pinned bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return ErrNotFound
	}
	p.Pinned = pinned
	p.UpdatedAt = time.Now().UTC()
	return nil
}

// UpsertProjectByExternalID 按 上游项目 ID 幂等导入（同步链路唯一入口，语义与 Pg 一致）：
// 不存在则插入（整行走入参）、存在则更新外部来源字段 name 与（非空时的）repo_url——
// repo_url 覆写契约（INFERA-175）：上游侧解析出绑定（非空）覆写现值，解析不出
// （空）保留现值不清空；default_branch/pinned 归 infera 侧配置，冲突分支不覆盖。
// 重复执行不产生重复行。ExternalProjectID 为空 → ErrInvalid。
func (m *Memory) UpsertProjectByExternalID(_ context.Context, p *Project) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ExternalProjectID == "" {
		return ErrInvalid
	}
	now := time.Now().UTC()
	for _, ex := range m.projects {
		if ex.ExternalProjectID != p.ExternalProjectID {
			continue
		}
		ex.Name = p.Name
		if p.RepoURL != "" {
			ex.RepoURL = p.RepoURL
		}
		ex.UpdatedAt = now
		ex.ExternalSyncedAt = &now
		*p = *ex
		return nil
	}
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p.CreatedAt = now
	p.UpdatedAt = now
	p.ExternalSyncedAt = &now
	cp := *p
	m.projects[cp.ID] = &cp
	return nil
}

func (m *Memory) ProjectStats(ctx context.Context, id string) (ProjectStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return ProjectStats{}, ErrNotFound
	}
	s := ProjectStats{Last: p.UpdatedAt}
	for _, d := range m.deliveries {
		if d.ProjectID != id {
			continue
		}
		if d.Status == "active" {
			s.Active++
		}
		if d.PendingGate != "" {
			s.Pending++
		}
		if d.UpdatedAt.After(s.Last) {
			s.Last = d.UpdatedAt
		}
	}
	return s, nil
}

// RequirementStats 项目维度需求统计（语义与 Pg 一致）：ByStatus 恒含四个
// 固定键；PendingDecisions 只数 pending_gate 非空且未完结的行；项目不存在
// → ErrNotFound。
func (m *Memory) RequirementStats(ctx context.Context, id string) (RequirementStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return RequirementStats{}, ErrNotFound
	}
	s := RequirementStats{
		ProjectID:    id,
		ByStatus:     map[string]int{"active": 0, "queued": 0, "completed": 0, "blocked": 0},
		LastSyncedAt: p.ExternalSyncedAt,
	}
	for _, d := range m.deliveries {
		if d.ProjectID != id {
			continue
		}
		s.RequirementTotal++
		if _, known := s.ByStatus[d.Status]; known {
			s.ByStatus[d.Status]++
		}
		if d.Status == "completed" {
			s.Delivered++
		}
		if d.PendingGate != "" && d.Status != "completed" {
			s.PendingDecisions++
		}
	}
	return s, nil
}

// ListPendingDecisions 跨项目取全部待人工决策需求（pending_gate 非空且未
// 完结），带 ProjectName，按 updated_at 降序（语义与 Pg 一致）。
func (m *Memory) ListPendingDecisions(_ context.Context) ([]PendingDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PendingDecision, 0)
	for _, d := range m.deliveries {
		if d.PendingGate == "" || d.Status == "completed" {
			continue
		}
		row := PendingDecision{
			ID: d.ID, ProjectID: d.ProjectID, Title: d.Title, Status: d.Status,
			PendingGate: d.PendingGate, CurrentStage: d.CurrentStage,
			ExternalIssueKey: d.ExternalIssueKey, Assignee: d.Assignee, Priority: d.Priority,
			CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
		}
		if p, ok := m.projects[d.ProjectID]; ok {
			row.ProjectName = p.Name
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b PendingDecision) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return out, nil
}

// deliveries

func (m *Memory) CreateDelivery(ctx context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	cp := *d
	m.deliveries[cp.ID] = &cp
	return nil
}

func (m *Memory) GetDelivery(ctx context.Context, id string) (*Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.deliveries[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (m *Memory) ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Delivery, 0)
	for _, d := range m.deliveries {
		if d.ProjectID == projectID {
			out = append(out, *d)
		}
	}
	slices.SortFunc(out, func(a, b Delivery) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// ListActiveDeliveries 跨项目取所有 active 交付（重启恢复用），按创建时间升序。
func (m *Memory) ListActiveDeliveries(ctx context.Context) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Delivery, 0)
	for _, d := range m.deliveries {
		if d.Status == "active" {
			out = append(out, *d)
		}
	}
	slices.SortFunc(out, func(a, b Delivery) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// ListChildDeliveries 取某父 delivery 的全部子需求，按批次号、创建时间升序。
func (m *Memory) ListChildDeliveries(ctx context.Context, parentID string) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Delivery, 0)
	for _, d := range m.deliveries {
		if d.ParentID == parentID {
			out = append(out, *d)
		}
	}
	slices.SortFunc(out, func(a, b Delivery) int {
		if a.Wave != b.Wave {
			return a.Wave - b.Wave
		}
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return out, nil
}

// UpdateDelivery 按读到的 UpdatedAt 条件更新（乐观锁，同 UpdateAgent）：
// 并发读-改-写的后写者版本已过期 → ErrConflict，不静默覆盖（全行覆盖曾无版本校验）。
// 外部来源映射字段归同步入口所有，全行覆盖不冲掉（同 Pg 的列集语义）。
func (m *Memory) UpdateDelivery(ctx context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.deliveries[d.ID]
	if !ok {
		return ErrNotFound
	}
	if !ex.UpdatedAt.Equal(d.UpdatedAt) {
		return ErrConflict // 读-改-写窗口内被并发更新
	}
	d.UpdatedAt = time.Now().UTC()
	d.ExternalIssueID = ex.ExternalIssueID
	d.ExternalIssueKey = ex.ExternalIssueKey
	d.Assignee = ex.Assignee
	d.Priority = ex.Priority
	d.ExternalSyncedAt = ex.ExternalSyncedAt
	cp := *d
	m.deliveries[cp.ID] = &cp
	return nil
}

// UpsertDeliveryByExternalID 按 上游 issue ID 幂等导入（同步链路唯一入口，语义与 Pg 一致）：
// 不存在则插入（整行走入参，同 CreateDelivery）、存在则只更新外部来源字段
// （title/description/status/parent_id/wave/issue_key/assignee/priority）——
// 引擎侧字段（stage/gate/fail_count/...）不被同步覆盖。重复执行不产生重复行。
// ExternalIssueID 为空 → ErrInvalid；ProjectID 不存在 → ErrNotFound。
func (m *Memory) UpsertDeliveryByExternalID(_ context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d.ExternalIssueID == "" {
		return ErrInvalid
	}
	if _, ok := m.projects[d.ProjectID]; !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	for _, ex := range m.deliveries {
		if ex.ExternalIssueID != d.ExternalIssueID {
			continue
		}
		ex.ProjectID = d.ProjectID
		ex.Title = d.Title
		ex.Description = d.Description
		ex.Status = d.Status
		ex.ParentID = d.ParentID
		ex.Wave = d.Wave
		ex.ExternalIssueKey = d.ExternalIssueKey
		ex.Assignee = d.Assignee
		ex.Priority = d.Priority
		ex.UpdatedAt = now
		ex.ExternalSyncedAt = &now
		*d = *ex
		return nil
	}
	if d.ID == "" {
		d.ID = uuid.NewString()
	}
	d.CreatedAt = now
	d.UpdatedAt = now
	d.ExternalSyncedAt = &now
	cp := *d
	m.deliveries[cp.ID] = &cp
	return nil
}

// labels（标签库，语义与 Pg 一致）

// CreateLabel 插入标签；外部 ID 已被占用 → ErrConflict（不静默产生第二行）。
func (m *Memory) CreateLabel(_ context.Context, l *Label) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.ExternalLabelID != "" {
		for _, ex := range m.labels {
			if ex.ExternalLabelID == l.ExternalLabelID {
				return ErrConflict
			}
		}
	}
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	l.CreatedAt = now
	l.UpdatedAt = now
	cp := *l
	m.labels[cp.ID] = &cp
	return nil
}

// UpsertLabelByExternalID 按 上游标签 ID 幂等导入（同步链路唯一入口，语义与
// Pg 一致）：不存在插入、存在只更新 name/color，重复执行不产生重复行。
// ExternalLabelID 为空 → ErrInvalid。
func (m *Memory) UpsertLabelByExternalID(_ context.Context, l *Label) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.ExternalLabelID == "" {
		return ErrInvalid
	}
	now := time.Now().UTC()
	for _, ex := range m.labels {
		if ex.ExternalLabelID != l.ExternalLabelID {
			continue
		}
		ex.Name = l.Name
		ex.Color = l.Color
		ex.UpdatedAt = now
		*l = *ex
		return nil
	}
	if l.ID == "" {
		l.ID = uuid.NewString()
	}
	l.CreatedAt = now
	l.UpdatedAt = now
	cp := *l
	m.labels[cp.ID] = &cp
	return nil
}

func (m *Memory) ListLabels(_ context.Context) ([]Label, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Label, 0, len(m.labels))
	for _, l := range m.labels {
		out = append(out, *l)
	}
	slices.SortFunc(out, func(a, b Label) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// AttachLabel 挂标幂等：重复挂同一标签不产生重复关联。交付或标签不存在 →
// ErrNotFound。
func (m *Memory) AttachLabel(_ context.Context, deliveryID, labelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.deliveries[deliveryID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.labels[labelID]; !ok {
		return ErrNotFound
	}
	if m.deliveryLabels[deliveryID] == nil {
		m.deliveryLabels[deliveryID] = map[string]time.Time{}
	}
	if _, ok := m.deliveryLabels[deliveryID][labelID]; ok {
		return nil // 已挂：幂等
	}
	m.deliveryLabels[deliveryID][labelID] = time.Now().UTC()
	return nil
}

// DetachLabel 摘除交付的标签关联；关联本就不存在 → ErrNotFound。
func (m *Memory) DetachLabel(_ context.Context, deliveryID, labelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.deliveryLabels[deliveryID][labelID]; !ok {
		return ErrNotFound
	}
	delete(m.deliveryLabels[deliveryID], labelID)
	return nil
}

func (m *Memory) ListDeliveryLabels(_ context.Context, deliveryID string) ([]Label, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deliveryLabelsLocked(deliveryID), nil
}

// LabelsByDeliveryID 批量取多个交付挂的标签（任务列表一次装配，免 N+1）。
func (m *Memory) LabelsByDeliveryID(_ context.Context, deliveryIDs []string) (map[string][]Label, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string][]Label, len(deliveryIDs))
	for _, id := range deliveryIDs {
		if ls := m.deliveryLabelsLocked(id); len(ls) > 0 {
			out[id] = ls
		}
	}
	return out, nil
}

// deliveryLabelsLocked 取单个交付挂的标签（完整 Label 行，按 name 升序）；
// 调用方必须已持锁。
func (m *Memory) deliveryLabelsLocked(deliveryID string) []Label {
	ids := m.deliveryLabels[deliveryID]
	out := make([]Label, 0, len(ids))
	for id := range ids {
		if l, ok := m.labels[id]; ok {
			out = append(out, *l)
		}
	}
	slices.SortFunc(out, func(a, b Label) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// events / artifacts / stage_runs

func (m *Memory) AppendEvent(ctx context.Context, e *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	e.CreatedAt = time.Now().UTC()
	cp := *e
	m.events[cp.DeliveryID] = append(m.events[cp.DeliveryID], &cp)
	return nil
}

func (m *Memory) ListEvents(ctx context.Context, deliveryID string) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	evs := m.events[deliveryID]
	out := make([]Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, *e)
	}
	return out, nil
}

func (m *Memory) SaveArtifact(ctx context.Context, a *Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	a.CreatedAt = time.Now().UTC()
	cp := *a
	m.artifacts[cp.DeliveryID] = append(m.artifacts[cp.DeliveryID], &cp)
	return nil
}

// LatestArtifact 从最新往旧找指定 kind 的第一条（无则 ErrNotFound）。
func (m *Memory) LatestArtifact(ctx context.Context, deliveryID, kind string) (*Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	arts := m.artifacts[deliveryID]
	for i := len(arts) - 1; i >= 0; i-- {
		if arts[i].Kind == kind {
			cp := *arts[i]
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) ListArtifacts(ctx context.Context, deliveryID string) ([]Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	arts := m.artifacts[deliveryID]
	out := make([]Artifact, 0, len(arts))
	for _, a := range arts {
		out = append(out, *a)
	}
	return out, nil
}

func (m *Memory) StartStageRun(ctx context.Context, r *StageRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	r.StartedAt = time.Now().UTC()
	cp := *r
	m.stageRuns[cp.DeliveryID] = append(m.stageRuns[cp.DeliveryID], &cp)
	return nil
}

func (m *Memory) FinishStageRun(ctx context.Context, id string, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, runs := range m.stageRuns {
		for _, r := range runs {
			if r.ID == id {
				r.Status = status
				now := time.Now().UTC()
				r.FinishedAt = &now
				return nil
			}
		}
	}
	return ErrNotFound
}

// LatestStageRun 取该阶段最近一次运行。同 started_at 并列时取后插入者
// （slice 即插入序）——latest 用于 attempt 递增与收尾定位，取错旧行会让 attempt 回退。
func (m *Memory) LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var latest *StageRun
	for _, r := range m.stageRuns[deliveryID] {
		if r.Stage != stage {
			continue
		}
		if latest == nil || !r.StartedAt.Before(latest.StartedAt) {
			latest = r // 含并列（!Before）：后插入者胜出
		}
	}
	if latest == nil {
		return nil, ErrNotFound
	}
	cp := *latest
	return &cp, nil
}

// compile-time check
var _ Store = (*Memory)(nil)

// agents / pipeline bindings

func (m *Memory) CreateAgent(_ context.Context, a *Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ex := range m.agents {
		if ex.Name == a.Name {
			return ErrConflict
		}
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now
	if a.Config == nil {
		a.Config = map[string]any{}
	}
	cp := *a
	m.agents[cp.ID] = &cp
	return nil
}

func (m *Memory) ListAgents(_ context.Context) ([]Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Agent, 0, len(m.agents))
	for _, a := range m.agents {
		out = append(out, *a)
	}
	slices.SortFunc(out, func(a, b Agent) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

func (m *Memory) GetAgent(_ context.Context, id string) (*Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

// UpdateAgent 按读到的 UpdatedAt 条件更新（乐观锁）：并发读-改-写的后写者
// 版本已过期 → ErrConflict，不静默覆盖。写前先过 name 唯一校验（同 Create 语义）。
func (m *Memory) UpdateAgent(_ context.Context, a *Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ex, ok := m.agents[a.ID]
	if !ok {
		return ErrNotFound
	}
	for id, other := range m.agents {
		if id != a.ID && other.Name == a.Name {
			return ErrConflict
		}
	}
	if !ex.UpdatedAt.Equal(a.UpdatedAt) {
		return ErrConflict // 读-改-写窗口内被并发更新
	}
	if a.Config == nil {
		a.Config = map[string]any{}
	}
	ex.Name = a.Name
	ex.Runner = a.Runner
	ex.Config = a.Config
	ex.UpdatedAt = time.Now().UTC()
	a.CreatedAt = ex.CreatedAt
	a.UpdatedAt = ex.UpdatedAt
	return nil
}

func (m *Memory) DeleteAgent(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[id]; !ok {
		return ErrNotFound
	}
	for _, b := range m.bindings {
		if b.AgentID == id {
			return ErrConflict
		}
	}
	delete(m.agents, id)
	return nil
}

func bindingKey(projectID, node string) string { return projectID + "\x00" + node }

// UpsertBinding 按 (project,node) 幂等覆盖（项目级专用；空 ProjectID → ErrInvalid）。
func (m *Memory) UpsertBinding(_ context.Context, b *PipelineBinding) error {
	if b.ProjectID == "" {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[b.AgentID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.projects[b.ProjectID]; !ok {
		return ErrNotFound
	}
	key := bindingKey(b.ProjectID, b.Node)
	if ex, ok := m.bindings[key]; ok {
		ex.AgentID = b.AgentID
		b.ID = ex.ID
		b.CreatedAt = ex.CreatedAt
		return nil
	}
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	b.CreatedAt = time.Now().UTC()
	cp := *b
	m.bindings[key] = &cp
	return nil
}

// DeleteBinding 删项目的某节点绑定（项目级专用；空 projectID → ErrInvalid）。
func (m *Memory) DeleteBinding(_ context.Context, projectID, node string) error {
	if projectID == "" {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bindings[bindingKey(projectID, node)]; !ok {
		return ErrNotFound
	}
	delete(m.bindings, bindingKey(projectID, node))
	return nil
}

// ReplaceBindings 原子替换某项目的全部绑定（与 Pg 的单事务语义对齐）：
// 锁内先整体校验（项目、每个 agent 存在），全部通过才删旧写新——
// 任一步不通过直接返回，集合永不半写。空 projectID → ErrInvalid。
func (m *Memory) ReplaceBindings(_ context.Context, projectID string, byNode map[string]string) error {
	if projectID == "" {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return ErrNotFound
	}
	for _, agentID := range byNode {
		if _, ok := m.agents[agentID]; !ok {
			return ErrNotFound
		}
	}
	for key, b := range m.bindings {
		if b.ProjectID == projectID {
			delete(m.bindings, key)
		}
	}
	now := time.Now().UTC()
	for node, agentID := range byNode {
		cp := PipelineBinding{ID: uuid.NewString(), ProjectID: projectID, Node: node, AgentID: agentID, CreatedAt: now}
		m.bindings[bindingKey(projectID, node)] = &cp
	}
	return nil
}

// ListBindings 某项目的绑定，按创建时间升序（项目级专用；空 projectID → ErrInvalid）。
func (m *Memory) ListBindings(_ context.Context, projectID string) ([]PipelineBinding, error) {
	if projectID == "" {
		return nil, ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PipelineBinding, 0)
	for _, b := range m.bindings {
		if b.ProjectID == projectID {
			out = append(out, *b)
		}
	}
	slices.SortFunc(out, func(a, b PipelineBinding) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// ListAllBindings 全部项目的绑定，按创建时间升序。
func (m *Memory) ListAllBindings(_ context.Context) ([]PipelineBinding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PipelineBinding, 0, len(m.bindings))
	for _, b := range m.bindings {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b PipelineBinding) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}
