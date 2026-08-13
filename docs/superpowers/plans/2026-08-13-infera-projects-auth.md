# infera P7（项目 + 登录 + onboarding）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 infera 补上「登录 → 建项目（绑仓库）→ 项目下提需求」的真实流程：Project 实体（1 项目=1 仓库）、单用户密码登录、Delivery 挂项目；顺带砍掉 deploy 阶段、做成自动推进到 gate、修掉 workdir gap（拉的真代码喂给 Coder/testRunner → 真 PR）。

**Architecture:** 新 `projects` 表 + `deliveries.project_id`（删 `repo_url/branch`，仓库在项目层绑定一次）。`internal/auth` 单用户密码门 + HMAC 签名 cookie + 中间件。`ProjectService`（建项目时试 clone 校验）。`DeliveryService.Create` 改收 `project_id`；新增 `RunUntilGate` 异步自动推进到 gate。`ExecuteService` 从项目取仓库、把 clone 目录挂进 Coder/testRunner 容器。前端：登录页 + 项目列表/详情 + 深浅双模主题。

**Tech Stack:** Go（chi/sqlc/pgx，沿用）· crypto/hmac+subtle（auth，零新依赖）· Next.js 16 / React 19 / Tailwind v4（沿用）

**Spec:** `docs/superpowers/specs/2026-08-13-infera-projects-auth-design.md`
**依赖：** P1-P6 已完成

---

## 环境约定（所有任务通用）

- Go 命令在 `server/` 下：`cd /Users/huiyangz/tokfinity/infera/server`，模块代理 `GOPROXY=https://goproxy.cn,direct`。
- migrate CLI：`/Users/huiyangz/go/bin/migrate`（端口 **5433**）；sqlc：`/Users/huiyangz/go/bin/sqlc`。
- npm 在 `apps/web/` 下：`export npm_config_registry=https://registry.npmmirror.com`。
- 所有 `pgtype.UUID` 为 DB/service 边界类型（P1 既定）。提交信息末尾加：
  ```
  Co-Authored-By: Claude <noreply@anthropic.com>
  ```

---

## Task 1: 迁移 — projects 表 + deliveries.project_id（删 repo_url/branch）

**Files:**
- Create: `server/migrations/000007_projects.up.sql`
- Create: `server/migrations/000007_projects.down.sql`

- [ ] **Step 1: 写 up 迁移**

`server/migrations/000007_projects.up.sql`:
```sql
CREATE TABLE projects (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text NOT NULL,
    repo_url       text NOT NULL DEFAULT '',
    default_branch text NOT NULL DEFAULT 'main',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- 回填现有 deliveries 到一个 Default 项目
INSERT INTO projects (name, repo_url) VALUES ('Default', '');

ALTER TABLE deliveries ADD COLUMN project_id uuid REFERENCES projects(id) ON DELETE CASCADE;
UPDATE deliveries SET project_id = (SELECT id FROM projects WHERE name = 'Default' LIMIT 1);
ALTER TABLE deliveries ALTER COLUMN project_id SET NOT NULL;

ALTER TABLE deliveries DROP COLUMN repo_url;
ALTER TABLE deliveries DROP COLUMN branch;
```

- [ ] **Step 2: 写 down 迁移**

`server/migrations/000007_projects.down.sql`:
```sql
ALTER TABLE deliveries ADD COLUMN repo_url text NOT NULL DEFAULT '';
ALTER TABLE deliveries ADD COLUMN branch text NOT NULL DEFAULT '';
ALTER TABLE deliveries DROP COLUMN project_id;
DROP TABLE projects;
```

- [ ] **Step 3: 跑迁移 + 验证**

```bash
cd /Users/huiyangz/tokfinity/infera/server
/Users/huiyangz/go/bin/migrate -path migrations -database "postgres://infera:infera@localhost:5433/infera?sslmode=disable" up
docker compose -f /Users/huiyangz/tokfinity/infera/docker-compose.yml exec -T postgres psql -U infera -c "\d deliveries" | grep -E "project_id|repo_url|branch"
docker compose -f /Users/huiyangz/tokfinity/infera/docker-compose.yml exec -T postgres psql -U infera -c "\d projects"
```
Expected：`deliveries` 有 `project_id`、无 `repo_url`/`branch`；`projects` 表结构打印出来。对 `infera_test` 库无需手动跑（dbtest.Migrate 会跑全部）。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/migrations/
git commit -m "feat(db): projects table + deliveries.project_id (drop repo_url/branch)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

> 此时 `go build ./...` 会**失败**（sqlc 还没重新生成、旧代码引用 `RepoUrl`/`Branch`）。Task 2/4 会修。先不 build。

---

## Task 2: sqlc 查询 — projects CRUD + deliveries 改造 + 重新生成

**Files:**
- Create: `server/pkg/db/queries/projects.sql`
- Modify: `server/pkg/db/queries/deliveries.sql`

- [ ] **Step 1: 写 projects 查询**

`server/pkg/db/queries/projects.sql`:
```sql
-- name: CreateProject :one
INSERT INTO projects (name, repo_url, default_branch)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $2, repo_url = $3, default_branch = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;

-- name: GetProjectByDeliveryID :one
SELECT p.* FROM projects p
JOIN deliveries d ON d.project_id = p.id
WHERE d.id = $1;
```

- [ ] **Step 2: 改 deliveries.sql — CreateDelivery 改收 project_id；加按项目列表**

在 `server/pkg/db/queries/deliveries.sql` 里把 `CreateDelivery` 整体替换为：
```sql
-- name: CreateDelivery :one
INSERT INTO deliveries (project_id, title, description)
VALUES ($1, $2, $3)
RETURNING *;
```
并在文件末尾追加：
```sql
-- name: ListDeliveriesByProject :many
SELECT * FROM deliveries WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;
```
（`GetDelivery`/`ListDeliveries`/`UpdateDeliveryStage`/`UpdateDeliveryStatus`/`IncrementDeliveryFailCount`/`ResetDeliveryFailCount`/`Set/ClearDeliveryPendingGate` 不动。）

- [ ] **Step 3: 重新生成 + 编译（预期仍失败，下一步修代码）**

```bash
cd /Users/huiyangz/tokfinity/infera/server
/Users/huiyangz/go/bin/sqlc generate
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | head -20
```
Expected：sqlc 生成成功；`go build` 报错指向 `RepoUrl`/`Branch`/`CreateDeliveryParams` 旧用法（Task 3-5 修）。

- [ ] **Step 4: 记录生成的类型（供后续 task 用，读完即可）**

```bash
grep -A8 'type Project struct' pkg/db/generated/models.go
grep -A6 'type CreateProjectParams' pkg/db/generated/projects.sql.go
grep -A6 'type CreateDeliveryParams' pkg/db/generated/deliveries.sql.go
```
确认：`Project{Name,RepoUrl,DefaultBranch,...}`；`CreateDeliveryParams{ProjectID pgtype.UUID, Title, Description string}`；`Delivery` 无 `RepoUrl/Branch`、有 `ProjectID`。

- [ ] **Step 5: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/pkg/db/
git commit -m "feat(db): sqlc queries for projects + delivery project scoping

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: stage 状态机 — 砍 deploy

**Files:**
- Modify: `server/internal/stage/stage.go`
- Modify: `server/internal/stage/stage_test.go`

- [ ] **Step 1: 改测试**

把 `server/internal/stage/stage_test.go` 里 `TestNextOfEachStage` 的 cases 改为（去掉 code_review→deploy）：
```go
func TestNextOfEachStage(t *testing.T) {
	cases := []struct{ from, want string }{
		{"intake", "spec"},
		{"spec", "spec_approval"},
		{"spec_approval", "test_gen"},
		{"test_gen", "code_gen"},
		{"code_gen", "unit_test"},
		{"unit_test", "code_review"},
	}
	for _, c := range cases {
		got, ok := Next(c.from)
		assert.True(t, ok, "stage %q should have a next", c.from)
		assert.Equal(t, c.want, got)
	}
}

func TestNextOfCodeReviewIsEmpty(t *testing.T) {
	_, ok := Next("code_review")
	assert.False(t, ok, "code_review is terminal")
}

func TestIsGate(t *testing.T) {
	assert.True(t, IsGate("spec_approval"))
	assert.True(t, IsGate("code_review"))
	assert.False(t, IsGate("code_gen"))
}
```

- [ ] **Step 2: 改实现**

`server/internal/stage/stage.go` 的 `order` 改为：
```go
var order = []string{
	"intake",        // 需求
	"spec",          // Spec Agent 写 spec
	"spec_approval", // 人审批 spec（gate）
	"test_gen",      // Test Agent 生成用例 + 单测
	"code_gen",      // Coder Agent 写实现（修复 hub）
	"unit_test",     // 系统跑单测
	"code_review",   // Reviewer Agent 预审 + 人批准（gate，终点）
}
```
（`gates` 不变：`spec_approval`、`code_review`。）

- [ ] **Step 3: 跑测试**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go test ./internal/stage/ -v
```
Expected：PASS。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/stage/
git commit -m "feat(stage): drop deploy; code_review is terminal

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: DeliveryService.Create 改收 project_id + dbtest helper + 修所有既有测试

**Files:**
- Modify: `server/internal/dbtest/dbtest.go`
- Modify: `server/internal/service/delivery.go`（仅 Create + CreateInput）
- Modify: `server/internal/service/*_test.go`（全部 Create 调用点）

- [ ] **Step 1: dbtest 加 project seed helper**

在 `server/internal/dbtest/dbtest.go` 末尾追加：
```go
// SeedProject 建一个测试项目并返回其 id（供需要 project_id 的测试用）。
func SeedProject(t *testing.T, pool *pgxpool.Pool, name string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	err := pool.QueryRow(context.Background(),
		`INSERT INTO projects (name, repo_url) VALUES ($1, '') RETURNING id`, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return id
}
```
（顶部 import 已有 `context`、`pgxpool`、`pgtype`？`pgtype` 需加 `"github.com/jackc/pgx/v5/pgtype"`；`pgxpool` 已在。补 import。）

- [ ] **Step 2: 改 Create 签名**

`server/internal/service/delivery.go`：把 `CreateInput` 和 `Create` 改为：
```go
type CreateInput struct {
	Title       string
	Description string
}

// Create 在指定项目下建一条新 Delivery，初始 stage = intake。
func (s *DeliveryService) Create(ctx context.Context, projectID pgtype.UUID, in CreateInput) (generated.Delivery, error) {
	if in.Title == "" {
		return generated.Delivery{}, fmt.Errorf("title is required")
	}
	d, err := s.q.CreateDelivery(ctx, generated.CreateDeliveryParams{
		ProjectID:   projectID,
		Title:       in.Title,
		Description: in.Description,
	})
	if err != nil {
		return generated.Delivery{}, fmt.Errorf("create delivery: %w", err)
	}
	return d, nil
}
```
（删掉 `CreateInput` 里的 `RepoURL`、`Branch` 字段。）

- [ ] **Step 3: 修所有测试里的 Create 调用**

对 `server/internal/service/` 下每个 `_test.go`：
- 每个测试函数里，在 `svc.Create(...)` 之前加 `pid := dbtest.SeedProject(t, pool, "p1")`（如已有 seed，复用），并把 `dbtest.Truncate(...)` 的表列表加上 `"projects"`。
- 把 `svc.Create(ctx, CreateInput{Title: "..."})` 改为 `svc.Create(ctx, pid, CreateInput{Title: "..."})`。
- `dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs")` → `dbtest.Truncate(t, pool, "timeline_events", "deliveries", "agent_configs", "projects")`（顺序：先 timeline/deliveries，再 projects，最后 agent_configs；外键约束：deliveries 依赖 projects，所以 projects 最后truncate 或一起 CASCADE）。统一用：`dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")`。

涉及文件：`delivery_test.go`、`delivery_advance_test.go`、`delivery_agent_test.go`、`delivery_loop_test.go`、`delivery_gate_test.go`、`execute_test.go`。

- [ ] **Step 4: 跑全部 service 测试**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go test ./internal/service/ -v 2>&1 | tail -30
```
Expected：全部 PASS。如有遗漏的 `Create` 调用点（编译错），按同样方式修。

- [ ] **Step 5: 全量编译 + 测试**

```bash
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -5
GOPROXY=https://goproxy.cn,direct go test ./... 2>&1 | tail -12
```
Expected：`handler` 包可能仍报错（handler 里 `Create` 的 service 调用 / `createDeliveryReq` 带 repo_url）——Task 10 修 handler。若 handler 报错，本步只确认 `service`/`stage`/`dbtest` 等已绿。

- [ ] **Step 6: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/dbtest/ server/internal/service/
git commit -m "feat(server): delivery belongs to project (Create takes project_id)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 5: auth 包 — 密码门 + 签名 cookie + 中间件

**Files:**
- Create: `server/internal/auth/auth.go`
- Create: `server/internal/auth/auth_test.go`

- [ ] **Step 1: 先写测试**

`server/internal/auth/auth_test.go`:
```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVerifyPassword(t *testing.T) {
	m := New("s3cret")
	assert.True(t, m.Verify("s3cret"))
	assert.False(t, m.Verify("wrong"))
	assert.False(t, m.Verify(""))
}

func TestLoginCookieRoundTrip(t *testing.T) {
	m := New("s3cret")
	rec := httptest.NewRecorder()
	m.SetLogin(rec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	assert.True(t, m.IsLoggedIn(req))
}

func TestWrongCookieNotLoggedIn(t *testing.T) {
	m := New("s3cret")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "infera_session", Value: "garbage"})
	assert.False(t, m.IsLoggedIn(req))
}

func TestMiddlewareBlocksAndAllows(t *testing.T) {
	m := New("s3cret")
	called := false
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// 未登录：401，不调用下游
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, called)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 登录后：放行
	rec2 := httptest.NewRecorder()
	loginRec := httptest.NewRecorder()
	m.SetLogin(loginRec)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	h.ServeHTTP(rec2, req)
	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec2.Code)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go test ./internal/auth/ -v
```
Expected：FAIL（`New`/`Manager` 未定义）。

- [ ] **Step 3: 实现**

`server/internal/auth/auth.go`:
```go
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
)

const cookieName = "infera_session"

// Manager 单用户密码门：密码常量时间校验 + HMAC 签名 cookie。
type Manager struct {
	password string
}

// New 构造 Manager。password 为空时 Verify 永远返回 false（无法登录）。
func New(password string) *Manager { return &Manager{password: password} }

// Verify 常量时间比较密码；空密码配置时拒绝一切登录。
func (m *Manager) Verify(pw string) bool {
	if m.password == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pw), []byte(m.password)) == 1
}

func (m *Manager) signedToken() string {
	mac := hmac.New(sha256.New, []byte(m.password))
	mac.Write([]byte("infera-session-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// SetLogin 种 httpOnly 签名 cookie。
func (m *Manager) SetLogin(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: m.signedToken(), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// Clear 清 cookie（登出）。
func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
}

// IsLoggedIn 校验请求里的签名 cookie。
func (m *Manager) IsLoggedIn(r *http.Request) bool {
	if m.password == "" {
		return false
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(m.signedToken())) == 1
}

// Middleware 挡在需要登录的路由前；未登录返回 401。
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.IsLoggedIn(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
GOPROXY=https://goproxy.cn,direct go test ./internal/auth/ -v
```
Expected：4 个测试全 PASS。

- [ ] **Step 5: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/auth/
git commit -m "feat(auth): single-user password gate + signed cookie + middleware

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 6: config 加密码 + auth handlers + 路由保护

**Files:**
- Modify: `server/internal/config/config.go`
- Create: `server/internal/handler/auth.go`
- Modify: `server/internal/handler/router.go`
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: config 加 `InferaPassword`**

`server/internal/config/config.go` 的 `Config` 加字段、`Load` 加读取：
```go
type Config struct {
	DatabaseURL    string
	Port           string
	GitHubToken    string
	AgentImage     string
	RepoWorkRoot   string
	InferaPassword string // 单用户登录密码
}
```
`Load` 内追加：
```go
		InferaPassword: getenv("INFERA_PASSWORD", ""),
```

- [ ] **Step 2: 写 auth handlers**

`server/internal/handler/auth.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/tokfinity/infera/internal/auth"
)

type AuthHandler struct {
	m *auth.Manager
}

func NewAuthHandler(m *auth.Manager) *AuthHandler { return &AuthHandler{m: m} }

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !h.m.Verify(body.Password) {
		writeErr(w, http.StatusUnauthorized, "invalid password")
		return
	}
	h.m.SetLogin(w)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": true})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.m.Clear(w)
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": false})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"logged_in": h.m.IsLoggedIn(r)})
}
```

- [ ] **Step 3: 改 router — 装配 auth + 保护路由**

`server/internal/handler/router.go` 整体替换为（注意：project/delivery 路由会用 Task 8/10 的 handler，此处先保留现有 delivery 路由并加 auth 保护；projects 路由 Task 8 加）：
```go
package handler

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/auth"
	"github.com/tokfinity/infera/internal/realtime"
	"github.com/tokfinity/infera/internal/service"
)

func NewRouter(pool *pgxpool.Pool, svc *service.DeliveryService, hub *realtime.Hub, authMgr *auth.Manager) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", Health)
	r.Get("/ws", WS(hub))

	authH := NewAuthHandler(authMgr)
	r.Post("/api/login", authH.Login)
	r.Post("/api/logout", authH.Logout)
	r.Get("/api/me", authH.Me)

	// 受保护路由
	r.Group(func(r chi.Router) {
		r.Use(authMgr.Middleware)
		dh := NewDeliveryHandler(svc, pool)
		r.Route("/api/deliveries", func(r chi.Router) {
			r.Get("/", dh.List)
			r.Get("/{id}", dh.Get)
			r.Post("/{id}/advance", dh.Advance)
			r.Get("/{id}/gate", dh.Gate)
			r.Post("/{id}/approve", dh.Approve)
			r.Post("/{id}/reject", dh.Reject)
		})
		// projects 路由在 Task 8 加；delivery 创建改在 Task 10
	})
	return r
}
```
> 注：`dh.Create`（POST /）先从路由里去掉（Task 10 改成 `POST /api/projects/{id}/deliveries`）。`r.Post("/", dh.Create)` 删除。

- [ ] **Step 4: 改 main — 构造 auth.Manager 并传入**

`server/cmd/server/main.go`：在加载 cfg 后加：
```go
	if cfg.InferaPassword == "" {
		log.Fatal("INFERA_PASSWORD not set; required for login")
	}
	authMgr := auth.New(cfg.InferaPassword)
```
把 `handler.NewRouter(pool, deliverySvc, hub)` 改为 `handler.NewRouter(pool, deliverySvc, hub, authMgr)`。import 加 `"github.com/tokfinity/infera/internal/auth"`。

- [ ] **Step 5: 编译 + 手动验证**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -5
INFERA_PASSWORD=test123 go run ./cmd/server &
sleep 2
echo "--- 未登录访问受保护 ---"
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/deliveries   # 401
echo "--- 登录 ---"
curl -s -c /tmp/c.txt -X POST http://localhost:8080/api/login -H 'Content-Type: application/json' -d '{"password":"test123"}'
echo
echo "--- 带 cookie 访问 ---"
curl -s -b /tmp/c.txt -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/deliveries   # 200
echo "--- 错密码 ---"
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://localhost:8080/api/login -H 'Content-Type: application/json' -d '{"password":"nope"}'  # 401
kill $(lsof -tiTCP:8080 -sTCP:LISTEN)
```
Expected：401 / `{"logged_in":true}` / 200 / 401。

- [ ] **Step 6: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/config/ server/internal/handler/ server/cmd/server/
git commit -m "feat(server): login/logout/me endpoints + auth middleware

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 7: ProjectService（建项目试 clone 校验）+ 测试

**Files:**
- Create: `server/internal/service/project.go`
- Create: `server/internal/service/project_test.go`

- [ ] **Step 1: 先写测试（用假 cloner）**

`server/internal/service/project_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/dbtest"
)

type fakeCloner struct{ fail bool }

func (f fakeCloner) Clone(ctx context.Context, repoURL, dest string) error {
	if f.fail {
		return errFakeClone
	}
	return nil
}

var errFakeClone = assert.AnError

func TestCreateProjectOK(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{})
	p, err := svc.Create(context.Background(), CreateProjectInput{Name: "web", RepoURL: "https://github.com/x/y.git", DefaultBranch: "main"})
	assert.NoError(t, err)
	assert.Equal(t, "web", p.Name)
	assert.Equal(t, "main", p.DefaultBranch)
}

func TestCreateProjectRejectsBadRepo(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{fail: true})
	_, err := svc.Create(context.Background(), CreateProjectInput{Name: "web", RepoURL: "https://github.com/x/y.git"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "repo")
}

func TestListAndGetProject(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "deliveries", "projects")

	svc := NewProject(pool, fakeCloner{})
	p, _ := svc.Create(context.Background(), CreateProjectInput{Name: "a"})

	got, err := svc.Get(context.Background(), p.ID)
	assert.NoError(t, err)
	assert.Equal(t, "a", got.Name)

	list, err := svc.List(context.Background())
	assert.NoError(t, err)
	assert.True(t, len(list) >= 1)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go test ./internal/service/ -run TestCreateProject -v
```
Expected：FAIL（`NewProject` 等未定义）。

- [ ] **Step 3: 实现**

`server/internal/service/project.go`:
```go
package service

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/pkg/db/generated"
)

// RepoCloner 抽象"克隆仓库到本地"（生产用 github.RepoCloner，测试用 fake）。
type RepoCloner interface {
	Clone(ctx context.Context, repoURL, dest string) error
}

type ProjectService struct {
	q      *generated.Queries
	cloner RepoCloner
}

func NewProject(pool *pgxpool.Pool, cloner RepoCloner) *ProjectService {
	return &ProjectService{q: generated.New(pool), cloner: cloner}
}

type CreateProjectInput struct {
	Name          string
	RepoURL       string
	DefaultBranch string
}

// Create 建项目；repo_url 非空时先试 clone 校验可达 + 有权限。
func (s *ProjectService) Create(ctx context.Context, in CreateProjectInput) (generated.Project, error) {
	if in.Name == "" {
		return generated.Project{}, fmt.Errorf("name is required")
	}
	branch := in.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	if in.RepoURL != "" {
		tmp, err := os.MkdirTemp("", "infera-clone-*")
		if err != nil {
			return generated.Project{}, fmt.Errorf("mktemp: %w", err)
		}
		defer os.RemoveAll(tmp)
		if err := s.cloner.Clone(ctx, in.RepoURL, tmp); err != nil {
			return generated.Project{}, fmt.Errorf("repo not accessible: %w", err)
		}
	}
	return s.q.CreateProject(ctx, generated.CreateProjectParams{
		Name: in.Name, RepoUrl: in.RepoURL, DefaultBranch: branch,
	})
}

func (s *ProjectService) List(ctx context.Context) ([]generated.Project, error) {
	return s.q.ListProjects(ctx)
}

func (s *ProjectService) Get(ctx context.Context, id pgtype.UUID) (generated.Project, error) {
	return s.q.GetProject(ctx, id)
}

func (s *ProjectService) Update(ctx context.Context, id pgtype.UUID, in CreateProjectInput) (generated.Project, error) {
	branch := in.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	return s.q.UpdateProject(ctx, generated.UpdateProjectParams{
		ID: id, Name: in.Name, RepoUrl: in.RepoURL, DefaultBranch: branch,
	})
}

func (s *ProjectService) Delete(ctx context.Context, id pgtype.UUID) error {
	return s.q.DeleteProject(ctx, id)
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
GOPROXY=https://goproxy.cn,direct go test ./internal/service/ -run "TestCreateProject|TestListAndGetProject" -v
```
Expected：PASS。

- [ ] **Step 5: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/service/project.go server/internal/service/project_test.go
git commit -m "feat(server): project service with clone-validation on create

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 8: Project handlers + 路由

**Files:**
- Create: `server/internal/handler/project.go`
- Modify: `server/internal/handler/router.go`

- [ ] **Step 1: 写 project handler**

`server/internal/handler/project.go`:
```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tokfinity/infera/internal/service"
)

type ProjectHandler struct {
	svc  *service.ProjectService
	pool *pgxpool.Pool
}

func NewProjectHandler(svc *service.ProjectService, pool *pgxpool.Pool) *ProjectHandler {
	return &ProjectHandler{svc: svc, pool: pool}
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := h.svc.Create(r.Context(), service.CreateProjectInput{
		Name: req.Name, RepoURL: req.RepoURL, DefaultBranch: req.DefaultBranch,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := h.svc.Get(r.Context(), parseUUID(id))
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Name          string `json:"name"`
		RepoURL       string `json:"repo_url"`
		DefaultBranch string `json:"default_branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	p, err := h.svc.Update(r.Context(), parseUUID(id), service.CreateProjectInput{
		Name: req.Name, RepoURL: req.RepoURL, DefaultBranch: req.DefaultBranch,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.svc.Delete(r.Context(), parseUUID(id)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
```

- [ ] **Step 2: router 加 project 路由 + cloner 装配**

`server/internal/handler/router.go` 的 `NewRouter` 签名加 `projectSvc *service.ProjectService`，并在受保护 group 内加：
```go
		ph := NewProjectHandler(projectSvc, pool)
		r.Route("/api/projects", func(r chi.Router) {
			r.Post("/", ph.Create)
			r.Get("/", ph.List)
			r.Get("/{id}", ph.Get)
			r.Patch("/{id}", ph.Update)
			r.Delete("/{id}", ph.Delete)
		})
```
`main.go` 在 executor 装配附近加（用真实 cloner）：
```go
	cloner := github.NewRepoCloner(cfg.GitHubToken)
	projectSvc := service.NewProject(pool, cloner)
```
`handler.NewRouter(pool, deliverySvc, hub, authMgr, projectSvc)`。import 加 `"github.com/tokfinity/infera/internal/github"`（若未在 main）。

- [ ] **Step 3: 编译 + 手动验证**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -5
INFERA_PASSWORD=test123 go run ./cmd/server &
sleep 2
curl -s -c /tmp/c.txt -X POST http://localhost:8080/api/login -H 'Content-Type: application/json' -d '{"password":"test123"}' >/dev/null
echo "--- 建项目（空 repo_url，跳过 clone 校验）---"
curl -s -b /tmp/c.txt -X POST http://localhost:8080/api/projects -H 'Content-Type: application/json' -d '{"name":"web"}'
echo
echo "--- 列项目 ---"
curl -s -b /tmp/c.txt http://localhost:8080/api/projects
echo
kill $(lsof -tiTCP:8080 -sTCP:LISTEN)
```
Expected：建项目返回 201 + project JSON（含 id）；列项目返回数组含它。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/handler/project.go server/internal/handler/router.go server/cmd/server/main.go
git commit -m "feat(server): project CRUD endpoints

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 9: ExecuteService — 仓库从项目取 + 修 workdir gap

**Files:**
- Modify: `server/internal/service/execute.go`

- [ ] **Step 1: 加 WorkdirFor + ensureClone + repo 从项目取**

在 `server/internal/service/execute.go`：
- import 加 `"os"`（已有）、`"path/filepath"`（已有）。
- `ExecuteService` 已有 `repoRoot`。
- 加方法：
```go
// WorkdirFor 返回某 delivery 的本地 clone 目录路径。
func (s *ExecuteService) WorkdirFor(deliveryID pgtype.UUID) string {
	return filepath.Join(s.repoRoot, pgUUIDString(deliveryID))
}

// ensureClone 确保该 delivery 的项目仓库已 clone 到 workdir（首次才 clone）。
func (s *ExecuteService) ensureClone(ctx context.Context, deliveryID pgtype.UUID) error {
	if s.pr == nil {
		return nil // 无仓库模式
	}
	wd := s.WorkdirFor(deliveryID)
	if _, err := os.Stat(wd); err == nil {
		return nil // 已存在
	}
	proj, err := s.q.GetProjectByDeliveryID(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if proj.RepoUrl == "" {
		return nil // 绿地空仓库，不 clone
	}
	_ = os.MkdirAll(s.repoRoot, 0o755)
	return s.cloner.Clone(ctx, proj.RepoUrl, wd)
}
```
- 修改 `ExecuteStage`：在 `code_gen` 的 agent 执行**前** ensureClone，并把 workdir 传进 backend：
```go
	// code_gen 前：确保仓库已 clone（workdir 给 Coder 和后续 PR）
	workdir := ""
	if stage == "code_gen" {
		_ = s.ensureClone(ctx, deliveryID)
		workdir = s.WorkdirFor(deliveryID)
	}
	res, err := s.backend.Execute(ctx, agent.ExecInput{
		Role:    role,
		Prompt:  fmt.Sprintf("%s\n\n# 本次任务\n%s", cfg.SystemPrompt, prompt),
		Workdir: workdir,
	})
```
- 修改 `maybePushAndOpenPR`：仓库从项目取、workdir 用 WorkdirFor、分支用项目默认分支。把现有 `maybePushAndOpenPR` 整体替换为：
```go
func (s *ExecuteService) maybePushAndOpenPR(ctx context.Context, deliveryID pgtype.UUID, title string) error {
	if s.pr == nil {
		return nil
	}
	proj, err := s.q.GetProjectByDeliveryID(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if proj.RepoUrl == "" {
		return nil // 无仓库
	}
	workdir := s.WorkdirFor(deliveryID)
	branch := "infera/" + pgUUIDString(deliveryID)
	if err := s.git.WithWorkdir(workdir).CommitAndPush(ctx, branch, "infera: "+title); err != nil {
		return err
	}
	pr, err := s.pr.Create(ctx, repoOwnerRepo(proj.RepoUrl), branch, proj.DefaultBranch,
		"["+title+"] by Coder Agent", "由 infera Coder Agent 自动生成")
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"url": pr.GetHTMLURL(), "number": pr.GetNumber()})
	_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
		DeliveryID: deliveryID, Stage: "code_gen", EventType: "pr_opened", Payload: payload,
	})
	s.broadcast(deliveryID, "code_gen", "pr_opened")
	return nil
}
```
- 修改 `ExecuteStage` 里 code_gen 分支对 `maybePushAndOpenPR` 的调用（签名变了，不再传 `d`、`branch`）：
```go
	if stage == "code_gen" {
		if d, err := s.q.GetDelivery(ctx, deliveryID); err == nil {
			if err := s.maybePushAndOpenPR(ctx, deliveryID, d.Title); err != nil {
				ep, _ := json.Marshal(map[string]any{"error": err.Error()})
				_, _ = s.q.CreateTimelineEvent(ctx, generated.CreateTimelineEventParams{
					DeliveryID: deliveryID, Stage: stage, EventType: "pr_failed", Payload: ep,
				})
				s.broadcast(deliveryID, stage, "pr_failed")
			}
		}
	}
```
- 删除旧的 `IsLatestPRMerged`（deploy 已砍，Task 10 配合）—— 本步先保留，Task 10 删。

- [ ] **Step 2: 编译**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -10
```
Expected：无错误（IsLatestPRMerged 暂保留但不再被 delivery.go 调用——Task 10 删调用并删方法）。

- [ ] **Step 3: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/service/execute.go
git commit -m "fix(executor): mount cloned repo into coder container; repo from project

Workdir gap fix: clone project repo to <repoRoot>/<deliveryID>, pass it
as ExecInput.Workdir so Coder edits real files. PR uses project repo+branch.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 10: DeliveryService — 砍 deploy-wait + testRunner 用 workdir + 删 IsLatestPRMerged

**Files:**
- Modify: `server/internal/service/delivery.go`
- Modify: `server/internal/service/execute.go`（删 IsLatestPRMerged）

- [ ] **Step 1: delivery.go 删 deploy-wait 分支 + testRunner 用 executor workdir**

在 `server/internal/service/delivery.go` 的 `Advance` 里：
- 删掉整个 `// deploy：等最近 PR 合并 ... if next == "deploy" ...` 块。
- 把 unit_test 分支的 `s.testRunner.Run(ctx, "/work")` 改为用 executor 的 workdir：
```go
	if next == "unit_test" && s.testRunner != nil {
		wd := ""
		if s.executor != nil {
			wd = s.executor.WorkdirFor(d.ID)
		}
		res, err := s.testRunner.Run(ctx, wd)
		if err == nil && !res.Pass {
			return s.retryCodeAt(ctx, d, "unit_test", res.Detail)
		}
		if err == nil && res.Pass && d.FailCount > 0 {
			if updated, err := s.q.ResetDeliveryFailCount(ctx, d.ID); err == nil {
				d = updated
			}
		}
	}
```
（`code_review` gate 分支、`stage_started`、agent 执行分支不动。`code_review` 现在是终点，`Approve` 后 `Advance` 会走 `stage.Next(code_review)=!ok → completeDelivery`。）

- [ ] **Step 2: 删 IsLatestPRMerged（execute.go）**

删除 `server/internal/service/execute.go` 里整个 `IsLatestPRMerged` 方法（deploy 砍了，不再用）。

- [ ] **Step 3: 编译 + 全量测试**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -5
GOPROXY=https://goproxy.cn,direct go test ./... 2>&1 | tail -12
```
Expected：全绿。若 `delivery_loop_test.go` 的 `TestLoopPassesWhenTestsGreen` 因 code_review 终点而失败（之前期望 deploy→completed），检查：现在 code_review 批准后 `Advance` → `completeDelivery` → completed，断言 `completed` 仍成立。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/service/
git commit -m "feat(server): drop deploy-wait; unit_test runs on real workdir

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 11: RunUntilGate + 异步自动推进

**Files:**
- Modify: `server/internal/service/delivery.go`
- Modify: `server/internal/handler/delivery.go`（Create/Approve 触发 Start）

- [ ] **Step 1: delivery.go 加 running guard + RunUntilGate + Start**

`server/internal/service/delivery.go` 顶部 import 加 `"sync"`。`DeliveryService` 结构加字段：
```go
type DeliveryService struct {
	q           *generated.Queries
	executor    *ExecuteService
	testRunner  testrunner.Runner
	broadcaster realtime.Broadcaster

	runMu    sync.Mutex
	running  map[pgtype.UUID]struct{}
}
```
`New` 改为初始化 `running` map：
```go
func New(pool *pgxpool.Pool) *DeliveryService {
	return &DeliveryService{q: generated.New(pool), running: map[pgtype.UUID]struct{}{}}
}
```
追加方法：
```go
// RunUntilGate 连续推进直到遇到 gate / blocked / completed。
func (s *DeliveryService) RunUntilGate(ctx context.Context, id pgtype.UUID) error {
	for {
		d, err := s.q.GetDelivery(ctx, id)
		if err != nil {
			return err
		}
		if d.Status != generated.DeliveryStatusActive {
			return nil
		}
		if d.PendingGate != nil && *d.PendingGate != "" {
			return nil
		}
		if _, err := s.Advance(ctx, id); err != nil {
			return err
		}
	}
}

// Start 异步跑 RunUntilGate（用 background ctx，不绑请求）。重复启动会被 guard 拦住。
func (s *DeliveryService) Start(id pgtype.UUID) {
	s.runMu.Lock()
	if _, ok := s.running[id]; ok {
		s.runMu.Unlock()
		return
	}
	s.running[id] = struct{}{}
	s.runMu.Unlock()

	go func() {
		defer func() {
			s.runMu.Lock()
			delete(s.running, id)
			s.runMu.Unlock()
		}()
		_ = s.RunUntilGate(context.Background(), id)
	}()
}
```

- [ ] **Step 2: Create 与 Approve 触发 Start（在 handler 里）**

Create 改在 handler（Task 12 会改成 `POST /api/projects/{id}/deliveries`）。本步先让 `Approve` 触发：`server/internal/service/delivery.go` 的 `Approve` 末尾把 `return s.Advance(ctx, id)` 改为：
```go
	if _, err := s.Advance(ctx, id); err != nil {
		return generated.Delivery{}, err
	}
	d, err = s.q.GetDelivery(ctx, id)
	if err != nil {
		return generated.Delivery{}, err
	}
	s.Start(id) // 异步继续跑到下个 gate / completed
	return d, nil
```

- [ ] **Step 3: 写测试 — 自动推进到 gate**

新增 `server/internal/service/delivery_auto_test.go`:
```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/dbtest"
	"github.com/tokfinity/infera/internal/testrunner"
)

// TestRunUntilGateStopsAtSpecApproval：从 intake 自动跑到 spec_approval gate。
func TestRunUntilGateStopsAtSpecApproval(t *testing.T) {
	dbtest.Migrate(t)
	pool := dbtest.Pool(t)
	defer pool.Close()
	dbtest.Truncate(t, pool, "timeline_events", "deliveries", "projects", "agent_configs")
	pid := dbtest.SeedProject(t, pool, "p")
	_, _ = pool.Exec(context.Background(),
		`INSERT INTO agent_configs (name, role, config) VALUES ('Spec Agent','spec','{"system_prompt":"x"}')`)

	fake := agent.NewFakeBackend()
	svc := New(pool).WithExecutor(NewExecute(pool, fake))

	d, _ := svc.Create(context.Background(), pid, CreateInput{Title: "忘记密码"})
	err := svc.RunUntilGate(context.Background(), d.ID)
	assert.NoError(t, err)

	d, _ = svc.Get(context.Background(), d.ID)
	assert.Equal(t, "spec_approval", d.CurrentStage)
	assert.NotNil(t, d.PendingGate)
	assert.Equal(t, "spec_approval", *d.PendingGate)
}
```
（需要 `Get` 方法——已存在 `s.q.GetDelivery`？没有包装。加一个 `func (s *DeliveryService) Get(ctx,id) (generated.Delivery,error){ return s.q.GetDelivery(ctx,id) }` 到 delivery.go。）

- [ ] **Step 4: 跑测试**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go test ./internal/service/ -run TestRunUntilGate -v
```
Expected：PASS（RunUntilGate 从 intake 连续 advance 到 spec_approval 停下）。

- [ ] **Step 5: 编译 + 全量测试**

```bash
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -3
GOPROXY=https://goproxy.cn,direct go test ./... 2>&1 | tail -12
```
Expected：全绿。

- [ ] **Step 6: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/service/
git commit -m "feat(server): RunUntilGate auto-advances to gate; Approve continues

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 12: Delivery handler — 创建改在项目下 + 创建后 Start

**Files:**
- Modify: `server/internal/handler/delivery.go`
- Modify: `server/internal/handler/router.go`

- [ ] **Step 1: 改 Create handler（在项目下创建 + 自动 Start）**

`server/internal/handler/delivery.go` 的 `Create` 改为（从 URL 取 project id）：
```go
// Create 在项目下建 Delivery，并异步自动推进到下个 gate。
func (h *DeliveryHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	d, err := h.svc.Create(r.Context(), parseUUID(projectID), service.CreateInput{
		Title: req.Title, Description: req.Description,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.svc.Start(d.ID) // 异步推进到 gate
	writeJSON(w, http.StatusCreated, d)
}
```
删掉旧的 `createDeliveryReq` 结构体（含 RepoURL/Branch）。

- [ ] **Step 2: 路由加项目内创建**

`server/internal/handler/router.go` 受保护 group 内，在 projects route 里加：
```go
		r.Route("/api/projects", func(r chi.Router) {
			r.Post("/", ph.Create)
			r.Get("/", ph.List)
			r.Get("/{id}", ph.Get)
			r.Patch("/{id}", ph.Update)
			r.Delete("/{id}", ph.Delete)
			r.Post("/{id}/deliveries", dh.Create) // ← 项目内建交付
			r.Get("/{id}/deliveries", dh.ListByProject)
		})
```

- [ ] **Step 3: 加 ListByProject handler**

`server/internal/handler/delivery.go` 追加：
```go
func (h *DeliveryHandler) ListByProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	q := generated.New(h.pool)
	items, err := q.ListDeliveriesByProject(r.Context(), generated.ListDeliveriesByProjectParams{
		ProjectID: parseUUID(projectID), Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}
```

- [ ] **Step 4: 编译 + 手动验证（Fake-free，无 Agent）**

```bash
cd /Users/huiyangz/tokfinity/infera/server
GOPROXY=https://goproxy.cn,direct go build ./... 2>&1 | tail -5
INFERA_PASSWORD=test123 go run ./cmd/server &
sleep 2
curl -s -c /tmp/c.txt -X POST http://localhost:8080/api/login -H 'Content-Type: application/json' -d '{"password":"test123"}' >/dev/null
PID=$(curl -s -b /tmp/c.txt -X POST http://localhost:8080/api/projects -H 'Content-Type: application/json' -d '{"name":"web"}' | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
echo "project=$PID"
curl -s -b /tmp/c.txt -X POST http://localhost:8080/api/projects/$PID/deliveries -H 'Content-Type: application/json' -d '{"title":"忘记密码"}'
echo
kill $(lsof -tiTCP:8080 -sTCP:LISTEN)
```
Expected：建交付返回 201 + delivery JSON（含 `project_id`、`current_stage:intake`、无 `repo_url`）。

- [ ] **Step 5: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add server/internal/handler/
git commit -m "feat(server): create delivery under project + auto-start

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 13: 前端 — 主题系统（深浅双模）+ 顶栏

**Files:**
- Modify: `apps/web/app/globals.css`
- Create: `apps/web/app/components/TopBar.tsx`
- Modify: `apps/web/app/providers.tsx`
- Modify: `apps/web/app/layout.tsx`
- Install: `next-themes`

- [ ] **Step 1: 装 next-themes**

```bash
cd /Users/huiyangz/tokfinity/infera/apps/web
export npm_config_registry=https://registry.npmmirror.com
npm install next-themes
```

- [ ] **Step 2: globals.css — 设计 token（深浅双模）**

`apps/web/app/globals.css` 整体替换为：
```css
@import "tailwindcss";

:root {
  --bg: #ffffff;
  --fg: #0a0a0a;
  --muted: #6b7280;
  --border: #e5e7eb;
  --card: #f9fafb;
  --accent: #2563eb;
  --ok: #16a34a;
  --warn: #d97706;
  --bad: #dc2626;
}
.dark {
  --bg: #0a0a0a;
  --fg: #ededed;
  --muted: #9ca3af;
  --border: #27272a;
  --card: #18181b;
  --accent: #3b82f6;
  --ok: #22c55e;
  --warn: #f59e0b;
  --bad: #ef4444;
}
html, body { background: var(--bg); color: var(--fg); }
body { font-family: var(--font-geist-sans), system-ui, sans-serif; }
.mono { font-family: var(--font-geist-mono), ui-monospace, monospace; }
```

- [ ] **Step 3: providers — 加 ThemeProvider**

`apps/web/app/providers.tsx` 整体替换：
```tsx
"use client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "next-themes";
import { useState, ReactNode } from "react";

export function Providers({ children }: { children: ReactNode }) {
  const [client] = useState(() => new QueryClient());
  return (
    <QueryClientProvider client={client}>
      <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
        {children}
      </ThemeProvider>
    </QueryClientProvider>
  );
}
```

- [ ] **Step 4: TopBar 组件**

`apps/web/app/components/TopBar.tsx`:
```tsx
"use client";
import Link from "next/link";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import { logout } from "@/lib/api";

export function TopBar() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  return (
    <header
      className="flex items-center justify-between border-b px-6 py-3"
      style={{ borderColor: "var(--border)", background: "var(--bg)" }}
    >
      <Link href="/" className="font-bold tracking-tight">
        infera
      </Link>
      <div className="flex items-center gap-3 text-sm">
        {mounted && (
          <button
            className="border rounded px-2 py-1"
            style={{ borderColor: "var(--border)" }}
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? "☀" : "☾"}
          </button>
        )}
        <button
          className="text-[color:var(--muted)]"
          onClick={async () => { await logout(); location.href = "/login"; }}
        >
          登出
        </button>
      </div>
    </header>
  );
}
```

- [ ] **Step 5: layout 包 TopBar**

`apps/web/app/layout.tsx` 的 `<body>` 内，在 `{children}` 外包 `<TopBar/>`（保留 Providers、字体、metadata）。`<body>` 内容形如：
```tsx
        <Providers>
          <TopBar />
          {children}
        </Providers>
```
import 加 `import { TopBar } from "./components/TopBar";`。

- [ ] **Step 6: 验证 build**

```bash
npx tsc --noEmit 2>&1 | tail -5
npm run build 2>&1 | tail -8
```
Expected：编译通过。（`logout` 在 lib/api 还没加——Task 14 加；本步若 tsc 报 `logout` 未定义，先在 `lib/api.ts` 末尾加 `export async function logout(){ const r=await fetch("/api/logout",{method:"POST"}); return r.ok; }`。）

- [ ] **Step 7: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add apps/web/
git commit -m "feat(web): dark/light theme system + top bar

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 14: 前端 — types + api client（auth/projects/delivery）

**Files:**
- Modify: `apps/web/lib/types.ts`
- Modify: `apps/web/lib/api.ts`

- [ ] **Step 1: types — Delivery 去 repo、加 project_id；新增 Project**

`apps/web/lib/types.ts` 的 `Delivery` 改为：
```ts
export interface Delivery {
  id: string;
  project_id: string;
  title: string;
  description: string;
  status: DeliveryStatus;
  current_stage: string;
  pending_gate: string | null;
  fail_count: number;
  created_at: string;
  updated_at: string;
}
```
新增（放文件中部）：
```ts
export interface Project {
  id: string;
  name: string;
  repo_url: string;
  default_branch: string;
  created_at: string;
  updated_at: string;
}
```
（`STAGES` 改为去掉 `"deploy"`，末尾是 `"code_review"`。）

- [ ] **Step 2: api.ts — auth + projects + delivery-under-project**

`apps/web/lib/api.ts` 整体替换为：
```ts
import type { Delivery, DeliveryDetail, Project } from "./types";

// —— auth ——
export async function login(password: string): Promise<boolean> {
  const r = await fetch("/api/login", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
  });
  return r.ok;
}
export async function logout(): Promise<boolean> {
  const r = await fetch("/api/logout", { method: "POST" });
  return r.ok;
}
export async function me(): Promise<{ logged_in: boolean }> {
  const r = await fetch("/api/me");
  if (!r.ok) return { logged_in: false };
  return r.json();
}

// —— projects ——
export async function listProjects(): Promise<Project[]> {
  const r = await fetch("/api/projects"); if (!r.ok) throw new Error("list projects"); return r.json();
}
export async function createProject(input: { name: string; repo_url?: string; default_branch?: string }): Promise<Project> {
  const r = await fetch("/api/projects", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input),
  });
  if (!r.ok) { const e = await r.json().catch(() => ({})); throw new Error(e.error || "create project"); }
  return r.json();
}
export async function getProject(id: string): Promise<Project> {
  const r = await fetch(`/api/projects/${id}`); if (!r.ok) throw new Error("get project"); return r.json();
}
export async function listProjectDeliveries(id: string): Promise<Delivery[]> {
  const r = await fetch(`/api/projects/${id}/deliveries`); if (!r.ok) throw new Error("list deliveries"); return r.json();
}
export async function createDelivery(projectId: string, input: { title: string; description?: string }): Promise<Delivery> {
  const r = await fetch(`/api/projects/${projectId}/deliveries`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input),
  });
  if (!r.ok) throw new Error("create delivery"); return r.json();
}

// —— delivery 详情 / gate ——
export async function getDelivery(id: string): Promise<DeliveryDetail> {
  const r = await fetch(`/api/deliveries/${id}`); if (!r.ok) throw new Error("get delivery"); return r.json();
}
export async function getGate(id: string): Promise<GateInfo> {
  const r = await fetch(`/api/deliveries/${id}/gate`); if (!r.ok) throw new Error("gate"); return r.json();
}
export async function approveGate(id: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/approve`, { method: "POST" }); if (!r.ok) throw new Error("approve"); return r.json();
}
export async function rejectGate(id: string, reason: string): Promise<Delivery> {
  const r = await fetch(`/api/deliveries/${id}/reject`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ reason }),
  }); if (!r.ok) throw new Error("reject"); return r.json();
}
export interface GateInfo {
  delivery_id: string; gate: string;
  agent_output: { agent?: string; output?: string } | null; pr_url: string;
}
```
（删掉旧的 `listDeliveries`/`createDelivery(old)`/`advanceDelivery`——advance 现在是后端自动，前端不再手动调。）

- [ ] **Step 3: tsc**

```bash
cd /Users/huiyangz/tokfinity/infera/apps/web
npx tsc --noEmit 2>&1 | tail -20
```
Expected：页面（page.tsx）引用了已删的 `listDeliveries`/`advanceDelivery` → 报错；Task 15/16 修页面。本步只确认 lib 自身编译。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add apps/web/lib/
git commit -m "feat(web): types + api client for auth/projects/delivery

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 15: 前端 — 登录页 + auth gating + 项目列表(/) + 新建项目

**Files:**
- Create: `apps/web/lib/useRequireAuth.ts`
- Create: `apps/web/app/login/page.tsx`
- Modify: `apps/web/app/page.tsx`（改项目列表）
- Create: `apps/web/app/projects/new/page.tsx`

- [ ] **Step 1: useRequireAuth hook**

`apps/web/lib/useRequireAuth.ts`:
```ts
import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { me } from "@/lib/api";

// 检查登录态；未登录跳 /login。返回 {loggedIn, loading}。
export function useRequireAuth() {
  const router = useRouter();
  const { data, isLoading } = useQuery({ queryKey: ["me"], queryFn: me });
  useEffect(() => {
    if (!isLoading && data && !data.logged_in) router.replace("/login");
  }, [isLoading, data, router]);
  return { loggedIn: !!data?.logged_in, loading: isLoading };
}
```

- [ ] **Step 2: 登录页**

`apps/web/app/login/page.tsx`:
```tsx
"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { login } from "@/lib/api";

export default function LoginPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [pw, setPw] = useState("");
  const [err, setErr] = useState("");
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    const ok = await login(pw);
    if (!ok) { setErr("密码错误"); return; }
    qc.invalidateQueries({ queryKey: ["me"] });
    router.replace("/");
  };
  return (
    <main className="max-w-sm mx-auto p-10">
      <h1 className="text-xl font-bold mb-6">infera 登录</h1>
      <form onSubmit={submit} className="space-y-3">
        <input
          type="password" className="w-full border rounded px-3 py-2"
          style={{ borderColor: "var(--border)", background: "var(--card)" }}
          placeholder="密码" value={pw} onChange={(e) => setPw(e.target.value)}
        />
        {err && <p style={{ color: "var(--bad)" }} className="text-sm">{err}</p>}
        <button className="w-full rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>
          登录
        </button>
      </form>
    </main>
  );
}
```

- [ ] **Step 3: 项目列表（替换首页）**

`apps/web/app/page.tsx` 整体替换：
```tsx
"use client";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { listProjects } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function Home() {
  const { loggedIn, loading } = useRequireAuth();
  const { data } = useQuery({ queryKey: ["projects"], queryFn: listProjects, enabled: loggedIn });

  if (loading || !loggedIn) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-4xl mx-auto p-8">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">项目</h1>
        <Link href="/projects/new" className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>
          新建项目
        </Link>
      </div>
      {data?.length === 0 && <p style={{ color: "var(--muted)" }}>还没有项目。</p>}
      <div className="grid gap-3">
        {data?.map((p) => (
          <Link
            key={p.id} href={`/projects/${p.id}`}
            className="border rounded p-4 flex justify-between items-center hover:opacity-80"
            style={{ borderColor: "var(--border)", background: "var(--card)" }}
          >
            <div>
              <div className="font-medium">{p.name}</div>
              <div className="text-sm mono" style={{ color: "var(--muted)" }}>
                {p.repo_url || "（未绑仓库）"} · {p.default_branch}
              </div>
            </div>
          </Link>
        ))}
      </div>
    </main>
  );
}
```

- [ ] **Step 4: 新建项目页**

`apps/web/app/projects/new/page.tsx`:
```tsx
"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient, useMutation } from "@tanstack/react-query";
import { createProject } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function NewProjectPage() {
  const { loggedIn, loading } = useRequireAuth();
  const router = useRouter();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [repo, setRepo] = useState("");
  const [branch, setBranch] = useState("main");
  const [err, setErr] = useState("");
  const m = useMutation({
    mutationFn: () => createProject({ name, repo_url: repo, default_branch: branch }),
    onSuccess: (p) => { qc.invalidateQueries({ queryKey: ["projects"] }); router.replace(`/projects/${p.id}`); },
    onError: (e: Error) => setErr(e.message),
  });
  if (loading || !loggedIn) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-lg mx-auto p-8">
      <h1 className="text-2xl font-bold mb-6">新建项目</h1>
      <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (name.trim()) m.mutate(); }}>
        <label className="block">
          <span className="text-sm">项目名</span>
          <input className="w-full border rounded px-3 py-2 mt-1" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            value={name} onChange={(e) => setName(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">Git 仓库（绑一次；留空=绿地新项目）</span>
          <input className="w-full border rounded px-3 py-2 mt-1 mono" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            placeholder="https://github.com/you/repo.git" value={repo} onChange={(e) => setRepo(e.target.value)} />
        </label>
        <label className="block">
          <span className="text-sm">默认分支</span>
          <input className="w-full border rounded px-3 py-2 mt-1" style={{ borderColor: "var(--border)", background: "var(--card)" }}
            value={branch} onChange={(e) => setBranch(e.target.value)} />
        </label>
        {err && <p className="text-sm" style={{ color: "var(--bad)" }}>{err}</p>}
        <button className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>创建并绑定</button>
        <p className="text-xs" style={{ color: "var(--muted)" }}>填了仓库会先试 clone 校验可达 + 有写权限。</p>
      </form>
    </main>
  );
}
```

- [ ] **Step 5: build 验证**

```bash
cd /Users/huiyangz/tokfinity/infera/apps/web
npx tsc --noEmit 2>&1 | tail -10
npm run build 2>&1 | tail -10
```
Expected：编译通过（`/deliveries/[id]` 页面此时仍引用旧 api——Task 16 修；若 tsc 报错指向该页，先注释掉其 import 错误项或 Task 16 一并修）。

- [ ] **Step 6: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add apps/web/
git commit -m "feat(web): login page + auth gating + projects list + new project

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 16: 前端 — 项目详情 + 交付详情（面包屑 + 自动推进）

**Files:**
- Create: `apps/web/app/projects/[id]/page.tsx`
- Modify: `apps/web/app/deliveries/[id]/page.tsx`
- Modify: `apps/web/app/deliveries/[id]/gate/page.tsx`（仅去 pr_url 依赖？保留）

- [ ] **Step 1: 项目详情页**

`apps/web/app/projects/[id]/page.tsx`:
```tsx
"use client";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { useState } from "react";
import { getProject, listProjectDeliveries, createDelivery } from "@/lib/api";
import { useRequireAuth } from "@/lib/useRequireAuth";

export default function ProjectPage() {
  const params = useParams<{ id: string }>();
  const { loggedIn, loading } = useRequireAuth();
  const qc = useQueryClient();
  const { data: proj } = useQuery({ queryKey: ["project", params.id], queryFn: () => getProject(params.id), enabled: loggedIn });
  const { data: items } = useQuery({ queryKey: ["project-deliveries", params.id], queryFn: () => listProjectDeliveries(params.id), enabled: loggedIn });
  const [title, setTitle] = useState("");
  const create = useMutation({
    mutationFn: () => createDelivery(params.id, { title }),
    onSuccess: () => { setTitle(""); qc.invalidateQueries({ queryKey: ["project-deliveries", params.id] }); },
  });

  if (loading || !loggedIn || !proj) return <main className="p-8">加载中…</main>;
  return (
    <main className="max-w-4xl mx-auto p-8">
      <Link href="/" className="text-sm" style={{ color: "var(--muted)" }}>← 项目</Link>
      <h1 className="text-2xl font-bold mt-2 mb-1">{proj.name}</h1>
      <div className="text-sm mono mb-6" style={{ color: "var(--muted)" }}>
        {proj.repo_url || "（未绑仓库）"} · {proj.default_branch}
      </div>

      <form className="flex gap-2 mb-6" onSubmit={(e) => { e.preventDefault(); if (title.trim()) create.mutate(); }}>
        <input className="flex-1 border rounded px-3 py-2" style={{ borderColor: "var(--border)", background: "var(--card)" }}
          placeholder="一句话需求…" value={title} onChange={(e) => setTitle(e.target.value)} />
        <button className="rounded px-4 py-2 text-white" style={{ background: "var(--accent)" }}>新建交付</button>
      </form>

      <ul className="space-y-2">
        {items?.map((d) => (
          <li key={d.id} className="border rounded p-4 flex justify-between items-center"
              style={{ borderColor: "var(--border)", background: "var(--card)" }}>
            <div>
              <Link href={`/deliveries/${d.id}`} className="font-medium hover:underline">{d.title}</Link>
              <div className="text-sm flex gap-2 items-center" style={{ color: "var(--muted)" }}>
                <span>{d.current_stage}</span>
                {d.status === "blocked" && <span className="px-2 py-0.5 rounded text-xs" style={{ background: "var(--bad)", color: "#fff" }}>已升级</span>}
                {d.pending_gate && <Link href={`/deliveries/${d.id}/gate`} className="px-2 py-0.5 rounded text-xs" style={{ background: "var(--warn)", color: "#fff" }}>待审批</Link>}
              </div>
            </div>
            <span className="text-xs mono" style={{ color: "var(--muted)" }}>{d.status}</span>
          </li>
        ))}
      </ul>
    </main>
  );
}
```

- [ ] **Step 2: 交付详情页改面包屑 + 去 repo + 保留实时**

`apps/web/app/deliveries/[id]/page.tsx`：
- import 里把 `getDelivery, advanceDelivery` → `getDelivery`（去掉 advanceDelivery；不再有手动推进按钮——后端自动跑）。保留 `useDeliveryEvents`。
- 顶部 `<Link href="/">← 返回</Link>` 改为 `<Link href={\`/projects/${delivery.project_id}\`}>← 项目</Link>`（需要 delivery 加载后用 project_id）。
- 删除「推进到下一 stage」按钮块（自动推进，无需手点）。
- STAGES 已无 deploy（Task 14），流水线胶囊自动少一个。
- 「需审批」入口（pending_gate）保留。
（具体改动：把 header 返回链接换成项目；删 advance mutation 与按钮；其余 timeline/流水线可视化不变。）

- [ ] **Step 3: build 验证**

```bash
cd /Users/huiyangz/tokfinity/infera/apps/web
npx tsc --noEmit 2>&1 | tail -10
npm run build 2>&1 | tail -10
```
Expected：编译通过，路由含 `/login`、`/`、`/projects/new`、`/projects/[id]`、`/deliveries/[id]`、`/deliveries/[id]/gate`。

- [ ] **Step 4: 提交**

```bash
cd /Users/huiyangz/tokfinity/infera
git add apps/web/
git commit -m "feat(web): project detail + delivery detail breadcrumb (auto-advance)

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## P7 完成标准（Definition of Done）

- [ ] `projects` 表 + `deliveries.project_id`（无 `repo_url/branch`），迁移可逆
- [ ] 单用户密码登录门工作；未登录 → 401 / 前端跳 /login
- [ ] 项目 CRUD + 建·时·试·clone 校验
- [ ] Delivery 挂项目、创建后自动推进到 gate、gate 批准后继续到 completed/blocked
- [ ] 阶段 7 个（无 deploy）；code_gen 产真 PR（workdir 挂进容器）、unit_test 在真代码上跑
- [ ] 深浅双模 UI：登录 / 项目列表 / 新建项目 / 项目详情 / 交付详情 / gate
- [ ] 所有 `go test ./...` 通过；`npm run build` 干净

## 依赖 / 风险

- 真实闭环仍需 `ANTHROPIC` 凭证 + `GITHUB_TOKEN`；未给则只验 Fake 单测 + 本地 git/clone 单测。
- 自动推进异步（goroutine + per-delivery guard）；长任务靠 timeline 看进度。
- `completed` = code_review 批准；PR 在 code_gen 开，合不合由人在 GitHub 自行处理。
