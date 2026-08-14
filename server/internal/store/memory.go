package store

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Memory is an in-memory Store implementation for tests.
type Memory struct {
	mu         sync.Mutex
	projects   map[string]*Project
	deliveries map[string]*Delivery
	events     map[string][]*Event
	artifacts  map[string][]*Artifact
	stageRuns  map[string][]*StageRun
}

func NewMemory() *Memory {
	return &Memory{
		projects:   map[string]*Project{},
		deliveries: map[string]*Delivery{},
		events:     map[string][]*Event{},
		artifacts:  map[string][]*Artifact{},
		stageRuns:  map[string][]*StageRun{},
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

func (m *Memory) UpdateDelivery(ctx context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.deliveries[d.ID]; !ok {
		return ErrNotFound
	}
	d.UpdatedAt = time.Now().UTC()
	cp := *d
	m.deliveries[cp.ID] = &cp
	return nil
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

func (m *Memory) LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var latest *StageRun
	for _, r := range m.stageRuns[deliveryID] {
		if r.Stage != stage {
			continue
		}
		if latest == nil || r.StartedAt.After(latest.StartedAt) {
			latest = r
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
