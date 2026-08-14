# infera 后端 Greenfield 重写 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从零重写 infera 后端：workdir 是流水线一等资源（intake 即 clone、全程共享、终态释放），引擎由阶段图驱动，agent 可替换（claude|pi），API 兼容现有前端并新增 stats/pinned/artifacts。

**Architecture:** 单 Go 二进制（API + 引擎同进程）。分层：api(薄) / engine(阶段图) / workspace(工作区生命周期) / agent(可替换 runner) / git(纯库) / store(pgx)。DB 迁移从 v1 重新开始，事件 + artifacts 支撑前端详情页。

**Tech Stack:** Go 1.26 · chi · pgx/v5 · golang-migrate · docker SDK（agent 容器）· testify

**约定（全计划适用）：**
- 工作目录均为 `server/`（除非另写明）。测试命令 `go test ./...` 在 `server/` 内执行。
- 与规格的偏差：不用 sqlc，store 手写 SQL + pgx（旧代码即如此，工具链零新增）。
- 旧代码保留在 `server_legacy/`（Task 1 归档，Task 15 删除），搬运以文件为单位复制后改造。

---

### Task 1: 归档旧后端，建新模块骨架

**Files:**
- Create: `server/go.mod`, `server/cmd/infera/main.go`, `server/internal/config/config.go`
- (旧 `server/` → `server_legacy/`)

- [ ] **Step 1: 归档旧目录**

```bash
cd /Users/huiyangz/tokfinity/infera
git mv server server_legacy
git commit -m "chore: archive legacy backend (greenfield rewrite)"
```

- [ ] **Step 2: 新模块与配置**

`server/go.mod`:
```go
module github.com/tokfinity/infera

go 1.26.5
```

`server/internal/config/config.go`:
```go
package config

import "os"

type Config struct {
	Addr         string // HTTP 监听地址
	DatabaseURL  string
	Password     string // 单租户密码门
	GitHubToken  string
	AgentImage   string // agent 容器镜像
	AgentCmd     string // agent 命令（可替换：claude / pi / ...）
	RepoWorkRoot string // workdir 根目录
	TestCmd      string // unit_test 命令（本地模式）
}

func Load() Config {
	return Config{
		Addr:         getenv("PORT", ":8080"),
		DatabaseURL:  getenv("DATABASE_URL", "postgres://infera:infera@localhost:5433/infera?sslmode=disable"),
		Password:     os.Getenv("INFERA_PASSWORD"),
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		AgentImage:   getenv("AGENT_IMAGE", "infera-agent"),
		AgentCmd:     getenv("AGENT_CMD", "claude"),
		RepoWorkRoot: getenv("REPO_WORK_ROOT", "/tmp/infera-workdirs"),
		TestCmd:      getenv("TEST_CMD", "true"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

`server/cmd/infera/main.go`（先能起、能探活）:
```go
package main

import (
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
)

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	log.Printf("infera listening on %s", cfg.Addr)
	log.Fatal(http.ListenAndServe(cfg.Addr, mux))
}
```

- [ ] **Step 3: 验证可编译可运行**

```bash
cd server && go mod tidy && go build ./... && (go run ./cmd/infera & sleep 2; curl -s localhost:8080/api/health; kill %1)
```
Expected: `ok`

- [ ] **Step 4: Commit**

```bash
git add server && git commit -m "feat(server): greenfield module skeleton + config"
```

---

### Task 2: 迁移 v1（新 schema）+ pgx 池

**Files:**
- Create: `server/internal/db/db.go`, `server/internal/db/migrate.go`
- Create: `server/internal/db/migrations/0001_init.up.sql`, `0001_init.down.sql`

- [ ] **Step 1: schema**

`server/internal/db/migrations/0001_init.up.sql`:
```sql
CREATE TABLE projects (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    repo_url      TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT 'main',
    pinned        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deliveries (
    id            UUID PRIMARY KEY,
    project_id    UUID NOT NULL REFERENCES projects(id),
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',  -- active|completed|blocked
    current_stage TEXT NOT NULL DEFAULT 'intake',
    pending_gate  TEXT NOT NULL DEFAULT '',
    fail_count    INT NOT NULL DEFAULT 0,
    base_commit   TEXT NOT NULL DEFAULT '',        -- clone 时记录的快照
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deliveries_project ON deliveries(project_id);

CREATE TABLE stage_runs (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    attempt     INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'running',   -- running|done|failed
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE artifacts (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    kind        TEXT NOT NULL,                     -- spec|tests|diff|pr|agent_output
    content     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_artifacts_delivery ON artifacts(delivery_id);

CREATE TABLE events (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_delivery ON events(delivery_id);
```

`0001_init.down.sql`:
```sql
DROP TABLE IF EXISTS events; DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS stage_runs; DROP TABLE IF EXISTS deliveries;
DROP TABLE IF EXISTS projects;
```

- [ ] **Step 2: 池 + migrate runner**

`server/internal/db/db.go`:
```go
package db

import (
	"context"
	"embed"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return pool, pool.Ping(ctx2)
}

// Migrate 把 schema 迁到最新（iofs 内嵌）。
func Migrate(url string) error {
	d, err := migrate.NewWithSourceInstance("iofs", mustFS(), toPgxURL(url))
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

func mustFS() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		panic(err)
	}
	return sub
}

// golang-migrate 的 pgx driver 用 "pgx5://" scheme。
func toPgxURL(u string) string { return strings.Replace(u, "postgres://", "pgx5://", 1) }
```
（补 import `embed/io/fs`、`strings`；`fmt` 若未用则去掉。）

- [ ] **Step 3: 编译并实跑迁移（本地 postgres 已在 5433）**

```bash
cd server && go get github.com/golang-migrate/migrate/v4 github.com/jackc/pgx/v5 && go mod tidy && go build ./...
docker rm -f infera-postgres-test 2>/dev/null; docker run -d --name infera-postgres-test -e POSTGRES_USER=infera -e POSTGRES_PASSWORD=infera -e POSTGRES_DB=infera_test -p 5434:5432 postgres:16-alpine
cat > /tmp/mig_test.go <<'EOF'
package main
import ("fmt"; "github.com/tokfinity/infera/internal/db")
func main() { fmt.Println(db.Migrate("postgres://infera:infera@localhost:5434/infera_test?sslmode=disable")) }
EOF
mkdir -p server/cmd/migtest && cp /tmp/mig_test.go server/cmd/migtest/main.go && cd server && go run ./cmd/migtest
```
Expected: `<nil>`；`docker exec infera-postgres-test psql -U infera -d infera_test -c '\dt'` 列出 5 张表。跑完删除 `server/cmd/migtest`。

- [ ] **Step 4: Commit**

```bash
git add server && git commit -m "feat(server): pgx pool + v1 schema migrations"
```

---

### Task 3: store —— projects

**Files:**
- Create: `server/internal/store/store.go`（接口 + 公共类型）
- Create: `server/internal/store/pg.go`（pgx 实现：projects 部分）
- Create: `server/internal/store/memory.go`（内存实现，测试/E2E 用）
- Test: `server/internal/store/pg_test.go`, `memory_test.go`

store 采用「接口 + 双实现」：pgx 为主，内存版给引擎/API 单测。

- [ ] **Step 1: 接口与类型（含全计划用到的全部方法签名）**

`server/internal/store/store.go`:
```go
package store

import (
	"context"
	"time"
)

type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	DefaultBranch string    `json:"default_branch"`
	Pinned        bool      `json:"pinned"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ProjectStats struct {
	Active  int       `json:"active"`
	Pending int       `json:"pending"`
	Last    time.Time `json:"last_activity"`
}

type Delivery struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Status       string    `json:"status"` // active|completed|blocked
	CurrentStage string    `json:"current_stage"`
	PendingGate  string    `json:"pending_gate"`
	FailCount    int       `json:"fail_count"`
	BaseCommit   string    `json:"base_commit"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Event struct {
	ID        string    `json:"id"`
	DeliveryID string   `json:"delivery_id"`
	Stage     string    `json:"stage"`
	EventType string    `json:"event_type"`
	Payload   []byte    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

type Artifact struct {
	ID         string    `json:"id"`
	DeliveryID string    `json:"delivery_id"`
	Stage      string    `json:"stage"`
	Kind       string    `json:"kind"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type StageRun struct {
	ID         string     `json:"id"`
	DeliveryID string     `json:"delivery_id"`
	Stage      string     `json:"stage"`
	Attempt    int        `json:"attempt"`
	Status     string     `json:"status"` // running|done|failed
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

type Store interface {
	// projects
	CreateProject(ctx context.Context, p *Project) error
	ListProjects(ctx context.Context) ([]Project, error)
	GetProject(ctx context.Context, id string) (*Project, error)
	PatchProjectPinned(ctx context.Context, id string, pinned bool) error
	ProjectStats(ctx context.Context, id string) (ProjectStats, error)
	// deliveries
	CreateDelivery(ctx context.Context, d *Delivery) error
	GetDelivery(ctx context.Context, id string) (*Delivery, error)
	ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error)
	UpdateDelivery(ctx context.Context, d *Delivery) error
	// events / artifacts / stage_runs
	AppendEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, deliveryID string) ([]Event, error)
	SaveArtifact(ctx context.Context, a *Artifact) error
	ListArtifacts(ctx context.Context, deliveryID string) ([]Artifact, error)
	StartStageRun(ctx context.Context, r *StageRun) error
	FinishStageRun(ctx context.Context, id string, status string) error
	LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error)
}
```

- [ ] **Step 2: 内存实现（全方法，供测试）**

`server/internal/store/memory.go`:
```go
package store

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Memory struct {
	mu          sync.Mutex
	projects    map[string]*Project
	deliveries  map[string]*Delivery
	events      map[string][]*Event
	artifacts   map[string][]*Artifact
	stageRuns   map[string][]*StageRun
}

func NewMemory() *Memory {
	return &Memory{
		projects: map[string]*Project{}, deliveries: map[string]*Delivery{},
		events: map[string][]*Event{}, artifacts: map[string][]*Artifact{},
		stageRuns: map[string][]*StageRun{},
	}
}

func (m *Memory) CreateProject(_ context.Context, p *Project) error {
	m.mu.Lock(); defer m.mu.Unlock()
	p.ID = uuid.NewString(); now := time.Now(); p.CreatedAt, p.UpdatedAt = now, now
	m.projects[p.ID] = p; return nil
}
func (m *Memory) ListProjects(_ context.Context) ([]Project, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]Project, 0, len(m.projects))
	for _, p := range m.projects { out = append(out, *p) }
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *Memory) GetProject(_ context.Context, id string) (*Project, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	if p, ok := m.projects[id]; ok { c := *p; return &c, nil }
	return nil, ErrNotFound
}
func (m *Memory) PatchProjectPinned(_ context.Context, id string, pinned bool) error {
	m.mu.Lock(); defer m.mu.Unlock()
	p, ok := m.projects[id]; if !ok { return ErrNotFound }
	p.Pinned = pinned; p.UpdatedAt = time.Now(); return nil
}
func (m *Memory) ProjectStats(_ context.Context, id string) (ProjectStats, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	s := ProjectStats{}
	p, ok := m.projects[id]; if !ok { return s, ErrNotFound }
	s.Last = p.UpdatedAt
	for _, d := range m.deliveries {
		if d.ProjectID != id { continue }
		if d.Status == "active" { s.Active++ }
		if d.PendingGate != "" { s.Pending++ }
		if d.UpdatedAt.After(s.Last) { s.Last = d.UpdatedAt }
	}
	return s, nil
}
func (m *Memory) CreateDelivery(_ context.Context, d *Delivery) error {
	m.mu.Lock(); defer m.mu.Unlock()
	d.ID = uuid.NewString(); now := time.Now(); d.CreatedAt, d.UpdatedAt = now, now
	m.deliveries[d.ID] = d; return nil
}
func (m *Memory) GetDelivery(_ context.Context, id string) (*Delivery, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	if d, ok := m.deliveries[id]; ok { c := *d; return &c, nil }
	return nil, ErrNotFound
}
func (m *Memory) ListProjectDeliveries(_ context.Context, projectID string) ([]Delivery, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	var out []Delivery
	for _, d := range m.deliveries {
		if d.ProjectID == projectID { out = append(out, *d) }
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}
func (m *Memory) UpdateDelivery(_ context.Context, d *Delivery) error {
	m.mu.Lock(); defer m.mu.Unlock()
	if _, ok := m.deliveries[d.ID]; !ok { return ErrNotFound }
	d.UpdatedAt = time.Now(); m.deliveries[d.ID] = d; return nil
}
func (m *Memory) AppendEvent(_ context.Context, e *Event) error {
	m.mu.Lock(); defer m.mu.Unlock()
	e.ID = uuid.NewString(); e.CreatedAt = time.Now()
	m.events[e.DeliveryID] = append(m.events[e.DeliveryID], e); return nil
}
func (m *Memory) ListEvents(_ context.Context, deliveryID string) ([]Event, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]Event, 0, len(m.events[deliveryID]))
	for _, e := range m.events[deliveryID] { out = append(out, *e) }
	return out, nil
}
func (m *Memory) SaveArtifact(_ context.Context, a *Artifact) error {
	m.mu.Lock(); defer m.mu.Unlock()
	a.ID = uuid.NewString(); a.CreatedAt = time.Now()
	m.artifacts[a.DeliveryID] = append(m.artifacts[a.DeliveryID], a); return nil
}
func (m *Memory) ListArtifacts(_ context.Context, deliveryID string) ([]Artifact, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	out := make([]Artifact, 0, len(m.artifacts[deliveryID]))
	for _, a := range m.artifacts[deliveryID] { out = append(out, *a) }
	return out, nil
}
func (m *Memory) StartStageRun(_ context.Context, r *StageRun) error {
	m.mu.Lock(); defer m.mu.Unlock()
	r.ID = uuid.NewString(); r.StartedAt = time.Now()
	m.stageRuns[r.DeliveryID] = append(m.stageRuns[r.DeliveryID], r); return nil
}
func (m *Memory) FinishStageRun(_ context.Context, id string, status string) error {
	m.mu.Lock(); defer m.mu.Unlock()
	for _, runs := range m.stageRuns {
		for _, r := range runs {
			if r.ID == id { r.Status = status; now := time.Now(); r.FinishedAt = &now; return nil }
		}
	}
	return ErrNotFound
}
func (m *Memory) LatestStageRun(_ context.Context, deliveryID, stage string) (*StageRun, error) {
	m.mu.Lock(); defer m.mu.Unlock()
	var latest *StageRun
	for _, r := range m.stageRuns[deliveryID] {
		if r.Stage == stage && (latest == nil || r.StartedAt.After(latest.StartedAt)) { latest = r }
	}
	if latest == nil { return nil, ErrNotFound }
	c := *latest; return &c, nil
}
```

`server/internal/store/errors.go`:
```go
package store

import "errors"

var ErrNotFound = errors.New("not found")
```

- [ ] **Step 3: 内存实现测试**

`server/internal/store/memory_test.go`:
```go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryProjectPinnedAndStats(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	p := &Project{Name: "demo", RepoURL: "", DefaultBranch: "main"}
	require.NoError(t, m.CreateProject(ctx, p))
	require.NoError(t, m.PatchProjectPinned(ctx, p.ID, true))
	got, err := m.GetProject(ctx, p.ID)
	require.NoError(t, err)
	require.True(t, got.Pinned)

	d := &Delivery{ProjectID: p.ID, Title: "t", Status: "active", PendingGate: "spec_approval"}
	require.NoError(t, m.CreateDelivery(ctx, d))
	s, err := m.ProjectStats(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, s.Active)
	require.Equal(t, 1, s.Pending)
}
```

- [ ] **Step 4: 跑测试**

```bash
cd server && go get github.com/google/uuid github.com/stretchr/testify && go test ./internal/store/
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server && git commit -m "feat(server): store interface + memory impl"
```

---

### Task 4: store —— pgx 实现（projects + deliveries + events/artifacts/runs）

**Files:**
- Create: `server/internal/store/pg.go`
- Test: `server/internal/store/pg_test.go`（连 5434 测试库，无库则 Skip）

- [ ] **Step 1: pgx 实现**

`server/internal/store/pg.go`（SQL 与 schema 一一对应；`exec` 帮助函数处理 uuid 时间戳转换）:
```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pg struct{ pool *pgxpool.Pool }

func NewPg(pool *pgxpool.Pool) *Pg { return &Pg{pool: pool} }

var _ Store = (*Pg)(nil)

func (p *Pg) CreateProject(ctx context.Context, pr *Project) error {
	pr.ID = uuid.NewString()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO projects (id, name, repo_url, default_branch) VALUES ($1,$2,$3,$4)`,
		pr.ID, pr.Name, pr.RepoURL, pr.DefaultBranch)
	return err
}
func (p *Pg) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,name,repo_url,default_branch,pinned,created_at,updated_at FROM projects ORDER BY created_at`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var x Project
		if err := rows.Scan(&x.ID, &x.Name, &x.RepoURL, &x.DefaultBranch, &x.Pinned, &x.CreatedAt, &x.UpdatedAt); err != nil { return nil, err }
		out = append(out, x)
	}
	return out, rows.Err()
}
func (p *Pg) GetProject(ctx context.Context, id string) (*Project, error) {
	return p.scanProject(ctx, `SELECT id,name,repo_url,default_branch,pinned,created_at,updated_at FROM projects WHERE id=$1`, id)
}
func (p *Pg) scanProject(ctx context.Context, q string, args ...any) (*Project, error) {
	row := p.pool.QueryRow(ctx, q, args...)
	var x Project
	err := row.Scan(&x.ID, &x.Name, &x.RepoURL, &x.DefaultBranch, &x.Pinned, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) { return nil, ErrNotFound }
	if err != nil { return nil, err }
	return &x, nil
}
func (p *Pg) PatchProjectPinned(ctx context.Context, id string, pinned bool) error {
	ct, err := p.pool.Exec(ctx, `UPDATE projects SET pinned=$2, updated_at=now() WHERE id=$1`, id, pinned)
	if err != nil { return err }
	if ct.RowsAffected() == 0 { return ErrNotFound }
	return nil
}
func (p *Pg) ProjectStats(ctx context.Context, id string) (ProjectStats, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status='active'),
		       count(*) FILTER (WHERE pending_gate<>''),
		       greatest(now(), coalesce(max(updated_at), (SELECT updated_at FROM projects WHERE id=$1)))
		FROM deliveries WHERE project_id=$1`, id)
	var s ProjectStats
	err := row.Scan(&s.Active, &s.Pending, &s.Last)
	if errors.Is(err, pgx.ErrNoRows) { return s, ErrNotFound }
	return s, err
}
func (p *Pg) CreateDelivery(ctx context.Context, d *Delivery) error {
	d.ID = uuid.NewString()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO deliveries (id, project_id, title, description) VALUES ($1,$2,$3,$4)`,
		d.ID, d.ProjectID, d.Title, d.Description)
	return err
}
const deliveryCols = `id,project_id,title,description,status,current_stage,pending_gate,fail_count,base_commit,created_at,updated_at`
func scanDelivery(row pgx.Row) (*Delivery, error) {
	var d Delivery
	err := row.Scan(&d.ID, &d.ProjectID, &d.Title, &d.Description, &d.Status, &d.CurrentStage, &d.PendingGate, &d.FailCount, &d.BaseCommit, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) { return nil, ErrNotFound }
	if err != nil { return nil, err }
	return &d, nil
}
func (p *Pg) GetDelivery(ctx context.Context, id string) (*Delivery, error) {
	return scanDelivery(p.pool.QueryRow(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE id=$1`, id))
}
func (p *Pg) ListProjectDeliveries(ctx context.Context, projectID string) ([]Delivery, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+deliveryCols+` FROM deliveries WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil { return nil, err }
		out = append(out, *d)
	}
	return out, rows.Err()
}
func (p *Pg) UpdateDelivery(ctx context.Context, d *Delivery) error {
	ct, err := p.pool.Exec(ctx, `
		UPDATE deliveries SET status=$2,current_stage=$3,pending_gate=$4,fail_count=$5,base_commit=$6,updated_at=now()
		WHERE id=$1`,
		d.ID, d.Status, d.CurrentStage, d.PendingGate, d.FailCount, d.BaseCommit)
	if err != nil { return err }
	if ct.RowsAffected() == 0 { return ErrNotFound }
	return nil
}
func (p *Pg) AppendEvent(ctx context.Context, e *Event) error {
	e.ID = uuid.NewString()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO events (id, delivery_id, stage, event_type, payload) VALUES ($1,$2,$3,$4,$5)`,
		e.ID, e.DeliveryID, e.Stage, e.EventType, e.Payload)
	return err
}
func (p *Pg) ListEvents(ctx context.Context, deliveryID string) ([]Event, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,delivery_id,stage,event_type,payload,created_at FROM events WHERE delivery_id=$1 ORDER BY created_at`, deliveryID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.DeliveryID, &e.Stage, &e.EventType, &e.Payload, &e.CreatedAt); err != nil { return nil, err }
		out = append(out, e)
	}
	return out, rows.Err()
}
func (p *Pg) SaveArtifact(ctx context.Context, a *Artifact) error {
	a.ID = uuid.NewString()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO artifacts (id, delivery_id, stage, kind, content) VALUES ($1,$2,$3,$4,$5)`,
		a.ID, a.DeliveryID, a.Stage, a.Kind, a.Content)
	return err
}
func (p *Pg) ListArtifacts(ctx context.Context, deliveryID string) ([]Artifact, error) {
	rows, err := p.pool.Query(ctx, `SELECT id,delivery_id,stage,kind,content,created_at FROM artifacts WHERE delivery_id=$1 ORDER BY created_at`, deliveryID)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := rows.Scan(&a.ID, &a.DeliveryID, &a.Stage, &a.Kind, &a.Content, &a.CreatedAt); err != nil { return nil, err }
		out = append(out, a)
	}
	return out, rows.Err()
}
func (p *Pg) StartStageRun(ctx context.Context, r *StageRun) error {
	r.ID = uuid.NewString()
	_, err := p.pool.Exec(ctx,
		`INSERT INTO stage_runs (id, delivery_id, stage, attempt) VALUES ($1,$2,$3,$4)`,
		r.ID, r.DeliveryID, r.Stage, r.Attempt)
	return err
}
func (p *Pg) FinishStageRun(ctx context.Context, id string, status string) error {
	ct, err := p.pool.Exec(ctx, `UPDATE stage_runs SET status=$2, finished_at=now() WHERE id=$1`, id, status)
	if err != nil { return err }
	if ct.RowsAffected() == 0 { return ErrNotFound }
	return nil
}
func (p *Pg) LatestStageRun(ctx context.Context, deliveryID, stage string) (*StageRun, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id,delivery_id,stage,attempt,status,started_at,finished_at
		FROM stage_runs WHERE delivery_id=$1 AND stage=$2
		ORDER BY started_at DESC LIMIT 1`, deliveryID, stage)
	var r StageRun
	err := row.Scan(&r.ID, &r.DeliveryID, &r.Stage, &r.Attempt, &r.Status, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) { return nil, ErrNotFound }
	if err != nil { return nil, err }
	return &r, nil
}
```

- [ ] **Step 2: 集成测试（无 5434 测试库自动跳过）**

`server/internal/store/pg_test.go`:
```go
package store

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/db"
)

func testPool(t *testing.T) *Pg {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL 未设置（docker run -p 5434 infera-postgres-test 后设 postgres://infera:inferra@localhost:5434/infera_test?sslmode=disable）")
	}
	if err := db.Migrate(url); err != nil { t.Fatal(err) }
	pool, err := db.Connect(context.Background(), url)
	if err != nil { t.Fatal(err) }
	t.Cleanup(pool.Close)
	_, _ = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects`)
	return NewPg(pool)
}

func TestPgProjectAndDelivery(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	proj := &Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, p.CreateProject(ctx, proj))
	require.NoError(t, p.PatchProjectPinned(ctx, proj.ID, true))
	got, err := p.GetProject(ctx, proj.ID)
	require.NoError(t, err)
	require.True(t, got.Pinned)

	d := &Delivery{ProjectID: proj.ID, Title: "需求A", Status: "active", CurrentStage: "spec", PendingGate: "spec_approval"}
	require.NoError(t, p.CreateDelivery(ctx, d))
	d.FailCount = 1
	require.NoError(t, p.UpdateDelivery(ctx, d))
	gotD, err := p.GetDelivery(ctx, d.ID)
	require.NoError(t, err)
	require.Equal(t, 1, gotD.FailCount)

	ev := &Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_started", Payload: []byte(`{}`)}
	require.NoError(t, p.AppendEvent(ctx, ev))
	evs, err := p.ListEvents(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, evs, 1)

	art := &Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# spec"}
	require.NoError(t, p.SaveArtifact(ctx, art))
	arts, err := p.ListArtifacts(ctx, d.ID)
	require.NoError(t, err)
	require.Len(t, arts, 1)

	s, err := p.ProjectStats(ctx, proj.ID)
	require.NoError(t, err)
	require.Equal(t, 1, s.Active)
	require.Equal(t, 1, s.Pending)
}
```

- [ ] **Step 3: 跑测试**

```bash
docker start infera-postgres-test 2>/dev/null || docker run -d --name infera-postgres-test -e POSTGRES_USER=infera -e POSTGRES_PASSWORD=infera -e POSTGRES_DB=infera_test -p 5434:5432 postgres:16-alpine
cd server && TEST_DATABASE_URL='postgres://infera:infera@localhost:5434/infera_test?sslmode=disable' go test ./internal/store/
```
Expected: PASS（含 pg 集成与 memory）

- [ ] **Step 4: Commit**

```bash
git add server && git commit -m "feat(server): pgx store implementation"
```

---

### Task 5: git 库（ls-remote / clone / commit-push / PR）

**Files:**
- Create: `server/internal/git/git.go`
- Test: `server/internal/git/git_test.go`（本地 bare 仓库，无网络）

- [ ] **Step 1: 失败测试**

`server/internal/git/git_test.go`:
```go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// newBare 建一个带 1 个 commit 的本地 bare 仓库（模拟远端）。
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	run := func(cwd, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "init", "-b", "main", work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# hi\n"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", origin, "main")
	return origin
}

func TestLsRemoteAndCloneAndHead(t *testing.T) {
	origin := newBare(t)
	g := New()
	require.NoError(t, g.LsRemote(origin))
	require.Error(t, g.LsRemote(origin+"/nope"))

	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(origin, "main", cloneDir))
	head, err := g.Head(cloneDir)
	require.NoError(t, err)
	require.Len(t, head, 40)
}

func TestCommitAndPush(t *testing.T) {
	origin := newBare(t)
	g := New()
	cloneDir := filepath.Join(t.TempDir(), "work")
	require.NoError(t, g.Clone(origin, "main", cloneDir))
	require.NoError(t, os.WriteFile(filepath.Join(cloneDir, "f.txt"), []byte("x"), 0o644))
	pushed, err := g.CommitAndPush(cloneDir, "feat: add file", "refs/heads/infera/test", origin)
	require.NoError(t, err)
	require.True(t, pushed)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd server && go test ./internal/git/
```
Expected: FAIL（`New` 未定义）

- [ ] **Step 3: 实现（token 注入 URL 从旧 github/repo.go 搬运思路）**

`server/internal/git/git.go`:
```go
// Package git 封装 git 命令行与 GitHub API。agent 无关、引擎无关的纯库。
package git

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

type Git struct{ Token string }

func New() *Git { return &Git{} }

func injectToken(rawURL, token string) string {
	if token == "" || !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil { return rawURL }
	u.User = url.UserPassword(token, "")
	return u.String()
}

func (g *Git) run(cwd string, args ...string) (string, error) {
	full := append([]string{"-c", "user.email=agent@infera.dev", "-c", "user.name=infera-agent"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", args[0], err, out)
	}
	return string(out), nil
}

// LsRemote 毫秒级可达性/权限校验（不落盘）。
func (g *Git) LsRemote(rawURL string) error {
	cmd := exec.Command("git", "ls-remote", "--heads", injectToken(rawURL, g.Token))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ls-remote: %w: %s", err, out)
	}
	return nil
}

// Clone 浅克隆指定分支。
func (g *Git) Clone(rawURL, branch, dir string) error {
	_, err := g.run("", "clone", "--depth", "1", "--branch", branch, injectToken(rawURL, g.Token), dir)
	return err
}

// Head 返回当前 HEAD commit（快照基准）。
func (g *Git) Head(dir string) (string, error) {
	out, err := g.run(dir, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// CommitAndPush 提交 workdir 全部变更并推到远端分支；无变更时返回 (false, nil)。
func (g *Git) CommitAndPush(dir, msg, ref, pushURL string) (bool, error) {
	if _, err := g.run(dir, "add", "-A"); err != nil { return false, err }
	st, err := g.run(dir, "status", "--porcelain")
	if err != nil { return false, err }
	if strings.TrimSpace(st) == "" { return false, nil }
	if _, err := g.run(dir, "commit", "-m", msg); err != nil { return false, err }
	if _, err := g.run(dir, "push", injectToken(pushURL, g.Token), "HEAD:"+ref); err != nil { return false, err }
	return true, nil
}

var _ = context.Background // 保留 context 依赖位（PR 客户端接入时使用）
```

- [ ] **Step 4: 跑测试通过 + Commit**

```bash
cd server && go test ./internal/git/ && git add -A && git commit -m "feat(server): git lib (ls-remote/clone/head/commit-push)"
```

---

### Task 6: workspace 包（workdir 一等资源）

**Files:**
- Create: `server/internal/workspace/workspace.go`
- Test: `server/internal/workspace/workspace_test.go`

- [ ] **Step 1: 失败测试**

`server/internal/workspace/workspace_test.go`:
```go
package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/git"
)

func TestAcquireClonesAndRecordsBase(t *testing.T) {
	origin := newBare(t) // 从 git 测试包复制 helper（见 Step 2 说明）
	g := git.New()
	ws := New(t.TempDir(), g, time.Hour)

	dir, base, err := ws.Acquire("d1", origin, "main")
	require.NoError(t, err)
	require.NotEmpty(t, base)
	require.FileExists(t, filepath.Join(dir, "README.md"))

	// 幂等：再次 Acquire 复用，不重新 clone
	dir2, base2, err := ws.Acquire("d1", origin, "main")
	require.NoError(t, err)
	require.Equal(t, dir, dir2)
	require.Equal(t, base, base2)
}

func TestAcquireGreenfield(t *testing.T) {
	g := git.New()
	ws := New(t.TempDir(), g, time.Hour)
	dir, base, err := ws.Acquire("d2", "", "main")
	require.NoError(t, err)
	require.Empty(t, base) // 绿地：无仓库，base 为空
	require.DirExists(t, dir)
}

func TestReleaseCleansAfterRetention(t *testing.T) {
	g := git.New()
	ws := New(t.TempDir(), g, 10*time.Millisecond)
	dir, _, err := ws.Acquire("d3", newBare(t), "main")
	require.NoError(t, err)
	ws.Release("d3")
	require.Eventually(t, func() bool { _, err := os.Stat(dir); return os.IsNotExist(err) },
		2*time.Second, 5*time.Millisecond)
}
```

`newBare` helper：复制 `internal/git/git_test.go` 的同名函数到本包（测试文件间不共享包），放入 `workspace_test.go`。

- [ ] **Step 2: 跑测试确认失败**

```bash
cd server && go test ./internal/workspace/
```
Expected: FAIL

- [ ] **Step 3: 实现**

`server/internal/workspace/workspace.go`:
```go
// Package workspace 管理 delivery 的 workdir 生命周期：
// Acquire（intake 前）→ 全程共享 → Release（终态后延迟清理）。
package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tokfinity/infera/internal/git"
)

type Manager struct {
	root     string
	git      *git.Git
	retention time.Duration
	mu       sync.Mutex
	bases    map[string]string // deliveryID -> base_commit
	dirs     map[string]string
}

func New(root string, g *git.Git, retention time.Duration) *Manager {
	return &Manager{root: root, git: g, retention: retention, bases: map[string]string{}, dirs: map[string]string{}}
}

// Acquire 保证 delivery 的 workdir 就绪并返回 (dir, baseCommit)。
// 有仓库则 clone（幂等）；绿地项目只建目录。
func (m *Manager) Acquire(deliveryID, repoURL, branch string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if dir, ok := m.dirs[deliveryID]; ok {
		return dir, m.bases[deliveryID], nil
	}
	dir := filepath.Join(m.root, deliveryID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	base := ""
	if repoURL != "" {
		if err := m.git.Clone(repoURL, branch, dir); err != nil {
			_ = os.RemoveAll(dir)
			return "", "", err
		}
		base, err := m.git.Head(dir)
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", "", err
		}
		m.bases[deliveryID] = base
	}
	m.dirs[deliveryID] = dir
	m.bases[deliveryID] = base
	return dir, base, nil
}

func (m *Manager) Path(deliveryID string) string {
	return filepath.Join(m.root, deliveryID)
}

// Release 按保留期延迟清理；retention<=0 立即清理。
func (m *Manager) Release(deliveryID string) {
	m.mu.Lock()
	dir := m.dirs[deliveryID]
	delete(m.dirs, deliveryID)
	delete(m.bases, deliveryID)
	m.mu.Unlock()
	if dir == "" { return }
	if m.retention <= 0 {
		_ = os.RemoveAll(dir)
		return
	}
	go func() {
		time.Sleep(m.retention)
		_ = os.RemoveAll(dir)
	}()
}
```
（注意 Step 3 代码中 `base, err :=` 遮蔽问题——实现时写成：
```go
		head, err := m.git.Head(dir)
		if err != nil { _ = os.RemoveAll(dir); return "", "", err }
		base = head
```
避免 `base` 被短声明遮蔽。）

- [ ] **Step 4: 跑测试通过 + Commit**

```bash
cd server && go test ./internal/workspace/ ./internal/git/ && git add -A && git commit -m "feat(server): workspace lifecycle manager"
```

---

### Task 7: agent 运行时（可替换 runner）

**Files:**
- Create: `server/internal/agent/runner.go`（接口 + Request/Result + 角色 prompt）
- Create: `server/internal/agent/local.go`（本地命令 runner：E2E 与 pi/claude CLI 同形）
- Create: `server/internal/agent/docker.go`（容器 runner：从 `server_legacy/internal/agent/docker.go` 改造）
- Test: `server/internal/agent/runner_test.go`

- [ ] **Step 1: 接口 + prompt（角色=阶段，prompt 模板集中在此，换 agent 只换命令）**

`server/internal/agent/runner.go`:
```go
// Package agent 定义可替换的 agent 运行时。
// 契约：workdir 进、role+prompt 进、output 出。claude / pi / fake 皆可实现。
package agent

import "context"

type Request struct {
	Role    string // spec | test_gen | code_gen | code_review
	Prompt  string
	Workdir string
	// Inputs 是上游产物（如 spec 全文），由引擎拼装。
	Inputs map[string]string
}

type Result struct{ Output string }

type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
}

// Prompts 每个角色的指令模板。{description} {spec} 为占位符。
var Prompts = map[string]string{
	"spec": `你是资深工程师。基于仓库现状，为以下需求撰写实现规格（中文，Markdown）：
需求：{description}
要求：列出改动文件、接口变化、验收标准。只输出规格正文。`,
	"test_gen": `你是测试工程师。依据以下规格，在仓库中编写测试用例（Go 项目用 _test.go）。
规格：
{spec}
只输出新增/修改的文件清单与说明。`,
	"code_gen": `你是程序员。在当前仓库中实现以下需求，严格遵循规格。
需求：{description}
规格：
{spec}
实现完成后输出改动摘要。`,
	"code_review": `你是代码审查员。审查当前仓库工作区中未提交的改动，对照规格评估正确性与质量，输出审查意见。`,
}

func BuildPrompt(role string, description, spec string) string {
	p := Prompts[role]
	p = replace(p, "{description}", description)
	p = replace(p, "{spec}", spec)
	return p
}

func replace(s, old, new string) string {
	if new == "" { new = "（无）" }
	// 简单替换，避免引入模板库
	out := ""
	for i := 0; i < len(s); {
		if len(s[i:]) >= len(old) && s[i:i+len(old)] == old {
			out += new
			i += len(old)
		} else {
			out += string(s[i])
			i++
		}
	}
	return out
}
```

- [ ] **Step 2: 失败测试（local runner 用 echo / 写文件脚本模拟 agent）**

`server/internal/agent/runner_test.go`:
```go
package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildPrompt(t *testing.T) {
	p := BuildPrompt("spec", "写 README", "")
	require.Contains(t, p, "写 README")
	require.Contains(t, p, "（无）") // spec 占位被替换
}

func TestLocalRunner(t *testing.T) {
	dir := t.TempDir()
	r := NewLocal([]string{"sh", "-c", "echo done: $INFERA_ROLE && touch $INFERA_WORKDIR/agent_ran.txt"})
	res, err := r.Run(context.Background(), Request{Role: "spec", Prompt: "p", Workdir: dir})
	require.NoError(t, err)
	require.Contains(t, res.Output, "done: spec")
	require.FileExists(t, filepath.Join(dir, "agent_ran.txt"))
}
```

- [ ] **Step 3: 跑测试确认失败 → 实现 local.go**

```bash
cd server && go test ./internal/agent/
```
Expected: FAIL（NewLocal 未定义）

`server/internal/agent/local.go`:
```go
package agent

import (
	"bytes"
	"context"
	"os/exec"
)

// LocalRunner 在主机上执行命令（E2E 与本地 pi/claude CLI 调试用）。
// 契约环境变量：INFERA_ROLE / INFERA_WORKDIR；stdout 即 agent 输出。
type LocalRunner struct{ cmd []string }

func NewLocal(cmd []string) *LocalRunner { return &LocalRunner{cmd: cmd} }

func (l *LocalRunner) Run(ctx context.Context, req Request) (Result, error) {
	cmd := exec.CommandContext(ctx, l.cmd[0], l.cmd[1:]...)
	cmd.Dir = req.Workdir
	cmd.Env = append(cmd.Environ(),
		"INFERA_ROLE="+req.Role,
		"INFERA_WORKDIR="+req.Workdir,
	)
	cmd.Stdin = bytes.NewBufferString(req.Prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return Result{Output: out.String()}, err
}
```

- [ ] **Step 4: docker runner（从 legacy 搬运改造：挂 workdir、注入 prompt 环境变量）**

`server/internal/agent/docker.go`（参照 `server_legacy/internal/agent/docker.go` 的 docker SDK 用法，核心保留：create container + bind mount workdir + wait + logs；命令与镜像可配置）:
```go
package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type DockerRunner struct {
	cli    *client.Client
	image  string
	cmd    []string // 例: ["claude","-p"] —— prompt 走 stdin / 环境变量
}

func NewDocker(image string, cmd []string) (*DockerRunner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil { return nil, err }
	return &DockerRunner{cli: cli, image: image, cmd: cmd}, nil
}

func (d *DockerRunner) Run(ctx context.Context, req Request) (Result, error) {
	cfg := &container.Config{
		Image: d.image,
		Cmd:   append(append([]string{}, d.cmd...), req.Prompt),
		Env:   []string{"INFERA_ROLE=" + req.Role},
		WorkingDir: "/work",
	}
	hc := &container.HostConfig{
		Mounts: []mount.Mount{{
			Type:   mount.TypeBind,
			Source: req.Workdir,
			Target: "/work",
		}},
	}
	c, err := d.cli.ContainerCreate(ctx, cfg, hc, nil, nil, "")
	if err != nil { return Result{}, err }
	defer func() { _ = d.cli.ContainerRemove(context.Background(), c.ID, container.RemoveOptions{Force: true}) }()
	if err := d.cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil { return Result{}, err }
	waitCh, errCh := d.cli.ContainerWait(ctx, c.ID, container.WaitConditionNotRunning)
	select {
	case <-waitCh:
	case err := <-errCh:
		return Result{}, err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
	logs, err := d.cli.ContainerLogs(ctx, c.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil { return Result{}, err }
	defer logs.Close()
	var buf bytes.Buffer
	_, _ = stdcopy.StdCopy(&buf, &buf, logs)
	return Result{Output: buf.String()}, nil
}

var _ = fmt.Sprintf // 占位（去 unused import），实现时按需清理
var _ io.Writer = (*bytes.Buffer)(nil)
var _ = time.Second
```

- [ ] **Step 5: 跑测试通过 + Commit**

```bash
cd server && go get github.com/docker/docker && go mod tidy && go build ./... && go test ./internal/agent/ && git add -A && git commit -m "feat(server): replaceable agent runtime (local + docker)"
```

---

### Task 8: 引擎（阶段图 + 门禁 + 回环 + 终态）

**Files:**
- Create: `server/internal/engine/graph.go`
- Create: `server/internal/engine/engine.go`
- Test: `server/internal/engine/engine_test.go`

- [ ] **Step 1: 阶段图定义**

`server/internal/engine/graph.go`:
```go
package engine

// 阶段图：引擎只认识节点类型与下一跳，不认识具体业务。
type Kind int

const (
	KindAgent Kind = iota  // 需要 workdir 的 agent 节点
	KindGate               // 人工门禁（暂停）
	KindCommand            // 容器/命令节点（unit_test）
	KindTerminal           // 终态
)

type Node struct {
	Stage  string
	Kind   Kind
	Next   string // 成功下一跳
	OnFail string // 失败下一跳（unit_test 回环用），空 = 直接失败
}

// Stages 是全计划的唯一阶段清单（顺序即展示顺序）。
var Stages = []string{"intake", "spec", "spec_approval", "test_gen", "code_gen", "unit_test", "code_review"}

var Graph = map[string]Node{
	"intake":       {Stage: "intake", Kind: KindCommand, Next: "spec"},   // workspace.Acquire 在引擎入口执行
	"spec":         {Stage: "spec", Kind: KindAgent, Next: "spec_approval"},
	"spec_approval": {Stage: "spec_approval", Kind: KindGate, Next: "test_gen"},
	"test_gen":     {Stage: "test_gen", Kind: KindAgent, Next: "code_gen"},
	"code_gen":     {Stage: "code_gen", Kind: KindAgent, Next: "unit_test"},
	"unit_test":    {Stage: "unit_test", Kind: KindCommand, Next: "code_review", OnFail: "code_gen"},
	"code_review":  {Stage: "code_review", Kind: KindGate, Next: "DONE"},
}

const MaxFail = 3 // unit_test 连续失败上限，超过 = blocked
```

- [ ] **Step 2: 失败测试（FakeRunner + memory store 驱动全图）**

`server/internal/engine/engine_test.go`:
```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

type fakeRunner struct {
	failAt   map[string]int // role -> 失败次数（每次调用递减）
	calls    []string
}

func (f *fakeRunner) Run(_ context.Context, req agent.Request) (agent.Result, error) {
	f.calls = append(f.calls, req.Role)
	if req.Role == "spec" { return agent.Result{Output: "# 规格正文"}, nil }
	if req.Role == "test_gen" { return agent.Result{Output: "tests: a_test.go"}, nil }
	if req.Role == "code_gen" { return agent.Result{Output: "改了 2 个文件"}, nil }
	return agent.Result{Output: "review ok"}, nil
}

func newEngine(t *testing.T) (*Engine, *store.Memory, *fakeRunner) {
	st := store.NewMemory()
	proj := &store.Project{Name: "p"}
	require.NoError(t, st.CreateProject(context.Background(), proj))
	fr := &fakeRunner{}
	// workspace 用绿地域（root=临时目录）
	ws := newFakeWS(t)
	e := New(st, fr, ws, nil)
	return e, st, fr
}

// newFakeWS 在测试里给引擎一个「已就绪」的 workdir。
func newFakeWS(t *testing.T) *FakeWS {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644)
	return &FakeWS{dir: dir}
}

func TestPipelineHappyPath(t *testing.T) {
	e, st, _ := newEngine(t)
	ctx := context.Background()
	d := &store.Delivery{ProjectID: mustProj(t, st), Title: "需求A"}
	require.NoError(t, st.CreateDelivery(ctx, d))

	e.Start(ctx, d.ID) // 同步推进到第一个门禁

	got, _ := st.GetDelivery(ctx, d.ID)
	require.Equal(t, "spec_approval", got.CurrentStage)
	require.Equal(t, "spec_approval", got.PendingGate)
	require.NotEmpty(t, got.BaseCommit) // FakeWS 提供 base

	// artifacts: spec 已存
	arts, _ := st.ListArtifacts(ctx, d.ID)
	kinds := map[string]bool{}
	for _, a := range arts { kinds[a.Kind] = true }
	require.True(t, kinds["spec"])

	// 批准 → 走到 code_review 门禁
	require.NoError(t, e.Approve(ctx, d.ID))
	got, _ = st.GetDelivery(ctx, d.ID)
	require.Equal(t, "code_review", got.PendingGate)

	// 再批准 → completed，workdir 已释放
	require.NoError(t, e.Approve(ctx, d.ID))
	got, _ = st.GetDelivery(ctx, d.ID)
	require.Equal(t, "completed", got.Status)
	require.True(t, e.ws.(*FakeWS).released)
}

func TestUnitTestLoopAndBlocked(t *testing.T) {
	e, st, _ := newEngine(t)
	ctx := context.Background()
	d := &store.Delivery{ProjectID: mustProj(t, st), Title: "需求B"}
	require.NoError(t, st.CreateDelivery(ctx, d))
	e.testFail = true // 强制 unit_test 失败

	e.Start(ctx, d.ID)
	require.NoError(t, e.Approve(ctx, d.ID)) // spec 批准
	got, _ := st.GetDelivery(ctx, d.ID)
	// 第一次失败：回环 code_gen
	require.Equal(t, "code_gen", got.CurrentStage)
	require.Equal(t, 1, got.FailCount)

	// 连续失败到上限 → blocked
	e.Start(ctx, d.ID) // code_gen 会走到 unit_test 再失败
	e.Start(ctx, d.ID)
	e.Start(ctx, d.ID)
	got, _ = st.GetDelivery(ctx, d.ID)
	require.Equal(t, "blocked", got.Status)
}

func TestRejectLoopsBack(t *testing.T) {
	e, st, _ := newEngine(t)
	ctx := context.Background()
	d := &store.Delivery{ProjectID: mustProj(t, st), Title: "需求C"}
	require.NoError(t, st.CreateDelivery(ctx, d))
	e.Start(ctx, d.ID)
	require.NoError(t, e.Reject(ctx, d.ID, "重写"))
	got, _ := st.GetDelivery(ctx, d.ID)
	require.Equal(t, "spec", got.CurrentStage) // 回 spec 重写
	require.Equal(t, "", got.PendingGate)
}

func mustProj(t *testing.T, st *store.Memory) string {
	ps, _ := st.ListProjects(context.Background())
	require.NotEmpty(t, ps)
	return ps[0].ID
}
```

- [ ] **Step 3: 跑测试确认失败 → 实现引擎**

`server/internal/engine/engine.go`:
```go
// Package engine 流水线引擎：调度阶段图，门禁暂停，unit_test 回环，终态释放 workdir。
package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/store"
)

// Workspace 是引擎对工作区的全部依赖（*workspace.Manager 满足之；测试用 FakeWS）。
type Workspace interface {
	Acquire(deliveryID, repoURL, branch string) (string, string, error)
	Path(deliveryID string) string
	Release(deliveryID string)
}

// FakeWS 测试替身。
type FakeWS struct {
	dir      string
	released bool
}

func (f *FakeWS) Acquire(id, _, _ string) (string, string, error) { return f.dir, "b".PadRight(40), nil }
func (f *FakeWS) Path(id string) string                          { return f.dir }
func (f *FakeWS) Release(id string)                              { f.released = true }

// TestRunner 是引擎里命令节点（unit_test）的执行器接口。
type TestRunner interface {
	RunTests(ctx context.Context, workdir string) (pass bool, output string, err error)
}

type Engine struct {
	st  store.Store
	ar  agent.Runner
	ws  Workspace
	tr  TestRunner
	testFail bool // 测试钩子
}

func New(st store.Store, ar agent.Runner, ws Workspace, tr TestRunner) *Engine {
	return &Engine{st: st, ar: ar, ws: ws, tr: tr}
}

// Start 从当前阶段同步推进，直到门禁/终态/阻塞。
func (e *Engine) Start(ctx context.Context, deliveryID string) error {
	return e.run(ctx, deliveryID)
}

func (e *Engine) run(ctx context.Context, deliveryID string) error {
	for {
		d, err := e.st.GetDelivery(ctx, deliveryID)
		if err != nil { return err }
		if d.Status != "active" || d.PendingGate != "" { return nil }
		stop, err := e.step(ctx, d)
		if err != nil { return err }
		if stop { return nil }
	}
}

// step 执行当前阶段一次，返回是否停下（门禁/终态）。
func (e *Engine) step(ctx context.Context, d *store.Delivery) (bool, error) {
	node, ok := Graph[d.CurrentStage]
	if !ok { return true, fmt.Errorf("unknown stage %s", d.CurrentStage) }

	run := &store.StageRun{DeliveryID: d.ID, Stage: node.Stage}
	_ = e.st.StartStageRun(ctx, run)
	e.emit(ctx, d.ID, node.Stage, "stage_started", nil)

	var next string
	switch node.Kind {
	case KindAgent:
		out, err := e.runAgent(ctx, d, node.Stage)
		if err != nil {
			_ = e.st.FinishStageRun(ctx, run.ID, "failed")
			e.emit(ctx, d.ID, node.Stage, "stage_failed", map[string]string{"error": err.Error()})
			d.Status = "blocked"
			_ = e.st.UpdateDelivery(ctx, d)
			return true, nil
		}
		_ = e.st.SaveArtifact(ctx, &store.Artifact{DeliveryID: d.ID, Stage: node.Stage, Kind: artifactKind(node.Stage), Content: out})
		_ = e.st.FinishStageRun(ctx, run.ID, "done")
		next = node.Next

	case KindGate:
		_ = e.st.FinishStageRun(ctx, run.ID, "done")
		d.PendingGate = node.Stage
		_ = e.st.UpdateDelivery(ctx, d)
		e.emit(ctx, d.ID, node.Stage, "gate_pending", nil)
		return true, nil

	case KindCommand:
		if d.CurrentStage == "intake" {
			// intake 的"命令"就是 Acquire（在 Start 入口已做），直接过
			_ = e.st.FinishStageRun(ctx, run.ID, "done")
			next = node.Next
			break
		}
		// unit_test
		pass, out, err := e.runTests(ctx, d)
		_ = e.st.SaveArtifact(ctx, &store.Artifact{DeliveryID: d.ID, Stage: node.Stage, Kind: "test_output", Content: out})
		if err != nil || !pass {
			_ = e.st.FinishStageRun(ctx, run.ID, "failed")
			e.emit(ctx, d.ID, node.Stage, "test_failed", map[string]int{"fail_count": d.FailCount + 1})
			d.FailCount++
			if d.FailCount >= MaxFail {
				d.Status = "blocked"
				_ = e.st.UpdateDelivery(ctx, d)
				e.ws.Release(d.ID)
				return true, nil
			}
			next = node.OnFail
		} else {
			_ = e.st.FinishStageRun(ctx, run.ID, "done")
			e.emit(ctx, d.ID, node.Stage, "stage_done", nil)
			next = node.Next
		}

	case KindTerminal:
		return true, nil
	}

	if next == "DONE" {
		d.Status = "completed"
		d.CurrentStage = "code_review" // 停在审查位（已被批准）
		_ = e.st.UpdateDelivery(ctx, d)
		e.ws.Release(d.ID)
		e.emit(ctx, d.ID, d.CurrentStage, "delivery_completed", nil)
		return true, nil
	}
	d.CurrentStage = next
	_ = e.st.UpdateDelivery(ctx, d)
	return false, nil
}

func (e *Engine) runAgent(ctx context.Context, d *store.Delivery, stage string) (string, error) {
	dir := e.ws.Path(d.ID)
	spec := e.latestArtifact(ctx, d.ID, "spec")
	prompt := agent.BuildPrompt(stage, d.Description, spec)
	res, err := e.ar.Run(ctx, agent.Request{Role: stage, Prompt: prompt, Workdir: dir, Inputs: nil})
	return res.Output, err
}

func (e *Engine) runTests(ctx context.Context, d *store.Delivery) (bool, string, error) {
	if e.testFail { return false, "forced fail (test hook)", nil }
	if e.tr == nil { return true, "no test runner", nil }
	return e.tr.RunTests(ctx, e.ws.Path(d.ID))
}

func (e *Engine) latestArtifact(ctx context.Context, deliveryID, kind string) string {
	arts, err := e.st.ListArtifacts(ctx, deliveryID)
	if err != nil { return "" }
	v := ""
	for _, a := range arts { if a.Kind == kind { v = a.Content } }
	return v
}

func artifactKind(stage string) string {
	switch stage {
	case "spec": return "spec"
	case "test_gen": return "tests"
	case "code_gen": return "diff"
	case "code_review": return "agent_output"
	}
	return "agent_output"
}

// Approve 门禁放行：spec_approval→test_gen；code_review→DONE。
func (e *Engine) Approve(ctx context.Context, deliveryID string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil || d.PendingGate == "" { return fmt.Errorf("no pending gate") }
	e.emit(ctx, d.ID, d.PendingGate, "gate_approved", nil)
	d.PendingGate = ""
	node := Graph[d.CurrentStage]
	if node.Next == "DONE" {
		d.Status = "completed"
		_ = e.st.UpdateDelivery(ctx, d)
		e.ws.Release(d.ID)
		e.emit(ctx, d.ID, d.CurrentStage, "delivery_completed", nil)
		return nil
	}
	d.CurrentStage = node.Next
	_ = e.st.UpdateDelivery(ctx, d)
	return e.run(ctx, deliveryID)
}

// Reject 打回：spec_approval→spec；code_review→code_gen。
func (e *Engine) Reject(ctx context.Context, deliveryID, reason string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil || d.PendingGate == "" { return fmt.Errorf("no pending gate") }
	e.emit(ctx, d.ID, d.PendingGate, "gate_rejected", map[string]string{"reason": reason})
	back := "spec"
	if d.PendingGate == "code_review" { back = "code_gen" }
	d.PendingGate = ""
	d.CurrentStage = back
	_ = e.st.UpdateDelivery(ctx, d)
	return e.run(ctx, deliveryID)
}

func (e *Engine) emit(ctx context.Context, deliveryID, stage, typ string, payload any) {
	b, _ := json.Marshal(payload)
	_ = e.st.AppendEvent(ctx, &store.Event{DeliveryID: deliveryID, Stage: stage, EventType: typ, Payload: b})
}
```

**注意**：`FakeWS.Acquire` 里 `"b".PadRight(40)` 不是 Go 写法，实现时写 `strings.Repeat("b", 40)`（加 `strings` import）。`Start` 入口需要先做 workspace.Acquire 并写入 BaseCommit（测试断言了它）——在 `Engine.Start` 开头加：
```go
func (e *Engine) Start(ctx context.Context, deliveryID string) error {
	d, err := e.st.GetDelivery(ctx, deliveryID)
	if err != nil { return err }
	if d.BaseCommit == "" {
		proj, err := e.st.GetProject(ctx, d.ProjectID)
		if err != nil { return err }
		_, base, err := e.ws.Acquire(d.ID, proj.RepoURL, proj.DefaultBranch)
		if err != nil { return err }
		d.BaseCommit = base
		_ = e.st.UpdateDelivery(ctx, d)
		e.emit(ctx, d.ID, "intake", "workspace_ready", map[string]string{"base_commit": base})
	}
	return e.run(ctx, deliveryID)
}
```
（引擎需要能从 delivery 拿到 project：store 接口已有 GetProject。）

- [ ] **Step 4: 跑测试通过**

```bash
cd server && go test ./internal/engine/
```
Expected: PASS（三条全绿）

- [ ] **Step 5: Commit**

```bash
git add server && git commit -m "feat(server): stage-graph engine with gates, loop-back, terminal release"
```

---

### Task 9: API —— 认证 + projects（含 stats/pinned）

**Files:**
- Create: `server/internal/api/router.go`
- Create: `server/internal/api/auth.go`
- Create: `server/internal/api/projects.go`
- Test: `server/internal/api/api_test.go`

- [ ] **Step 1: 失败测试**

`server/internal/api/api_test.go`:
```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Memory) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil /*engine 由 deliveries 测试注入*/)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st
}

func login(t *testing.T, base string) *http.Client {
	client := &http.Client{}
	r, _ := http.Post(base+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"secret-pass"}`))
	require.Equal(t, 200, r.StatusCode)
	return client
}

func TestAuthGate(t *testing.T) {
	ts, _ := newServer(t)
	r, _ := http.Get(ts.URL + "/api/projects")
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)
	r, _ = c.Get(ts.URL + "/api/projects")
	require.Equal(t, 200, r.StatusCode)

	// me
	r, _ = c.Get(ts.URL + "/api/me")
	var me struct{ LoggedIn bool `json:"logged_in"` }
	_ = json.NewDecoder(r.Body).Decode(&me)
	require.True(t, me.LoggedIn)
}

func TestProjectsPinnedAndStats(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	// 创建项目
	r, _ := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"demo","repo_url":"","default_branch":"main"}`))
	require.Equal(t, 200, r.StatusCode)
	var p store.Project
	_ = json.NewDecoder(r.Body).Decode(&p)

	// 置顶
	req, _ := http.NewRequest("PATCH", ts.URL+"/api/projects/"+p.ID, bytes.NewBufferString(`{"pinned":true}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ = c.Do(req)
	require.Equal(t, 200, r.StatusCode)

	// stats：先塞一个 active + pending 的 delivery
	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{ProjectID: p.ID, Title: "x", Status: "active", PendingGate: "spec_approval"}))
	r, _ = c.Get(ts.URL + "/api/projects?include=stats")
	var list []map[string]any
	_ = json.NewDecoder(r.Body).Decode(&list)
	require.Len(t, list, 1)
	require.Equal(t, true, list[0]["pinned"])
	require.InEpsilon(t, float64(1), list[0]["stats"].(map[string]any)["active"], 0.001)
	require.InEpsilon(t, float64(1), list[0]["stats"].(map[string]any)["pending"], 0.001)
}
```

- [ ] **Step 2: 跑测试确认失败 → 实现认证**

```bash
cd server && go test ./internal/api/
```
Expected: FAIL

`server/internal/api/auth.go`:
```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// 单租户密码门：内存 session 表 + cookie。
type auth struct {
	password string
	mu       sync.Mutex
	sessions map[string]bool
}

func newAuth(password string) *auth { return &auth{password: password, sessions: map[string]bool{}} }

func (a *auth) login(w http.ResponseWriter, password string) bool {
	if password != a.password { return false }
	tok := randToken()
	a.mu.Lock(); a.sessions[tok] = true; a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "infera_session", Value: tok, Path: "/", HttpOnly: true})
	return true
}
func (a *auth) logout(r *http.Request) {
	if c, err := r.Cookie("infera_session"); err == nil {
		a.mu.Lock(); delete(a.sessions, c.Value); a.mu.Unlock()
	}
}
func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie("infera_session")
	if err != nil { return false }
	a.mu.Lock(); defer a.mu.Unlock()
	return a.sessions[c.Value]
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (a *auth) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = jsonWrite(w, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

var _ = chi.RouteContext // 占位（router.go 使用 chi）
```

- [ ] **Step 3: router + projects**

`server/internal/api/router.go`:
```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/tokfinity/infera/internal/store"
)

type EngineAPI interface {
	Start(ctx context.Context, deliveryID string) error // 引擎注入（避免包依赖环）
}

type Server struct {
	st   store.Store
	auth *auth
	engine EngineAPI
	mux  *chi.Mux
}

func NewServer(st store.Store, password string, engine EngineAPI) *Server {
	s := &Server{st: st, auth: newAuth(password), engine: engine}
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer)

	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })

	r.Group(func(pub chi.Router) {
		pub.Post("/api/login", s.handleLogin)
		pub.Get("/api/me", s.handleMe)
	})
	r.Group(func(priv chi.Router) {
		priv.Use(s.auth.middleware)
		priv.Get("/api/projects", s.handleListProjects)
		priv.Post("/api/projects", s.handleCreateProject)
		priv.Get("/api/projects/{id}", s.handleGetProject)
		priv.Patch("/api/projects/{id}", s.handlePatchProject)
		priv.Get("/api/projects/{id}/deliveries", s.handleListDeliveries)
		priv.Post("/api/projects/{id}/deliveries", s.handleCreateDelivery)
		priv.Get("/api/deliveries/{id}", s.handleGetDelivery)
		priv.Get("/api/deliveries/{id}/gate", s.handleGate)
		priv.Post("/api/deliveries/{id}/approve", s.handleApprove)
		priv.Post("/api/deliveries/{id}/reject", s.handleReject)
	})
	s.mux = r
	return s
}

func (s *Server) Mux() http.Handler { return s.mux }

func jsonWrite(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct{ Password string `json:"password"` }
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(400); return
	}
	if !s.auth.login(w, in.Password) {
		w.WriteHeader(401)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "密码错误"})
		return
	}
	jsonWrite(w, map[string]bool{"logged_in": true})
}
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	jsonWrite(w, map[string]bool{"logged_in": s.auth.valid(r)})
}
```

`server/internal/api/projects.go`:
```go
package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
)

var gitChecker *git.Git // main 注入（LsRemote 校验用）

func (s *Server) SetGit(g *git.Git) { gitChecker = g }

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.st.ListProjects(ctx)
	if err != nil { w.WriteHeader(500); return }
	type row struct {
		store.Project
		Stats *store.ProjectStats `json:"stats,omitempty"`
	}
	out := make([]row, 0, len(ps))
	withStats := r.URL.Query().Get("include") == "stats"
	for _, p := range ps {
		x := row{Project: p}
		if withStats {
			st, _ := s.st.ProjectStats(ctx, p.ID)
			x.Stats = &st
		}
		out = append(out, x)
	}
	jsonWrite(w, out)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := decode(r, &in); err != nil { w.WriteHeader(400); return }
	if in.Name == "" { http.Error(w, "name required", 400); return }
	if in.DefaultBranch == "" { in.DefaultBranch = "main" }
	// 可达性校验：ls-remote（毫秒级，不落盘）
	if in.RepoURL != "" && gitChecker != nil {
		if err := gitChecker.LsRemote(in.RepoURL); err != nil {
			http.Error(w, "仓库不可达或无权限: "+err.Error(), 400)
			return
		}
	}
	p := &store.Project{Name: in.Name, RepoURL: in.RepoURL, DefaultBranch: in.DefaultBranch}
	if err := s.st.CreateProject(r.Context(), p); err != nil { w.WriteHeader(500); return }
	jsonWrite(w, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.GetProject(r.Context(), chi.URLParam(r, "id"))
	if err == store.ErrNotFound { w.WriteHeader(404); return }
	if err != nil { w.WriteHeader(500); return }
	jsonWrite(w, p)
}

func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	var in struct{ Pinned *bool `json:"pinned"` }
	if err := decode(r, &in); err != nil || in.Pinned == nil { w.WriteHeader(400); return }
	id := chi.URLParam(r, "id")
	if err := s.st.PatchProjectPinned(r.Context(), id, *in.Pinned); err == store.ErrNotFound {
		w.WriteHeader(404); return
	} else if err != nil { w.WriteHeader(500); return }
	p, err := s.st.GetProject(r.Context(), id)
	if err != nil { w.WriteHeader(500); return }
	jsonWrite(w, p)
}

var _ = context.Background
```
`decode` 帮助函数放 `router.go`：
```go
func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
```
（deliveries/gate 四个 handler 在 Task 10 加；本 task 的测试不触达它们。）

- [ ] **Step 4: 跑测试通过 + Commit**

```bash
cd server && go get github.com/go-chi/chi/v5 && go mod tidy && go test ./internal/api/ && git add -A && git commit -m "feat(server): auth + projects API (stats/pinned)"
```

---

### Task 10: API —— deliveries（创建触发引擎 / 详情带 artifacts / gate）

**Files:**
- Create: `server/internal/api/deliveries.go`
- Modify: `server/internal/api/api_test.go`（追加测试）

- [ ] **Step 1: 追加失败测试**

在 `api_test.go` 末尾追加（`fakeEngine` 记录调用）：
```go
type fakeEngine struct{ started []string }

func (f *fakeEngine) Start(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

func newServerWithEngine(t *testing.T) (*httptest.Server, *store.Memory, *fakeEngine) {
	st := store.NewMemory()
	fe := &fakeEngine{}
	srv := NewServer(st, "secret-pass", fe)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st, fe
}

func TestDeliveryLifecycleAPI(t *testing.T) {
	ts, st, fe := newServerWithEngine(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	p := &store.Project{Name: "p"}
	_ = st.CreateProject(ctx, p)

	// 创建 delivery（触发引擎）
	r, _ := c.Post(ts.URL+"/api/projects/"+p.ID+"/deliveries", "application/json",
		bytes.NewBufferString(`{"title":"需求A","description":"描述"}`))
	require.Equal(t, 200, r.StatusCode)
	var d store.Delivery
	_ = json.NewDecoder(r.Body).Decode(&d)
	require.Equal(t, "intake", d.CurrentStage)
	require.Equal(t, []string{d.ID}, fe.started)

	// 引擎跑完 spec 停在门禁（模拟引擎写入）
	got, _ := st.GetDelivery(ctx, d.ID)
	got.CurrentStage, got.PendingGate = "spec_approval", "spec_approval"
	_ = st.UpdateDelivery(ctx, got)
	_ = st.SaveArtifact(ctx, &store.Artifact{DeliveryID: d.ID, Stage: "spec", Kind: "spec", Content: "# spec 正文"})
	_ = st.AppendEvent(ctx, &store.Event{DeliveryID: d.ID, Stage: "spec", EventType: "stage_done", Payload: []byte(`{}`)})

	// 详情：delivery + timeline + artifacts
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID)
	var detail struct {
		Delivery  store.Delivery   `json:"delivery"`
		Timeline  []store.Event    `json:"timeline"`
		Artifacts []store.Artifact `json:"artifacts"`
	}
	_ = json.NewDecoder(r.Body).Decode(&detail)
	require.Equal(t, "spec_approval", detail.Delivery.PendingGate)
	require.Len(t, detail.Timeline, 1)
	require.Len(t, detail.Artifacts, 1)

	// gate：spec 全文
	r, _ = c.Get(ts.URL + "/api/deliveries/" + d.ID + "/gate")
	var gate map[string]any
	_ = json.NewDecoder(r.Body).Decode(&gate)
	require.Equal(t, d.ID, gate["delivery_id"])
	require.Contains(t, gate["agent_output"].(map[string]any)["output"], "spec 正文")

	// approve / reject
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "", nil)
	require.Equal(t, 200, r.StatusCode)
	r, _ = c.Post(ts.URL+"/api/deliveries/"+d.ID+"/reject", "application/json", bytes.NewBufferString(`{"reason":"x"}`))
	require.Equal(t, 200, r.StatusCode)
}
```
（`fakeEngine.Start` 需要的 `context` 已在文件 import。）

- [ ] **Step 2: 跑测试确认失败 → 实现**

`server/internal/api/deliveries.go`:
```go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/tokfinity/infera/internal/store"
)

func (s *Server) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	ds, err := s.st.ListProjectDeliveries(r.Context(), chi.URLParam(r, "id"))
	if err != nil { w.WriteHeader(500); return }
	if ds == nil { ds = []store.Delivery{} }
	jsonWrite(w, ds)
}

func (s *Server) handleCreateDelivery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := decode(r, &in); err != nil || in.Title == "" { w.WriteHeader(400); return }
	d := &store.Delivery{ProjectID: chi.URLParam(r, "id"), Title: in.Title, Description: in.Description, Status: "active", CurrentStage: "intake"}
	if _, err := s.st.GetProject(r.Context(), d.ProjectID); err == store.ErrNotFound {
		w.WriteHeader(404); return
	}
	if err := s.st.CreateDelivery(r.Context(), d); err != nil { w.WriteHeader(500); return }
	_ = s.st.AppendEvent(r.Context(), &store.Event{DeliveryID: d.ID, Stage: "intake", EventType: "delivery_created", Payload: []byte(`{}`)})
	if s.engine != nil {
		go func() {
			ctx := r.Context()
			_ = s.engine.Start(ctx, d.ID)
		}()
	}
	jsonWrite(w, d)
}

func (s *Server) handleGetDelivery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.st.GetDelivery(ctx, chi.URLParam(r, "id"))
	if err == store.ErrNotFound { w.WriteHeader(404); return }
	if err != nil { w.WriteHeader(500); return }
	evs, _ := s.st.ListEvents(ctx, d.ID)
	arts, _ := s.st.ListArtifacts(ctx, d.ID)
	if evs == nil { evs = []store.Event{} }
	if arts == nil { arts = []store.Artifact{} }
	jsonWrite(w, map[string]any{"delivery": d, "timeline": evs, "artifacts": arts})
}

// handleGate 返回门禁审批所需的全部材料（spec 全文 / reviewer 意见 / PR 链接）。
func (s *Server) handleGate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := s.st.GetDelivery(ctx, chi.URLParam(r, "id"))
	if err == store.ErrNotFound { w.WriteHeader(404); return }
	if err != nil { w.WriteHeader(500); return }
	if d.PendingGate == "" { w.WriteHeader(400); _ = jsonWrite(w, map[string]string{"error": "no pending gate"}); return }

	out := struct {
		DeliveryID  string `json:"delivery_id"`
		Gate        string `json:"gate"`
		AgentOutput *struct {
			Agent  string `json:"agent"`
			Output string `json:"output"`
		} `json:"agent_output"`
		PRURL string `json:"pr_url"`
	}{DeliveryID: d.ID, Gate: d.PendingGate}

	kind := "spec"
	if d.PendingGate == "code_review" { kind = "agent_output" }
	arts, _ := s.st.ListArtifacts(ctx, d.ID)
	ao := &struct {
		Agent  string `json:"agent"`
		Output string `json:"output"`
	}{Agent: d.PendingGate}
	for _, a := range arts {
		if a.Kind == kind { ao.Output = a.Content }
		if a.Kind == "pr" { out.PRURL = a.Content }
	}
	out.AgentOutput = ao
	jsonWrite(w, out)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	// 见 Task 10 Step 2 下半段
	approveReject(s, w, r, true, "")
}
func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	var in struct{ Reason string `json:"reason"` }
	_ = decode(r, &in)
	approveReject(s, w, r, false, in.Reason)
}
```
approve/reject 依赖引擎——引擎不通过 EngineAPI 接口暴露 Approve/Reject，扩展接口（`router.go`）：
```go
type EngineAPI interface {
	Start(ctx context.Context, deliveryID string) error
	Approve(ctx context.Context, deliveryID string) error
	Reject(ctx context.Context, deliveryID, reason string) error
}
```
并实现：
```go
func approveReject(s *Server, w http.ResponseWriter, r *http.Request, ok bool, reason string) {
	id := chi.URLParam(r, "id")
	if s.engine == nil { w.WriteHeader(500); return }
	var err error
	if ok { err = s.engine.Approve(r.Context(), id) } else { err = s.engine.Reject(r.Context(), id, reason) }
	if err != nil { w.WriteHeader(400); _ = jsonWrite(w, map[string]string{"error": err.Error()}); return }
	d, gerr := s.st.GetDelivery(r.Context(), id)
	if gerr != nil { w.WriteHeader(500); return }
	jsonWrite(w, d)
}
```
同步给 `fakeEngine` 补方法：
```go
func (f *fakeEngine) Approve(_ context.Context, _ string) error { return nil }
func (f *fakeEngine) Reject(_ context.Context, _, _ string) error { return nil }
```

- [ ] **Step 3: 跑测试通过 + Commit**

```bash
cd server && go test ./internal/api/ && git add -A && git commit -m "feat(server): deliveries API (create triggers engine, artifacts, gate)"
```

---

### Task 11: WS 实时推送

**Files:**
- Create: `server/internal/api/ws.go`（hub 从 `server_legacy/internal/realtime/hub.go` 思路重写：map[deliveryID]map[conn]）
- Modify: `server/internal/api/router.go`（挂 `/ws`）
- Test: `server/internal/api/ws_test.go`

- [ ] **Step 1: hub + handler**

`server/internal/api/ws.go`:
```go
package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type hub struct {
	mu   sync.Mutex
	subs map[string]map[*websocket.Conn]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[*websocket.Conn]struct{}{}} }

func (h *hub) subscribe(deliveryID string, c *websocket.Conn) {
	h.mu.Lock(); defer h.mu.Unlock()
	if h.subs[deliveryID] == nil { h.subs[deliveryID] = map[*websocket.Conn]struct{}{} }
	h.subs[deliveryID][c] = struct{}{}
}
func (h *hub) unsubscribe(deliveryID string, c *websocket.Conn) {
	h.mu.Lock(); defer h.mu.Unlock()
	delete(h.subs[deliveryID], c)
	if len(h.subs[deliveryID]) == 0 { delete(h.subs, deliveryID) }
}
func (h *hub) publish(deliveryID string, payload any) {
	b, _ := json.Marshal(payload)
	h.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(h.subs[deliveryID]))
	for c := range h.subs[deliveryID] { conns = append(conns, c) }
	h.mu.Unlock()
	for _, c := range conns { _ = c.WriteMessage(websocket.TextMessage, b) }
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	deliveryID := r.URL.Query().Get("delivery")
	if deliveryID == "" { w.WriteHeader(400); return }
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil { return }
	s.hub.subscribe(deliveryID, c)
	defer func() { s.hub.unsubscribe(deliveryID, c); _ = c.Close() }()
	for {
		if _, _, err := c.ReadMessage(); err != nil { return }
	}
}
```
`Server` 结构体加 `hub *hub`（`NewServer` 里 `hub: newHub()`），router 公开组挂：
```go
r.Get("/ws", s.handleWS)
```

- [ ] **Step 2: 测试（订阅后 publish 收到消息）**

`server/internal/api/ws_test.go`:
```go
package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestWSPubSub(t *testing.T) {
	st := newMemStore()
	srv := NewServer(st, "pw", nil)
	ts := httptest.NewServer(srv.Mux())
	defer ts.Close()

	url := "ws" + ts.URL[4:] + "/ws?delivery=d1"
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	defer c.Close()
	time.Sleep(50 * time.Millisecond) // 等订阅生效

	srv.hub.publish("d1", map[string]string{"event": "stage_started"})
	var msg map[string]string
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	require.NoError(t, c.ReadJSON(&msg))
	require.Equal(t, "stage_started", msg["event"])
}
```
（`newMemStore` = `store.NewMemory()`，直接调用即可，省 helper：`st := store.NewMemory()`。补 import。）

- [ ] **Step 3: 引擎事件接 WS（engine 依赖注入 notifier）**

`server/internal/engine/engine.go`：`Engine` 增加 `Notify func(deliveryID, stage, eventType string)`；`emit` 末尾调用（nil 安全）。`main.go` 组装时把 `srv.Hub().publish` 包一层传给引擎。`Server` 加导出方法：
```go
func (s *Server) Hub() *hub { return s.hub }
```
引擎测试不受影响（Notify 为 nil 时跳过）。

- [ ] **Step 4: 跑全部测试 + Commit**

```bash
cd server && go get github.com/gorilla/websocket && go mod tidy && go test ./... && git add -A && git commit -m "feat(server): websocket realtime hub"
```

---

### Task 12: testrunner（unit_test 命令节点实现）+ main 组装

**Files:**
- Create: `server/internal/testrunner/testrunner.go`（本地 + docker 两种，从 `server_legacy/internal/testrunner/real.go` 搬运改造）
- Modify: `server/cmd/infera/main.go`（完整组装）

- [ ] **Step 1: testrunner**

`server/internal/testrunner/testrunner.go`:
```go
// Package testrunner 执行 unit_test 命令节点。
package testrunner

import (
	"context"
	"os/exec"
	"strings"
)

// Local 在主机执行命令（cmd 用 sh -c），退出码 0 = 通过。
type Local struct{ Script string }

func (l *Local) RunTests(_ context.Context, workdir string) (bool, string, error) {
	cmd := exec.Command("sh", "-c", l.Script)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	return err == nil, string(out), nil
}

// Docker 在 agent 容器里跑 go test（从 legacy real.go 搬运：bind workdir → /work，image 可配置）。
type Docker struct{ Image string }

func (d *Docker) RunTests(ctx context.Context, workdir string) (bool, string, error) {
	out, err := runInContainer(ctx, d.Image, []string{"go", "test", "./..."}, workdir)
	return err == nil, out, nil
}

var _ = strings.TrimSpace
```
（`runInContainer` 复用 `agent/docker.go` 的 SDK 调用模式——为避免重复，实现时把 DockerRunner 的容器执行抽成 `agent.RunInContainer(ctx, image, cmd, workdir) (string, error)` 导出函数，docker.go 与 testrunner 都调用它。）

- [ ] **Step 2: main.go 完整组装**

`server/cmd/infera/main.go`:
```go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

func main() {
	cfg := config.Load()
	if cfg.Password == "" {
		log.Fatal("INFERA_PASSWORD 未设置")
	}

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	st := store.NewPg(pool)

	g := git.New()
	g.Token = cfg.GitHubToken

	ws := workspace.New(cfg.RepoWorkRoot, g, 30*time.Minute) // 终态后保留 30min 供排查

	// agent runner：默认本地命令（AGENT_CMD），容器化后切 docker
	ar := agent.Runner(agent.NewLocal([]string{"sh", "-c", cfg.AgentCmd + " \"$INFERA_PROMPT\""}))
	if os.Getenv("AGENT_BACKEND") == "docker" {
		dr, err := agent.NewDocker(cfg.AgentImage, []string{cfg.AgentCmd})
		if err != nil { log.Fatalf("docker: %v", err) }
		ar = dr
	}

	tr := engine.TestRunner(&testrunner.Local{Script: cfg.TestCmd})

	srv := api.NewServer(st, cfg.Password, nil)
	srv.SetGit(g)

	eng := engine.New(st, ar, ws, tr)
	eng.Notify = func(deliveryID, stage, eventType string) {
		srv.Hub().Publish(deliveryID, map[string]string{"stage": stage, "event": eventType})
	}
	srv.SetEngine(eng) // Server.engine 可后期注入（NewServer 传 nil 的替代路径）

	log.Printf("infera listening on %s (workdir root %s)", cfg.Addr, cfg.RepoWorkRoot)
	log.Fatal(http.ListenAndServe(cfg.Addr, srv.Mux()))
}
```
需要的小改动（本 step 内完成）：
- `agent.LocalRunner` 支持从环境变量 `INFERA_PROMPT` 读 prompt（Run 里 `cmd.Env` 追加 `"INFERA_PROMPT="+req.Prompt`）。
- `api.Server` 加 `SetEngine(e EngineAPI)`；`NewServer` 的 engine 参数保留（测试用）。
- `hub.publish` 导出为 `Publish`（main 调用），ws.go 内部引用同步改名。
- `engine.Engine.Notify` 字段签名 `func(deliveryID, stage, eventType string)`。

- [ ] **Step 3: 编译 + 全测**

```bash
cd server && go build ./... && go test ./...
```
Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add server && git commit -m "feat(server): testrunner + full wiring in main"
```

---

### Task 13: E2E 冒烟（绿地项目 + fake agent，全链路）

**Files:**
- Create: `server/test/e2e_test.go`

- [ ] **Step 1: E2E 测试（起真服务器 + 真 pg 测试库 + fake agent 脚本）**

`server/test/e2e_test.go`:
```go
package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

// E2E：绿地项目（无仓库）+ fake agent 脚本，走完 创建→spec→门禁→批准→test_gen→code_gen→unit_test→code_review→completed。
func TestGreenfieldHappyPath(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" { t.Skip("TEST_DATABASE_URL 未设置") }
	if err := db.Migrate(dbURL); err != nil { t.Fatal(err) }
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	_, _ = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects`)
	st := store.NewPg(pool)

	// fake agent：按角色输出，并把"实现"写进 workdir
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec) echo "# 规格：新增 greeting" ;;
  test_gen) echo "tests: greet_test.go" ;;
  code_gen) echo hello > "$INFERA_WORKDIR/hello.txt"; echo "实现：hello.txt" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	root := t.TempDir()
	ws := workspace.New(root, git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f hello.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr)
	srv.SetEngine(eng)
	ts := startHTTP(t, srv.Mux())
	base := ts.URL

	client := loginE2E(t, base)

	// 1. 建项目（绿地）
	var proj store.Project
	post(t, client, base+"/api/projects", `{"name":"e2e"}`, &proj)
	require.Empty(t, proj.RepoURL)

	// 2. 建需求 → 引擎异步推进到 spec 门禁
	var d store.Delivery
	post(t, client, base+"/api/projects/"+proj.ID+"/deliveries", `{"title":"打招呼","description":"写 hello"}`, &d)
	require.Eventually(t, func() bool {
		var det struct{ Delivery store.Delivery `json:"delivery"` }
		get(t, client, base+"/api/deliveries/"+d.ID, &det)
		return det.Delivery.PendingGate == "spec_approval"
	}, 10*time.Second, 200*time.Millisecond, "应停在 spec 审批门")

	// 3. gate 能拿到 spec artifact
	var gate struct {
		AgentOutput struct{ Output string `json:"output"` } `json:"agent_output"`
	}
	get(t, client, base+"/api/deliveries/"+d.ID+"/gate", &gate)
	require.Contains(t, gate.AgentOutput.Output, "规格")

	// 4. 批准 spec → 推进到 code_review 门禁
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	require.Eventually(t, func() bool {
		var det struct{ Delivery store.Delivery `json:"delivery"` }
		get(t, client, base+"/api/deliveries/"+d.ID, &det)
		return det.Delivery.PendingGate == "code_review"
	}, 10*time.Second, 200*time.Millisecond)

	// 5. 批准 review → completed，workdir 清理排程
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	require.Eventually(t, func() bool {
		var det struct{ Delivery store.Delivery `json:"delivery"` }
		get(t, client, base+"/api/deliveries/"+d.ID, &det)
		return det.Delivery.Status == "completed" && det.Delivery.BaseCommit == ""
	}, 10*time.Second, 200*time.Millisecond)

	// 6. 详情含 artifacts（spec/tests/diff/test_output/agent_output）
	var detail struct{ Artifacts []store.Artifact `json:"artifacts"` }
	get(t, client, base+"/api/deliveries/"+d.ID, &detail)
	kinds := map[string]int{}
	for _, a := range detail.Artifacts { kinds[a.Kind]++ }
	require.GreaterOrEqual(t, kinds["spec"], 1)
	require.GreaterOrEqual(t, kinds["tests"], 1)
	require.GreaterOrEqual(t, kinds["test_output"], 1)
}

// —— 小工具 ——
func startHTTP(t *testing.T, h http.Handler) *httptest.ServerCompat { return nil } // 见下
```
（`startHTTP` 直接用 `httptest.NewServer(h)` + `t.Cleanup(ts.Close)`，无需 ServerCompat——上面占位行删掉，实现为：
```go
func startHTTP(t *testing.T, h http.Handler) *httptest.Server {
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}
```
以及 `loginE2E/post/get` 三个 helper：loginE2E 复用 api 包测试思路但本包独立实现——POST /api/login 拿 cookie 的 `http.Client{Jar: jar}`（`net/http/cookiejar`）；post/get 封装 JSON 请求。）
`config` import 未用到则移除。

- [ ] **Step 2: 跑 E2E**

```bash
docker start infera-postgres-test
cd server && TEST_DATABASE_URL='postgres://infera:infera@localhost:5434/infera_test?sslmode=disable' go test ./test/ -v -count=1
```
Expected: PASS（全链路 ~秒级）

- [ ] **Step 3: Commit**

```bash
git add server && git commit -m "test(server): greenfield E2E happy path"
```

---

### Task 14: 前端对接点核对（零改动或最小改动）

**Files:**
- Modify（仅必要时）: `apps/web/src/lib/infera-api.ts`、`apps/web/src/features/projects/projects-list.tsx`

- [ ] **Step 1: 契约核对**

逐项核对（curl 实测）：
- `GET /api/projects?include=stats` 返回 `stats.active/pending/last_activity` —— 若前端要用，`projects-list.tsx` 的 `useQueries` N+1 换成单请求（改动：删 useQueries，listProjects 加参数，卡片读 `p.stats`）。
- `PATCH /api/projects/:id` —— 前端 `togglePin` 改为乐观更新 + `fetch PATCH`。
- `GET /api/deliveries/:id` 已带 `artifacts` —— 需求详情页档案块可直接读。

- [ ] **Step 2: 前端切换 stats/pinned 到服务端**

`infera-api.ts` 加：
```ts
export async function listProjectsWithStats(): Promise<
  (Project & { stats?: { active: number; pending: number; last_activity: string } })[]
> {
  return json(await fetch('/api/projects?include=stats'))
}
export async function patchProjectPinned(id: string, pinned: boolean): Promise<Project> {
  return json(
    await fetch(`/api/projects/${id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pinned }),
    }),
  )
}
```
`projects-list.tsx`：`useQueries` 块删除，`listProjects` → `listProjectsWithStats`，卡片摘要改读 `proj.stats`；`togglePin` 乐观 set + `patchProjectPinned` 失败回滚。置顶仍按服务端 `pinned` 排序（去掉 localStorage）。

- [ ] **Step 3: 前端验证**

```bash
cd apps/web && npx tsc -b && (vite 已在跑则热更) 
curl -s -b /tmp/infera-jar 'localhost:8080/api/projects?include=stats' | head -c 400
```
Expected: tsc 0 错误；curl 返回带 stats 的列表。

- [ ] **Step 4: Commit**

```bash
git add apps/web && git commit -m "feat(web): server-backed project stats + pinning"
```

---

### Task 15: 收尾 —— 删 legacy、切 run-dev、文档

**Files:**
- Delete: `server_legacy/`
- Modify: `run-dev.sh`, `README.md`（若存在相关段落）

- [ ] **Step 1: 新后端先在主库（5433）起一遍**

```bash
cd server && DATABASE_URL='postgres://infera:infera@localhost:5433/infera?sslmode=disable' INFERA_PASSWORD=$(grep ^INFERA_PASSWORD= ../.env | cut -d= -f2-) go run ./cmd/infera &
sleep 3; curl -s localhost:8080/api/health; kill %1
```
Expected: `ok`（注意 5433 库会被 v1 迁移建新表；旧表 drop 由用户确认后手动执行——**不要自动 DROP**，在 Step 3 提示里说明）

- [ ] **Step 2: 删 legacy**

```bash
git rm -r server_legacy && git commit -m "chore: remove legacy backend"
```

- [ ] **Step 3: run-dev.sh 切新后端 + 文档**

`run-dev.sh` 中 `cd server && go run ./cmd/server` 改为 `go run ./cmd/infera`；注释里的迁移说明改为"启动时自动迁移（v1 起）"。
README（或 docs）加一段：新后端架构图（spec 链接）、`AGENT_CMD`/`AGENT_BACKEND`/`TEST_CMD` 环境变量说明、pi 接入示例（`AGENT_CMD=pi AGENT_BACKEND=docker`）。

- [ ] **Step 4: 全量验证 + Commit**

```bash
cd server && go test ./... && cd .. && cd apps/web && npx tsc -b
git add -A && git commit -m "chore: switch run-dev to new backend, docs"
```

---

## Self-Review 记录

- 规格覆盖：workdir 前置（Task 6/8）、agent 共享 workdir（Task 7/8）、ls-remote 校验（Task 5/9）、base_commit 快照（Task 6/8）、终态延迟清理（Task 6）、阶段图可扩展（Task 8）、artifacts API（Task 10）、stats/pinned（Task 9/14）、事件 WS（Task 11）、agent 可替换（Task 7 `AGENT_CMD`/`AGENT_BACKEND`，Task 15 pi 示例）✅
- 已知简化（有意）：PR 开具（code_review 后 push+PR）未进本计划——旧逻辑依赖真实 GitHub token 且 spec 将其列为非目标之外的第一优先级缺口，**列入完成后的第一项跟进**；引擎 goroutine 无并发锁（单交付单 goroutine，多交付并行安全因 store 各自事务）——E2E 后如有竞态再补。
- 类型一致性：`store.Store` 接口方法名在 Task 3 定义后全程引用一致；`EngineAPI` 在 Task 9 定义、Task 10 扩展 Approve/Reject、Task 12 SetEngine——执行时按最终签名实现。
