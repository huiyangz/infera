# infera P1（地基）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 搭起 infera 的后端 + 前端骨架与核心数据模型，让一条 Delivery 能按静态 stage 状态机从「需求」推进到「部署」（stage 内部先 stub，不接 Claude Code），并在最小 Web UI 上创建和查看 Delivery。

**Architecture:** Go (chi + sqlc + pgx) 后端 + Next.js (App Router) 前端 + PostgreSQL。后端按 handler / service / sqlc-generated 分层。Delivery 是中心实体，按固定 stage 序列推进；每次推进写一条 timeline event。stage 执行在 P1 是 stub（只改状态 + 记录），Agent 真实执行留给 P2。

**Tech Stack:** Go 1.22 · chi/v5 · sqlc · pgx/v5 · golang-migrate · testify · PostgreSQL 16 · Next.js 14 (App Router) · TypeScript · TanStack Query · Tailwind CSS · docker-compose

**Spec:** `docs/superpowers/specs/2026-08-12-infera-product-design.md`（第 5、6、7 节定义概念与 stage）

---

## 文件结构总览

```
infera/
├── docker-compose.yml                  # 本地 postgres
├── server/                             # Go 后端（独立 module）
│   ├── go.mod                          # module github.com/tokfinity/infera
│   ├── sqlc.yaml                       # sqlc 配置
│   ├── migrations/                     # golang-migrate SQL 迁移
│   │   ├── 000001_deliveries.up.sql
│   │   ├── 000001_deliveries.down.sql
│   │   ├── 000002_timeline_agents.up.sql
│   │   └── 000002_timeline_agents.down.sql
│   ├── cmd/server/main.go              # HTTP server 入口
│   ├── internal/
│   │   ├── config/config.go            # 环境变量配置
│   │   ├── db/db.go                    # DB 连接 + 测试 helper
│   │   ├── stage/stage.go              # stage 状态机定义
│   │   ├── handler/delivery.go         # Delivery HTTP handlers
│   │   ├── handler/router.go           # chi 路由装配
│   │   └── service/delivery.go         # Delivery 业务逻辑
│   └── pkg/db/
│       ├── queries/deliveries.sql      # sqlc 查询
│       ├── queries/timeline.sql
│       └── generated/                  # sqlc 生成（不手改）
└── apps/web/                           # Next.js 前端
    ├── package.json
    ├── app/
    │   ├── layout.tsx
    │   ├── page.tsx                    # Delivery 列表 + 创建
    │   └── deliveries/[id]/page.tsx    # Delivery 详情（stage + timeline）
    ├── lib/api.ts                      # API client
    └── lib/types.ts
```

---

## Task 1: 仓库初始化 + docker-compose（postgres）

**Files:**
- Create: `.gitignore`
- Create: `docker-compose.yml`

- [ ] **Step 1: 初始化 git 仓库**

```bash
cd /Users/huiyangz/tokfinity/infera
git init
git config user.email "you@example.com"  # 按实际改
git config user.name "you"
```

- [ ] **Step 2: 写 .gitignore**

```gitignore
# Go
server/bin/
server/tmp/
*.test
*.out

# Node / Next.js
node_modules/
apps/web/.next/
apps/web/out/

# Env / OS
.env
.env.local
.DS_Store

# sqlc 生成不忽略（提交进库以保证可构建）
```

- [ ] **Step 3: 写 docker-compose.yml**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    container_name: infera-postgres
    environment:
      POSTGRES_USER: infera
      POSTGRES_PASSWORD: infera
      POSTGRES_DB: infera
    ports:
      - "5432:5432"
    volumes:
      - infera-pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U infera"]
      interval: 2s
      timeout: 2s
      retries: 10

volumes:
  infera-pgdata:
```

- [ ] **Step 4: 启动 postgres 并验证**

```bash
docker compose up -d
docker compose exec postgres psql -U infera -c "SELECT version();" | head -1
```
Expected: 打印一行 `PostgreSQL 16.x ...`

- [ ] **Step 5: 提交**

```bash
git add .gitignore docker-compose.yml docs/
git commit -m "chore: init repo with docker-compose postgres"
```

---

## Task 2: Go server 脚手架（chi + health）

**Files:**
- Create: `server/go.mod`
- Create: `server/internal/config/config.go`
- Create: `server/cmd/server/main.go`
- Create: `server/internal/handler/router.go`
- Create: `server/internal/handler/health_test.go`
- Create: `server/internal/handler/health.go`

- [ ] **Step 1: 初始化 Go module + 依赖**

```bash
cd server
go mod init github.com/tokfinity/infera
go get github.com/go-chi/chi/v5@latest
go get github.com/jackc/pgx/v5/stdlib@latest
go mod tidy
```

- [ ] **Step 2: 写 config**

`server/internal/config/config.go`:
```go
package config

import "os"

type Config struct {
	DatabaseURL string
	Port        string
}

func Load() Config {
	return Config{
		DatabaseURL: getenv("DATABASE_URL", "postgres://infera:infera@localhost:5432/infera?sslmode=disable"),
		Port:        getenv("PORT", "8080"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 3: 写 health handler（先写测试）**

`server/internal/handler/health_test.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}
```

- [ ] **Step 4: 跑测试确认失败**

```bash
go get github.com/stretchr/testify@latest
go test ./internal/handler/ -run TestHealth -v
```
Expected: FAIL / 编译错误（`Health` 未定义）

- [ ] **Step 5: 实现 health handler**

`server/internal/handler/health.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
)

func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 6: 跑测试确认通过**

```bash
go test ./internal/handler/ -run TestHealth -v
```
Expected: PASS

- [ ] **Step 7: 写 router**

`server/internal/handler/router.go`:
```go
package handler

import "github.com/go-chi/chi/v5"

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)
	return r
}
```

- [ ] **Step 8: 写 main.go**

`server/cmd/server/main.go`:
```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/handler"
)

func main() {
	cfg := config.Load()
	r := handler.NewRouter()
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
```

- [ ] **Step 9: 手动验证 server 可启动**

```bash
go run ./cmd/server &
sleep 1
curl -s http://localhost:8080/health
kill %1
```
Expected: `{"status":"ok"}`

- [ ] **Step 10: 提交**

```bash
git add server/
git commit -m "feat(server): chi skeleton with health endpoint"
```

---

## Task 3: 迁移 — deliveries 表

**Files:**
- Create: `server/migrations/000001_deliveries.up.sql`
- Create: `server/migrations/000001_deliveries.down.sql`

- [ ] **Step 1: 安装 golang-migrate CLI（macOS）**

```bash
brew install golang-migrate
migrate -version
```
Expected: 打印版本号（如 `4.17.x`）

- [ ] **Step 2: 写 up 迁移**

`server/migrations/000001_deliveries.up.sql`:
```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE delivery_status AS ENUM ('active', 'completed', 'blocked');

CREATE TABLE deliveries (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title         text NOT NULL,
    description   text NOT NULL DEFAULT '',
    repo_url      text NOT NULL DEFAULT '',
    branch        text NOT NULL DEFAULT '',
    status        delivery_status NOT NULL DEFAULT 'active',
    current_stage text NOT NULL DEFAULT 'intake',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX deliveries_status_idx ON deliveries(status);
```

- [ ] **Step 3: 写 down 迁移**

`server/migrations/000001_deliveries.down.sql`:
```sql
DROP TABLE IF EXISTS deliveries;
DROP TYPE IF EXISTS delivery_status;
```

- [ ] **Step 4: 跑迁移并验证**

```bash
cd server
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
docker compose exec postgres psql -U infera -c "\d deliveries"
```
Expected: 表结构打印出来，含 `id`、`title`、`current_stage` 等列

- [ ] **Step 5: 回滚再上行，确认可逆**

```bash
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" down
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
```
Expected: 两次都成功

- [ ] **Step 6: 提交**

```bash
git add server/migrations/
git commit -m "feat(db): deliveries table migration"
```

---

## Task 4: 迁移 — timeline_events + agent_configs 表

**Files:**
- Create: `server/migrations/000002_timeline_agents.up.sql`
- Create: `server/migrations/000002_timeline_agents.down.sql`

- [ ] **Step 1: 写 up 迁移**

`server/migrations/000002_timeline_agents.up.sql`:
```sql
CREATE TABLE timeline_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       text NOT NULL,
    event_type  text NOT NULL,
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX timeline_events_delivery_idx ON timeline_events(delivery_id, created_at);

CREATE TYPE agent_role AS ENUM ('spec', 'test', 'coder', 'reviewer');

CREATE TABLE agent_configs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    role       agent_role NOT NULL,
    config     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
```

- [ ] **Step 2: 写 down 迁移**

`server/migrations/000002_timeline_agents.down.sql`:
```sql
DROP TABLE IF EXISTS agent_configs;
DROP TYPE IF EXISTS agent_role;
DROP TABLE IF EXISTS timeline_events;
```

- [ ] **Step 3: 跑迁移并验证**

```bash
cd server
migrate -path migrations -database "postgres://infera:infera@localhost:5432/infera?sslmode=disable" up
docker compose exec postgres psql -U infera -c "\d timeline_events"
docker compose exec postgres psql -U infera -c "\d agent_configs"
```
Expected: 两张表结构打印出来

- [ ] **Step 4: 提交**

```bash
git add server/migrations/
git commit -m "feat(db): timeline_events and agent_configs migrations"
```

---

## Task 5: sqlc 配置 + DB 连接 + 测试 helper

**Files:**
- Create: `server/sqlc.yaml`
- Create: `server/internal/db/db.go`

- [ ] **Step 1: 安装 sqlc CLI（macOS）**

```bash
brew install sqlc
sqlc version
```
Expected: 打印版本号（如 `v1.25.x`）

- [ ] **Step 2: 写 sqlc.yaml**

`server/sqlc.yaml`:
```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "pkg/db/queries/"
    schema: "migrations/"
    gen:
      go:
        package: "generated"
        out: "pkg/db/generated"
        sql_package: "pgx/v5"
        emit_json_tags: true
        emit_pointers_for_null_types: true
```

- [ ] **Step 3: 写 DB 连接包**

`server/internal/db/db.go`:
```go
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql 驱动，测试用
)

// Pool 返回一个 pgx 连接池。
func Pool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// OpenSQL 打开一个 *database/sql 句柄（测试与 migrate 用）。
func OpenSQL(databaseURL string) (*sql.DB, error) {
	return sql.Open("pgx", databaseURL)
}
```

- [ ] **Step 4: 跑依赖整理**

```bash
cd server
go get github.com/jackc/pgx/v5/pgxpool@latest
go mod tidy
```

- [ ] **Step 5: 验证编译**

```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add server/sqlc.yaml server/internal/db/ server/go.mod server/go.sum
git commit -m "feat(server): sqlc config and db connection pool"
```

---

## Task 6: sqlc 查询 — deliveries

**Files:**
- Create: `server/pkg/db/queries/deliveries.sql`

- [ ] **Step 1: 写 deliveries 查询**

`server/pkg/db/queries/deliveries.sql`:
```sql
-- name: CreateDelivery :one
INSERT INTO deliveries (title, description, repo_url, branch)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetDelivery :one
SELECT * FROM deliveries WHERE id = $1;

-- name: ListDeliveries :many
SELECT * FROM deliveries ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: UpdateDeliveryStage :one
UPDATE deliveries
SET current_stage = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateDeliveryStatus :one
UPDATE deliveries
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;
```

- [ ] **Step 2: 生成代码**

```bash
cd server
sqlc generate
ls pkg/db/generated/
```
Expected: 看到 `db.go`、`deliveries.sql.go`、`models.go` 等生成文件

- [ ] **Step 3: 验证编译**

```bash
go build ./...
```
Expected: 无错误

- [ ] **Step 4: 提交**

```bash
git add server/pkg/db/
git commit -m "feat(db): sqlc queries for deliveries"
```

---

## Task 7: sqlc 查询 — timeline_events + agent_configs

**Files:**
- Create: `server/pkg/db/queries/timeline.sql`
- Create: `server/pkg/db/queries/agents.sql`

- [ ] **Step 1: 写 timeline 查询**

`server/pkg/db/queries/timeline.sql`:
```sql
-- name: CreateTimelineEvent :one
INSERT INTO timeline_events (delivery_id, stage, event_type, payload)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListTimelineEvents :many
SELECT * FROM timeline_events
WHERE delivery_id = $1
ORDER BY created_at ASC;
```

- [ ] **Step 2: 写 agents 查询**

`server/pkg/db/queries/agents.sql`:
```sql
-- name: GetAgentByRole :one
SELECT * FROM agent_configs WHERE role = $1 LIMIT 1;
```

- [ ] **Step 3: 生成 + 编译**

```bash
cd server
sqlc generate
go build ./...
```
Expected: 无错误

- [ ] **Step 4: 提交**

```bash
git add server/pkg/db/
git commit -m "feat(db): sqlc queries for timeline and agents"
```

---

## Task 8: stage 状态机包

**Files:**
- Create: `server/internal/stage/stage.go`
- Create: `server/internal/stage/stage_test.go`

- [ ] **Step 1: 先写测试**

`server/internal/stage/stage_test.go`:
```go
package stage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNextOfEachStage(t *testing.T) {
	cases := []struct{ from, want string }{
		{"intake", "spec"},
		{"spec", "spec_approval"},
		{"spec_approval", "test_gen"},
		{"test_gen", "code_gen"},
		{"code_gen", "unit_test"},
		{"unit_test", "code_review"},
		{"code_review", "deploy"},
	}
	for _, c := range cases {
		got, ok := Next(c.from)
		assert.True(t, ok, "stage %q should have a next", c.from)
		assert.Equal(t, c.want, got)
	}
}

func TestNextOfDeployIsEmpty(t *testing.T) {
	_, ok := Next("deploy")
	assert.False(t, ok, "deploy has no next")
}

func TestIsGate(t *testing.T) {
	assert.True(t, IsGate("spec_approval"))
	assert.True(t, IsGate("code_review"))
	assert.False(t, IsGate("code_gen"))
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/stage/ -v
```
Expected: FAIL / 编译错误（`Next`、`IsGate` 未定义）

- [ ] **Step 3: 实现 stage 包**

`server/internal/stage/stage.go`:
```go
package stage

// 固定的 stage 序列（P1 静态模板）。顺序即推进顺序。
var order = []string{
	"intake",         // 需求
	"spec",           // Spec Agent 写 spec
	"spec_approval",  // 人审批 spec（gate）
	"test_gen",       // Test Agent 生成用例 + 单测
	"code_gen",       // Coder Agent 写实现（修复 hub）
	"unit_test",      // 系统跑单测
	"code_review",    // Reviewer Agent 预审 + 人批准（gate）
	"deploy",         // 部署
}

var gates = map[string]bool{
	"spec_approval": true,
	"code_review":   true,
}

// All 返回全部 stage 的拷贝。
func All() []string {
	out := make([]string, len(order))
	copy(out, order)
	return out
}

// Next 返回 from 的下一个 stage；若 from 是最后一个，ok=false。
func Next(from string) (string, bool) {
	for i, s := range order {
		if s == from && i+1 < len(order) {
			return order[i+1], true
		}
	}
	return "", false
}

// IsGate 报告 s 是否是需要人介入的 gate stage。
func IsGate(s string) bool {
	return gates[s]
}

// IsValid 报告 s 是否是合法 stage。
func IsValid(s string) bool {
	for _, x := range order {
		if x == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/stage/ -v
```
Expected: PASS（3 个测试全过）

- [ ] **Step 5: 提交**

```bash
git add server/internal/stage/
git commit -m "feat(server): stage state machine"
```

---

## Task 9: DB 测试 helper + Delivery service（创建）

**Files:**
- Create: `server/internal/dbtest/dbtest.go`
- Create: `server/internal/service/delivery.go`
- Create: `server/internal/service/delivery_test.go`

> 说明：DB 测试连一个独立的 `infera_test` 库。先建它：`docker compose exec postgres createdb -U infera infera_test`。每个测试函数运行前 migrate，并在事务里 rollback 隔离。

- [ ] **Step 1: 建 test 库**

```bash
docker compose exec postgres createdb -U infera infera_test
```

- [ ] **Step 2: 写 dbtest helper**

`server/internal/dbtest/dbtest.go`:
```go
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testDBURL = "postgres://infera:infera@localhost:5432/infera_test?sslmode=disable"

// Migrate 对 test 库跑全部迁移。测试 main 里调一次即可。
func Migrate(t *testing.T) {
	t.Helper()
	m, err := migrate.New("file://migrations", testDBURL)
	if err != nil {
		t.Fatalf("migrate new: %v", err)
	}
	if err := m.Up(); err != nil && err.Error() != "no change" {
		t.Fatalf("migrate up: %v", err)
	}
	_ = m.Close()
}

// Pool 返回连到 test 库的连接池。
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDBURL)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	return pool
}

// Truncate 清空给定表，保证测试间隔离。
func Truncate(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, tb := range tables {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tb)); err != nil {
			t.Fatalf("truncate %s: %v", tb, err)
		}
	}
}

// 保留 sql.DB 别名，供 sqlc 生成的 Queries 在事务里使用。
func SQLTx(t *testing.T, pool *pgxpool.Pool) (*sql.DB, *sql.Tx, func()) {
	t.Helper()
	db, err := sql.Open("pgx", testDBURL)
	if err != nil {
		t.Fatalf("open sql: %v", err)
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	cleanup := func() {
		_ = tx.Rollback()
		_ = db.Close()
	}
	return db, tx, cleanup
}
```

- [ ] **Step 3: 装依赖**

```bash
cd server
go get github.com/golang-migrate/migrate/v4@latest
go mod tidy
```

- [ ] **Step 4: 写 service 测试**

`server/internal/service/delivery_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/pkg/db/generated"
)

func TestCreateDelivery(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(generated.New(pool))
	d, err := svc.Create(context.Background(), CreateInput{
		Title:       "忘记密码功能",
		Description: "登录页加重置流程",
		RepoURL:     "https://github.com/acme/web",
	})
	assert.NoError(t, err)
	assert.Equal(t, "忘记密码功能", d.Title)
	assert.Equal(t, "intake", d.CurrentStage)
	assert.Equal(t, "active", string(d.Status))
}
```

- [ ] **Step 5: 跑测试确认失败**

```bash
go test ./internal/service/ -run TestCreateDelivery -v
```
Expected: FAIL / 编译错误（`New`、`CreateInput`、`Create` 未定义）

- [ ] **Step 6: 实现 service**

`server/internal/service/delivery.go`:
```go
package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryService struct {
	q *generated.Queries
}

func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool)}
}

type CreateInput struct {
	Title       string
	Description string
	RepoURL     string
	Branch      string
}

// Create 建一条新 Delivery，初始 stage = intake。
func (s *DeliveryService) Create(ctx context.Context, in CreateInput) (generated.Delivery, error) {
	if in.Title == "" {
		return generated.Delivery{}, fmt.Errorf("title is required")
	}
	d, err := s.q.CreateDelivery(ctx, generated.CreateDeliveryParams{
		Title:       in.Title,
		Description: in.Description,
		RepoUrl:     in.RepoURL,
		Branch:      in.Branch,
	})
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("create delivery: %w", err)
	}
	return d, nil
}
```

> 注意：sqlc 生成字段名按数据库列名 camelCase（`repo_url` → `RepoUrl`）。若实际生成名不同，以 `pkg/db/generated/models.go` 为准调整。

- [ ] **Step 7: 跑测试确认通过**

```bash
go test ./internal/service/ -run TestCreateDelivery -v
```
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add server/internal/dbtest/ server/internal/service/ server/go.mod server/go.sum
git commit -m "feat(server): delivery service with create"
```

---

## Task 10: Delivery handlers + 路由（创建 / 列表 / 详情）

**Files:**
- Create: `server/internal/handler/delivery.go`
- Modify: `server/internal/handler/router.go`
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: 写 delivery handler**

`server/internal/handler/delivery.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/service"
	"github.com/tokfinity/infera/pkg/db/generated"
)

type DeliveryHandler struct {
	svc    *service.DeliveryService
	pool   *pgxpool.Pool
}

func NewDeliveryHandler(pool *pgxpool.Pool) *DeliveryHandler {
	return &DeliveryHandler{svc: service.New(pool), pool: pool}
}

type createDeliveryReq struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	RepoURL     string `json:"repo_url"`
	Branch      string `json:"branch"`
}

func (h *DeliveryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createDeliveryReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, err := h.svc.Create(r.Context(), service.CreateInput{
		Title: req.Title, Description: req.Description, RepoURL: req.RepoURL, Branch: req.Branch,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (h *DeliveryHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := generated.New(h.pool)
	items, err := q.ListDeliveries(r.Context(), generated.ListDeliveriesParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DeliveryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	q := generated.New(h.pool)
	d, err := q.GetDelivery(r.Context(), uuidOrNil(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	tl, _ := q.ListTimelineEvents(r.Context(), d.ID)
	writeJSON(w, http.StatusOK, map[string]any{"delivery": d, "timeline": tl})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
```

- [ ] **Step 2: 加 uuidOrNil 辅助（同文件追加）**

在 `server/internal/handler/delivery.go` 末尾追加：
```go
import "github.com/google/uuid"

func uuidOrNil(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil
	}
	return id
}
```

> 注意：Go 不允许在文件中间 import，把上面 `import "github.com/google/uuid"` 合并到文件顶部 import 块。即顶部 import 块应含 `"github.com/google/uuid"`。

- [ ] **Step 3: 装依赖**

```bash
cd server
go get github.com/google/uuid@latest
go mod tidy
```

- [ ] **Step 4: 更新 router 装配 delivery 路由**

`server/internal/handler/router.go`（整体替换）:
```go
package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(pool *pgxpool.Pool) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)

	dh := NewDeliveryHandler(pool)
	r.Route("/api/deliveries", func(r chi.Router) {
		r.Post("/", dh.Create)
		r.Get("/", dh.List)
		r.Get("/{id}", dh.Get)
		r.Post("/{id}/advance", dh.Advance) // 在 Task 11 实现
	})
	return r
}
```

- [ ] **Step 5: 更新 main.go 注入 pool**

`server/cmd/server/main.go`（整体替换）:
```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/tokfinity/infera/internal/config"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/handler"
)

func main() {
	cfg := config.Load()

	pool, err := db.Pool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	r := handler.NewRouter(pool)
	addr := ":" + cfg.Port
	fmt.Println("infera server listening on", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
```

- [ ] **Step 6: 暂时给 Advance 一个桩（Task 11 替换）**

在 `server/internal/handler/delivery.go` 追加：
```go
func (h *DeliveryHandler) Advance(w http.ResponseWriter, r *http.Request) {
	writeErr(w, http.StatusNotImplemented, "advance not implemented yet")
}
```

- [ ] **Step 7: 验证编译 + 手动跑**

```bash
go build ./...
go run ./cmd/server &
sleep 1
curl -s -X POST http://localhost:8080/api/deliveries -H 'Content-Type: application/json' -d '{"title":"忘记密码","description":"x","repo_url":"https://github.com/acme/web"}'
curl -s http://localhost:8080/api/deliveries
kill %1
```
Expected: POST 返回 201 + 新建的 Delivery；GET 返回数组含它

- [ ] **Step 8: 提交**

```bash
git add server/
git commit -m "feat(server): delivery CRUD handlers and routes"
```

---

## Task 11: Advance service + handler（推进 stage + timeline，stub 执行）

**Files:**
- Modify: `server/internal/service/delivery.go`
- Create: `server/internal/service/delivery_advance_test.go`
- Modify: `server/internal/handler/delivery.go`

> P1 的 advance 是 stub：不接 Agent，只推进到下一个 stage，并写一条 timeline event。这验证状态机和留痕。

- [ ] **Step 1: 写 advance 测试**

`server/internal/service/delivery_advance_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
)

func TestAdvanceMovesToNextStage(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(pool)
	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})

	advanced, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "spec", advanced.CurrentStage)
}

func TestAdvanceFromDeployCompletes(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries")

	svc := New(pool)
	d, _ := svc.Create(context.Background(), CreateInput{Title: "t"})
	// 手动把 stage 推到 deploy，模拟已经走到最后一步前
	_, _ = pool.Exec(context.Background(), "UPDATE deliveries SET current_stage='deploy' WHERE id=$1", d.ID)

	advanced, err := svc.Advance(context.Background(), d.ID)
	assert.NoError(t, err)
	assert.Equal(t, "completed", string(advanced.Status))
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/service/ -run TestAdvance -v
```
Expected: FAIL（`Advance` 未定义）

- [ ] **Step 3: 实现 Advance**

在 `server/internal/service/delivery.go` 追加：
```go
import (
	// 顶部 import 块追加：
	"github.com/google/uuid"
	"github.com/tokfinity/infera/internal/stage"
)

// Advance 把 Delivery 推进到下一个 stage，并写一条 timeline event。
// 若当前已在 deploy，则标记 completed。
func (s *DeliveryService) Advance(ctx context.Context, id uuid.UUID) (generated.Delivery, error) {
	d, err := s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("get delivery: %w", err)
	}

	next, ok := stage.Next(d.CurrentStage)
	if !ok {
		// 已在 deploy：完成
		updated, err := s.q.UpdateDeliveryStatus(ctx, generated.UpdateDeliveryStatusParams{
			ID:     d.ID,
			Status: generated.DeliveryStatusCompleted,
		})
		if err != nil {
			return generated.Delivery{}, err
		}
		_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
			DeliveryID: d.ID, Stage: d.CurrentStage, EventType: "delivery_completed", Payload: []byte(`{}`),
		})
		return updated, nil
	}

	updated, err := s.q.UpdateDeliveryStage(ctx, generated.UpdateDeliveryStageParams{
		ID: d.ID, CurrentStage: next,
	})
	if err != nil {
		return generated.Delivery{}, err
	}
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: d.ID, Stage: next, EventType: "stage_started", Payload: []byte(`{}`),
	})
	return updated, nil
}
```

> 把新增 import 合并进文件顶部 import 块，不要重复声明 `import`。`generated.DeliveryStatusCompleted` 是 sqlc 从 enum 生成的常量；若名字不同，以 `pkg/db/generated/models.go` 为准。

- [ ] **Step 4: 跑测试确认通过**

```bash
go test ./internal/service/ -v
```
Expected: PASS（含 Task 9 的 Create 测试）

- [ ] **Step 5: 把 handler 的 Advance 桩换成真调用**

替换 `server/internal/handler/delivery.go` 里的 `Advance` 方法：
```go
func (h *DeliveryHandler) Advance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := h.svc.Advance(r.Context(), uuidOrNil(id))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}
```

- [ ] **Step 6: 手动验证整条推进链**

```bash
go run ./cmd/server &
sleep 1
ID=$(curl -s -X POST http://localhost:8080/api/deliveries -H 'Content-Type: application/json' -d '{"title":"t"}' | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
echo "id=$ID"
for i in 1 2 3 4 5 6 7 8; do
  curl -s -X POST http://localhost:8080/api/deliveries/$ID/advance > /dev/null
done
curl -s http://localhost:8080/api/deliveries/$ID | grep -o '"current_stage":"[^"]*"\|"status":"[^"]*"'
kill %1
```
Expected: stage 从 intake 一路推进，最终 `status` 变 `completed`

- [ ] **Step 7: 提交**

```bash
git add server/
git commit -m "feat(server): advance stage with timeline events (stub)"
```

---

## Task 12: Next.js 前端脚手架

**Files:**
- Create: `apps/web/`（由 create-next-app 生成）
- Modify: `apps/web/app/layout.tsx`
- Create: `apps/web/app/providers.tsx`

- [ ] **Step 1: 用 create-next-app 初始化**

```bash
cd /Users/huiyangz/tokfinity/infera
npx create-next-app@latest apps/web --typescript --app --tailwind --eslint --src-dir=false --import-alias "@/*" --no-turbopack
```
> 交互提示一律按回车接受默认（选 No 对 "customize default import alias" 之外的项目）。`--src-dir=false` 让 app/ 在根。

- [ ] **Step 2: 装依赖**

```bash
cd apps/web
npm install @tanstack/react-query
```

- [ ] **Step 3: 写 providers（TanStack Query）**

`apps/web/app/providers.tsx`:
```tsx
"use client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, ReactNode } from "react";

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient());
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}
```

- [ ] **Step 4: 在 layout 包一层 providers**

`apps/web/app/layout.tsx`（整体替换 `<body>` 内容部分，保留 metadata）:
```tsx
import type { Metadata } from "next";
import { Inter } from "next/font/google";
import "./globals.css";
import { Providers } from "./providers";

const inter = Inter({ subsets: ["latin"] });

export const metadata: Metadata = {
  title: "infera",
  description: "Agent 主导的代码交付流水线",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh">
      <body className={inter.className}>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
```

- [ ] **Step 5: 配 dev proxy 到后端**

`apps/web/next.config.mjs`（整体替换）:
```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  async rewrites() {
    return [
      { source: "/api/:path*", destination: "http://localhost:8080/api/:path*" },
    ];
  },
};
export default nextConfig;
```

- [ ] **Step 6: 验证 dev server 可起**

```bash
npm run dev &
sleep 4
curl -s http://localhost:3000/ | grep -o "<title>.*</title>"
kill %1
```
Expected: 输出含 `infera` 的 title

- [ ] **Step 7: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add apps/web/
git commit -m "feat(web): next.js scaffold with tanstack query"
```

---

## Task 13: API client + 类型

**Files:**
- Create: `apps/web/lib/types.ts`
- Create: `apps/web/lib/api.ts`

- [ ] **Step 1: 写类型**

`apps/web/lib/types.ts`:
```ts
export type DeliveryStatus = "active" | "completed" | "blocked";

export interface Delivery {
  id: string;
  title: string;
  description: string;
  repo_url: string;
  branch: string;
  status: DeliveryStatus;
  current_stage: string;
  created_at: string;
  updated_at: string;
}

export interface TimelineEvent {
  id: string;
  delivery_id: string;
  stage: string;
  event_type: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface DeliveryDetail {
  delivery: Delivery;
  timeline: TimelineEvent[];
}

export const STAGES = [
  "intake",
  "spec",
  "spec_approval",
  "test_gen",
  "code_gen",
  "unit_test",
  "code_review",
  "deploy",
] as const;

export const GATES = new Set(["spec_approval", "code_review"]);
```

- [ ] **Step 2: 写 API client**

`apps/web/lib/api.ts`:
```ts
import type { Delivery, DeliveryDetail } from "./types";

export async function listDeliveries(): Promise<Delivery[]> {
  const r = await fetch("/api/deliveries");
  if (!r.ok) throw new Error("list failed");
  return r.json();
}

export async function createDelivery(input: {
  title: string;
  description?: string;
  repo_url?: string;
}): Promise<Delivery> {
  const r = await fetch("/api/deliveries", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!r.ok) throw new Error("create failed");
  return r.json();
}

export async function getDelivery(id: string): Promise<DeliveryDetail> {
  const r = await fetch(`/api/deliveries/${id}`);
  if (!r.ok) throw new Error("get failed");
  return r.json();
}

export async function advanceDelivery(id: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/advance`, { method: "POST" });
  if (!r.ok) throw new Error("advance failed");
  return r.json();
}
```

- [ ] **Step 3: 提交**

```bash
git add apps/web/lib/
git commit -m "feat(web): api client and types"
```

---

## Task 14: 列表页 + 创建表单

**Files:**
- Modify: `apps/web/app/page.tsx`

- [ ] **Step 1: 写列表 + 创建页**

`apps/web/app/page.tsx`（整体替换）:
```tsx
"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { listDeliveries, createDelivery, advanceDelivery } from "@/lib/api";

export default function Home() {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["deliveries"],
    queryFn: listDeliveries,
  });

  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => createDelivery({ title }),
    onSuccess: () => {
      setTitle("");
      qc.invalidateQueries({ queryKey: ["deliveries"] });
    },
  });

  const advance = useMutation({
    mutationFn: (id: string) => advanceDelivery(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["deliveries"] }),
  });

  return (
    <main className="max-w-3xl mx-auto p-8">
      <h1 className="text-2xl font-bold mb-6">infera · Deliveries</h1>

      <form
        className="flex gap-2 mb-8"
        onSubmit={(e) => {
          e.preventDefault();
          if (title.trim()) create.mutate();
        }}
      >
        <input
          className="flex-1 border rounded px-3 py-2"
          placeholder="一句话需求…"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
        />
        <button className="bg-black text-white rounded px-4 py-2" type="submit">
          创建
        </button>
      </form>

      {isLoading ? (
        <p>加载中…</p>
      ) : (
        <ul className="space-y-2">
          {data?.map((d) => (
            <li key={d.id} className="border rounded p-4 flex items-center justify-between">
              <div>
                <Link href={`/deliveries/${d.id}`} className="font-medium hover:underline">
                  {d.title}
                </Link>
                <div className="text-sm text-gray-500">
                  {d.current_stage} · {d.status}
                </div>
              </div>
              <button
                className="text-sm border rounded px-3 py-1"
                onClick={() => advance.mutate(d.id)}
              >
                推进 →
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
```

- [ ] **Step 2: 验证（后端 + 前端都起着）**

```bash
cd server && go run ./cmd/server &
cd ../apps/web && npm run dev &
sleep 4
# 浏览器打开 http://localhost:3000，输入需求点创建，应出现新行
```
Expected: UI 列表为空时显示，创建后出现新 Delivery；点推进 stage 变化

- [ ] **Step 3: 提交**

```bash
git add apps/web/app/page.tsx
git commit -m "feat(web): delivery list and create form"
```

---

## Task 15: 详情页（stage 序列 + timeline）

**Files:**
- Create: `apps/web/app/deliveries/[id]/page.tsx`

- [ ] **Step 1: 写详情页**

`apps/web/app/deliveries/[id]/page.tsx`:
```tsx
"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { getDelivery, advanceDelivery } from "@/lib/api";
import { STAGES, GATES } from "@/lib/types";

export default function DeliveryDetailPage() {
  const params = useParams<{ id: string }>();
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["delivery", params.id],
    queryFn: () => getDelivery(params.id),
  });

  const advance = useMutation({
    mutationFn: () => advanceDelivery(params.id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["delivery", params.id] }),
  });

  if (isLoading || !data) return <main className="p-8">加载中…</main>;
  const { delivery, timeline } = data;
  const currentIdx = STAGES.indexOf(delivery.current_stage as typeof STAGES[number]);

  return (
    <main className="max-w-3xl mx-auto p-8">
      <Link href="/" className="text-sm text-gray-500">← 返回</Link>
      <h1 className="text-2xl font-bold mt-2 mb-1">{delivery.title}</h1>
      <div className="text-sm text-gray-500 mb-6">
        {delivery.status} · 创建于 {new Date(delivery.created_at).toLocaleString()}
      </div>

      <h2 className="font-semibold mb-2">流水线</h2>
      <ol className="flex flex-wrap gap-2 mb-8">
        {STAGES.map((s, i) => {
          const done = i < currentIdx;
          const isCurrent = i === currentIdx;
          const isGate = GATES.has(s);
          return (
            <li
              key={s}
              className={[
                "px-3 py-1 rounded-full text-sm border",
                isCurrent ? "bg-black text-white" : done ? "bg-gray-100" : "text-gray-400",
                isGate ? "border-dashed" : "",
              ].join(" ")}
            >
              {s}
              {isGate ? " 🚪" : ""}
            </li>
          );
        })}
      </ol>

      {delivery.status === "active" && (
        <button
          className="bg-black text-white rounded px-4 py-2 mb-8"
          onClick={() => advance.mutate()}
        >
          推进到下一 stage →
        </button>
      )}

      <h2 className="font-semibold mb-2">时间线</h2>
      <ul className="space-y-1 text-sm">
        {timeline.map((e) => (
          <li key={e.id} className="border-l-2 pl-3 py-1">
            <span className="font-mono text-gray-500">
              {new Date(e.created_at).toLocaleTimeString()}
            </span>{" "}
            <span className="font-medium">{e.stage}</span> · {e.event_type}
          </li>
        ))}
      </ul>
    </main>
  );
}
```

- [ ] **Step 2: 端到端验证**

```bash
# 后端 + 前端起着，浏览器：
# 1. http://localhost:3000 创建一条 Delivery
# 2. 点进详情，看到 8 个 stage（gate 带 🚪），当前 intake 高亮
# 3. 多次点"推进"，stage 高亮后移，时间线不断追加事件
# 4. 推过 deploy 后 status 变 completed，推进按钮消失
```
Expected: 流水线可视化 + 时间线随推进增长，与后端状态一致

- [ ] **Step 3: 提交**

```bash
git add apps/web/app/deliveries/
git commit -m "feat(web): delivery detail page with stage pipeline and timeline"
```

---

## P1 完成标准（Definition of Done）

- [ ] `docker compose up -d` 起本地 postgres；`migrate up` 建全部表
- [ ] `go run ./server/cmd/server` 起后端，`/health`、`/api/deliveries` CRUD、`/api/deliveries/:id/advance` 均工作
- [ ] `npm run dev`（apps/web）起前端，能创建、列表、详情、推进 Delivery
- [ ] stage 从 intake 一路推进到 deploy，最终 status = completed；时间线全程留痕
- [ ] 所有 `go test ./...` 通过
- [ ] 每个任务一个 commit，共 ~15 个 commit

## 给后续 Plan 的接口约定（P2+ 会用到）

- **Agent 执行**（P2）：在 stage 推进时，把当前 stage 交给对应专职 Agent（intake/spec→Spec，test_gen→Test，code_gen/unit_test→Coder，code_review→Reviewer）。P1 的 advance 是 stub，P2 把 stub 换成真 Agent 调用。
- **loop**（P3）：unit_test ✗ / code_review ✗ 时，stage 应回退到 code_gen 而非前进。P1 只前进，P3 加回环。
- **卡住升级**（P3）：连续 3 次不过 → status = blocked + 通知人。
- **GitHub**（P4）：repo_url/branch 字段已预留；P4 接 GitHub App 做仓库同步与 PR。
