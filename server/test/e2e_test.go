package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/persist"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

// E2E：绿地项目（无仓库）+ fake agent 脚本，全链路：
// 创建→spec→门禁→批准→test_gen→code_gen→unit_test→code_review 门禁→批准→completed。
func TestGreenfieldHappyPath(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err, "TRUNCATE 清库失败（库被占用？）")

	st := store.NewPg(pool)

	// fake agent：按角色输出，并把“实现”写进 workdir
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec) echo "# 规格：新增 greeting" ;;
  test_gen) echo "tests: greet_test.go" ;;
  code_gen) echo hello > "$INFERA_WORKDIR/hello.txt"; echo "实现：hello.txt" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f hello.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	base := ts.URL

	// 带 jar 的登录客户端
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err := client.Post(base+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	// 1. 建项目（绿地）
	var proj store.Project
	origin := newBare(t)
	post(t, client, base+"/api/projects",
		fmt.Sprintf(`{"name":"e2e","repo_url":%q,"default_branch":"main"}`, origin), &proj)
	require.Equal(t, origin, proj.RepoURL)

	// 2. 建需求 → 引擎异步推进到 spec 门禁
	d := seedDelivery(t, st, srv, proj.ID, "打招呼", "写 hello")
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "应停在 spec 审批门")

	// 3. gate 拿到 spec artifact；spec 无 complexity 块 → 建议空串（前端按 small 预选）
	var gate gateJSON
	get(t, client, base+"/api/deliveries/"+d.ID+"/gate", &gate)
	require.Equal(t, "spec_approval", gate.Gate)
	require.Contains(t, gate.AgentOutput.Output, "规格")
	require.Empty(t, gate.ComplexitySuggestion)

	// 4. 批准 spec（无建议 → small）→ 直达 test_gen → code_review 门禁（不进 design/tasks）
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "应停在代码审查门")

	// 5. 批准 review → completed
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.Status == "completed"
	}, "应完成")

	// 6. artifacts 齐全：spec/tests/summary/test_output + 真 diff（固化产出）
	var det detailJSON
	get(t, client, base+"/api/deliveries/"+d.ID, &det)
	kinds := map[string]int{}
	var diffContent string
	for _, a := range det.Artifacts {
		kinds[a.Kind]++
		if a.Kind == "diff" {
			diffContent = a.Content
		}
	}
	require.GreaterOrEqual(t, kinds["spec"], 1)
	require.GreaterOrEqual(t, kinds["tests"], 1)
	require.GreaterOrEqual(t, kinds["summary"], 1)
	require.GreaterOrEqual(t, kinds["diff"], 1)
	require.GreaterOrEqual(t, kinds["test_output"], 1)
	require.Contains(t, diffContent, "+++ b/hello.txt", "diff artifact 应是真实 git diff 而非 agent 摘要")
	// 小需求：complexity=small、不进设计/任务阶段。
	require.Equal(t, "small", det.Delivery.Complexity)
	require.Equal(t, 0, kinds["design"])
	require.Equal(t, 0, kinds["tasks"])

	// 7. 事件链完整
	eventTypes := []string{}
	for _, e := range det.Timeline {
		eventTypes = append(eventTypes, e.EventType)
	}
	require.Contains(t, eventTypes, "workspace_ready")
	require.Contains(t, eventTypes, "gate_pending")
	require.Contains(t, eventTypes, "complexity_set")
	require.Contains(t, eventTypes, "persist_done")
	require.Contains(t, eventTypes, "delivery_completed")
}

// —— 第二个用例：unit_test 失败 → 回环 → 重试通过（证明驱动循环） ——
func TestUnitTestLoopRecovers(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err, "TRUNCATE 清库失败（库被占用？）")
	st := store.NewPg(pool)

	// code_gen 首次不写 hello.txt，重试（第二次运行）才写：用标记文件计数
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
if [ "$INFERA_ROLE" = "code_gen" ]; then
  MARK="$INFERA_WORKDIR/.attempt"
  N=$(cat "$MARK" 2>/dev/null || echo 0)
  N=$((N+1)); echo $N > "$MARK"
  if [ "$N" -ge 2 ]; then echo hello > "$INFERA_WORKDIR/hello.txt"; fi
  echo "code_gen attempt $N"
else
  echo "output for $INFERA_ROLE"
fi
`), 0o755))

	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f hello.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err2 := client.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err2)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"loop","repo_url":%q,"default_branch":"main"}`, newBare(t)), &proj)
	d := seedDelivery(t, st, srv, proj.ID, "回环", "x")

	// 到 spec 门禁 → 批准；unit_test 会失败一次（attempt1 无 hello.txt）→ 驱动循环重试 code_gen → 第二次通过 → code_review 门
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "停在 spec 门")
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "回环后停在 code_review 门")

	var det detailJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID, &det)
	require.Equal(t, 0, det.Delivery.FailCount) // 最终通过后重置

	// 事件链里有 test_failed（第一次 unit_test 失败的证据）
	eventTypes := []string{}
	for _, e := range det.Timeline {
		eventTypes = append(eventTypes, e.EventType)
	}
	require.Contains(t, eventTypes, "test_failed")
}

// —— 第三个用例：绑库项目（本地 bare 远端）——固化把分支真的推上去 ——
func TestRepoBackedPushesBranch(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err, "TRUNCATE 清库失败（库被占用？）")
	st := store.NewPg(pool)

	// 本地 bare 远端（含 README 的 main 分支）当项目仓库。
	origin := newBare(t)

	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec) echo "# 规格" ;;
  test_gen) echo "tests: x_test.go" ;;
  code_gen) echo feature > "$INFERA_WORKDIR/feature.txt"; echo "实现：feature.txt" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f feature.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err2 := client.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err2)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	// 绑库项目（本地路径远端）。
	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"repo-backed","repo_url":%q,"default_branch":"main"}`, origin), &proj)
	require.Equal(t, origin, proj.RepoURL)

	d := seedDelivery(t, st, srv, proj.ID, "加功能", "x")
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "停在 spec 门")
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "停在 code_review 门")

	// 门禁拿到预审；pr_url 为空（本地远端不开 PR，产物固化在推送分支上）。
	var gate gateJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID+"/gate", &gate)
	require.Equal(t, "code_review", gate.Gate)
	require.Empty(t, gate.PRURL)

	// 分支已推到远端：infera/<deliveryID 前 8 位>。
	branch := "refs/heads/infera/" + d.ID[:8]
	out, err := exec.Command("git", "ls-remote", origin, branch).Output()
	require.NoError(t, err)
	fields := strings.Fields(string(out))
	require.Len(t, fields, 2, "远端应有分支 %s，实际 ls-remote: %q", branch, string(out))
	require.Len(t, fields[0], 40, "分支应指向一个 commit")

	// diff artifact 是真 diff：含 agent 产出、不含基线 README。
	var det detailJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID, &det)
	var diffContent string
	for _, a := range det.Artifacts {
		if a.Kind == "diff" {
			diffContent = a.Content
		}
	}
	require.Contains(t, diffContent, "+++ b/feature.txt")
	require.NotContains(t, diffContent, "README.md")

	// 放行 → completed。
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.Status == "completed"
	}, "应完成")
}

// newBare 建一个带 1 个 commit（README.md）的本地 bare 仓库当远端。
func newBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "seed")
	run := func(cwd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run(dir, "init", "--bare", "-b", "main", origin)
	run(dir, "init", "-b", "main", work)
	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# hi\n"), 0o644)
	run(work, "add", ".")
	run(work, "commit", "-m", "init")
	run(work, "push", origin, "main")
	return origin
}

// --- HTTP 测试辅助 ---

// detailJSON 是 GET /api/deliveries/{id} 的响应体。
type detailJSON struct {
	Delivery  store.Delivery   `json:"delivery"`
	Timeline  []store.Event    `json:"timeline"`
	Artifacts []store.Artifact `json:"artifacts"`
}

// gateJSON 是 GET /api/deliveries/{id}/gate 的响应体（只取断言用到的字段）。
type gateJSON struct {
	Gate        string `json:"gate"`
	AgentOutput struct {
		Agent  string `json:"agent"`
		Output string `json:"output"`
	} `json:"agent_output"`
	PRURL string `json:"pr_url"`
	// spec_approval 门：AI 复杂度建议（无/坏块 = 空串）。
	ComplexitySuggestion string `json:"complexity_suggestion"`
}

// post 发 JSON POST，断言 2xx（创建类端点 201、其余 200）；out 非 nil 时解码响应体。
func post(t *testing.T, c *http.Client, url, body string, out any) {
	t.Helper()
	r, err := c.Post(url, "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Less(t, r.StatusCode, 300, "POST %s -> %d", url, r.StatusCode)
	require.GreaterOrEqual(t, r.StatusCode, 200, "POST %s -> %d", url, r.StatusCode)
	if out != nil {
		require.NoError(t, json.NewDecoder(r.Body).Decode(out))
	}
}

// get 发 GET，断言 200 并解码响应体到 out。
func get(t *testing.T, c *http.Client, url string, out any) {
	t.Helper()
	r, err := c.Get(url)
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusOK, r.StatusCode, "GET %s -> %d", url, r.StatusCode)
	require.NoError(t, json.NewDecoder(r.Body).Decode(out))
}

// waitFor 每 150ms 轮询 GET /api/deliveries/{id}，直到 cond 成立；15s 未满足则带末态 fatal。
func waitFor(t *testing.T, c *http.Client, base, deliveryID string, cond func(detailJSON) bool, msg string) detailJSON {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var det detailJSON
		get(t, c, base+"/api/deliveries/"+deliveryID, &det)
		if cond(det) {
			return det
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitFor 超时（15s）：%s；末态 status=%s stage=%s gate=%s fail_count=%d",
				msg, det.Delivery.Status, det.Delivery.CurrentStage, det.Delivery.PendingGate, det.Delivery.FailCount)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// --- split deliveries E2E ---

// seedDelivery 直连 store 建交付并异步点火。POST /api/projects/{id}/deliveries
// 已删除（生产创建入口 POST /api/projects/{id}/requirements 走上游同步，e2e
// 无上游可用）；本 helper 复刻被删 handler 的落库形状（active + intake +
// delivery_created 事件 + 后台驱动），后续推进/门禁仍全部走 HTTP 面。
func seedDelivery(t *testing.T, st store.Store, srv *api.Server, projectID, title, description string) store.Delivery {
	t.Helper()
	d := &store.Delivery{
		ProjectID:    projectID,
		Title:        title,
		Description:  description,
		Status:       "active",
		CurrentStage: "intake",
	}
	require.NoError(t, st.CreateDelivery(context.Background(), d))
	require.NoError(t, st.AppendEvent(context.Background(), &store.Event{
		DeliveryID: d.ID,
		Stage:      "intake",
		EventType:  "delivery_created",
		Payload:    []byte(`{}`),
	}))
	go srv.RunDelivery(d.ID)
	return *d
}

// listDeliveries 读项目交付列表（GET /api/projects/{id}/deliveries 已删除，
// 改直连 store——断言的是数据而非 HTTP 形状）。
func listDeliveries(t *testing.T, st store.Store, projectID string) []store.Delivery {
	t.Helper()
	ds, err := st.ListProjectDeliveries(context.Background(), projectID)
	require.NoError(t, err)
	return ds
}

// splitGateJSON 在 gateJSON 上加 split_plan 字段。
type splitGateJSON struct {
	gateJSON
	SplitPlan *[]store.ChildSpec `json:"split_plan"`
}

// tasksGateJSON 在 gateJSON 上加 tasks 字段（任务审批门的可编辑清单）。
type tasksGateJSON struct {
	gateJSON
	Tasks *[]store.TaskSpec `json:"tasks"`
}

// driveChildren 轮询项目交付列表，给所有停在门禁的子需求放行；
// 子需求全部完成后给父放行 code_review。父 completed 时返回。
func driveChildren(t *testing.T, st store.Store, c *http.Client, base, projectID, parentID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		ds := listDeliveries(t, st, projectID)
		parentDone, childrenDone := false, true
		for i := range ds {
			d := &ds[i]
			if d.ID == parentID {
				parentDone = d.Status == "completed"
				continue
			}
			if d.ParentID == parentID && d.Status != "completed" {
				childrenDone = false
			}
			// 子需求停在门禁 → 放行
			if d.ParentID == parentID && d.Status == "active" && d.PendingGate != "" {
				post(t, c, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
			}
		}
		if childrenDone && !parentDone {
			var det detailJSON
			get(t, c, base+"/api/deliveries/"+parentID, &det)
			if det.Delivery.PendingGate == "code_review" {
				post(t, c, base+"/api/deliveries/"+parentID+"/approve", `{}`, nil)
			} else if det.Delivery.Status == "blocked" {
				t.Fatalf("父 blocked：%+v", det.Delivery)
			}
		}
		if parentDone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("driveChildren 超时；末态：%+v", ds)
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// TestSplitFlow 拆分全链路：AI spec 带 infera-complexity 建议 → 按建议 large 进设计门 →
// AI design 带 infera-split 建议 → gate 解析 split_plan → 批准并拆分 →
// wave1 两子需求并行过门（子各自 small 全流程）→ 增量合并进父 → wave2 启动 → 全部合并 →
// 父 unit_test/code_review（persist 推父分支）→ completed；bare 上父分支含全部产物。
func TestSplitFlow(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err)
	st := store.NewPg(pool)

	// fake agent：根 spec 带 ROOT-SPEC 标记 → 产出 complexity 建议块（large）；
	// design 产出拆分建议块；子需求 spec 通用文本（无块 → small）；
	// code_gen 按 prompt 里的文件名写文件。
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec)
    case "$INFERA_PROMPT" in
      *ROOT-SPEC*) F=$(printf '\140\140\140'); printf '# 规格总览\n\n%sinfera-complexity\nlarge\n%s\n' "$F" "$F" ;;
      *) echo "# 子规格" ;;
    esac ;;
  design)
    F=$(printf '\140\140\140'); printf '# 设计正文\n\n%sinfera-split\n[{"title":"子A","description":"写入 a.txt","wave":1},{"title":"子B","description":"写入 b.txt","wave":1},{"title":"子C","description":"写入 c.txt","wave":2}]\n%s\n' "$F" "$F" ;;
  tasks) echo "任务清单" ;;
  test_gen) echo "tests" ;;
  code_gen)
    for f in a.txt b.txt c.txt; do
      case "$INFERA_PROMPT" in
        *$f*) echo "$f" > "$INFERA_WORKDIR/$f" ;;
      esac
    done
    echo "code_gen done" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	origin := newBare(t)
	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "true"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	eng.OnStartDelivery = func(id string) { go srv.RunDelivery(id) }
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err2 := client.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err2)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"split","repo_url":%q,"default_branch":"main"}`, origin), &proj)

	parent := seedDelivery(t, st, srv, proj.ID, "父需求", "ROOT-SPEC 写三个文件")
	waitFor(t, client, ts.URL, parent.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "父停在 spec 门")

	// spec 门：complexity_suggestion=large（spec 末尾 infera-complexity 块）；
	// 批准不带 complexity → 按建议 large → 进设计门。
	var specGate gateJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/gate", &specGate)
	require.Equal(t, "spec_approval", specGate.Gate)
	require.Equal(t, "large", specGate.ComplexitySuggestion)
	post(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/approve", `{}`, nil)
	specDet := waitFor(t, client, ts.URL, parent.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "design_approval"
	}, "按建议 large 进设计门")
	require.Equal(t, "large", specDet.Delivery.Complexity)

	// design 门：解析出 AI 拆分建议（design 末尾 infera-split 块）
	var gate splitGateJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/gate", &gate)
	require.Equal(t, "design_approval", gate.Gate)
	require.NotNil(t, gate.SplitPlan, "design 带 infera-split 块应解析出 split_plan")
	require.Len(t, *gate.SplitPlan, 3)
	require.Equal(t, 1, (*gate.SplitPlan)[0].Wave)
	require.Equal(t, 1, (*gate.SplitPlan)[1].Wave)
	require.Equal(t, 2, (*gate.SplitPlan)[2].Wave)

	// 批准并拆分：把解析到的 plan 原样提交
	splitBody, err := json.Marshal(map[string]any{"split": *gate.SplitPlan})
	require.NoError(t, err)
	post(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/approve", string(splitBody), nil)

	// 父 + 3 子 = 4；wave1 两子 active，wave2 子 queued（批次门控）
	require.Eventually(t, func() bool {
		return len(listDeliveries(t, st, proj.ID)) == 4
	}, 5*time.Second, 100*time.Millisecond)
	ds := listDeliveries(t, st, proj.ID)
	var w2 *store.Delivery
	for i := range ds {
		if ds[i].Wave == 2 {
			w2 = &ds[i]
		}
	}
	require.NotNil(t, w2)
	require.Equal(t, "queued", w2.Status, "wave2 未到批次应保持 queued")

	// 父详情含 children
	var det struct {
		Delivery  store.Delivery    `json:"delivery"`
		Children  *[]store.Delivery `json:"children"`
		Timeline  []store.Event     `json:"timeline"`
		Artifacts []store.Artifact  `json:"artifacts"`
	}
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID, &det)
	require.True(t, det.Delivery.SplitMode)
	require.Equal(t, "large", det.Delivery.Complexity)
	require.Equal(t, "code_gen", det.Delivery.CurrentStage, "拆分父停 code_gen（跳过 tasks/tasks_approval/test_gen）")
	require.NotNil(t, det.Children)
	require.Len(t, *det.Children, 3)

	// 驱动全部子需求过门 → 增量合并 → wave2 启动 → 父收尾完成
	driveChildren(t, st, client, ts.URL, proj.ID, parent.ID)

	// wave2 曾被批次门控（上面断言过 queued），最终也启动并完成
	ds = listDeliveries(t, st, proj.ID)
	completed := 0
	for _, d := range ds {
		if d.ParentID == parent.ID && d.Status == "completed" {
			completed++
		}
	}
	require.Equal(t, 3, completed, "三个子需求都应完成")

	// 父事件链：complexity_set / split / merge_done / 收尾
	var final detailJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID, &final)
	require.Equal(t, "completed", final.Delivery.Status)
	eventNames := map[string]bool{}
	for _, e := range final.Timeline {
		eventNames[e.EventType] = true
	}
	require.True(t, eventNames["complexity_set"])
	require.True(t, eventNames["split"])
	require.True(t, eventNames["merge_done"])
	require.True(t, eventNames["persist_done"])

	// bare 上父固化分支含三个子需求的全部产物
	branch := "infera/" + parent.ID[:8]
	merged := filepath.Join(t.TempDir(), "merged")
	out, err := exec.Command("git", "clone", "-q", "-b", branch, origin, merged).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		_, err := os.Stat(filepath.Join(merged, f))
		require.NoError(t, err, "父分支应含 %s", f)
	}
}

// TestLargeNoSplitFlow 大需求不拆分链路：spec 带 complexity 块 → 显式批准 large →
// design（不带 split 块，gate split_plan=null）→ 普通批准 → tasks（infera-tasks 块）→
// tasks_approval（gate 返回清单）→ 批准并覆盖清单（tasks_overridden）→ test_gen →
// code_gen 逐任务实现（每任务写一个文件，task_done 逐条落盘）→ unit_test（校验全部
// 任务产物）→ code_review → completed。
func TestLargeNoSplitFlow(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err)
	st := store.NewPg(pool)

	// fake agent：tasks 产 infera-tasks 块（2 任务）；code_gen 按 prompt 里出现的
	// 任务文件名写文件（逐任务调用时每次 prompt 只含一个任务）。
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec) F=$(printf '\140\140\140'); printf '# 规格\n\n%sinfera-complexity\nlarge\n%s\n' "$F" "$F" ;;
  design) echo "# 设计正文（不拆分）" ;;
  tasks) F=$(printf '\140\140\140'); printf '任务清单\n\n%sinfera-tasks\n[{"title":"写 big1.txt","detail":"写入 big1.txt"},{"title":"写 big2.txt","detail":"写入 big2.txt"}]\n%s\n' "$F" "$F" ;;
  test_gen) echo "tests" ;;
  code_gen)
    for f in big1.txt big2.txt big3.txt; do
      case "$INFERA_PROMPT" in
        *$f*) echo "$f" > "$INFERA_WORKDIR/$f" ;;
      esac
    done
    echo "code_gen done" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f big1.txt -a -f big2.txt -a -f big3.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err2 := client.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err2)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"large","repo_url":%q,"default_branch":"main"}`, newBare(t)), &proj)
	d := seedDelivery(t, st, srv, proj.ID, "大需求", "写多个文件")

	// spec 门：建议 large；显式批准 large（人工可改判路径）→ 设计门
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "停在 spec 门")
	var specGate gateJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID+"/gate", &specGate)
	require.Equal(t, "large", specGate.ComplexitySuggestion)
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{"complexity":"large"}`, nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "design_approval"
	}, "large 进设计门")

	// 设计门：设计文档可审 + split_plan=null（不带拆分块）→ 普通批准 → 任务门
	var designGate splitGateJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID+"/gate", &designGate)
	require.Equal(t, "design_approval", designGate.Gate)
	require.Contains(t, designGate.AgentOutput.Output, "设计正文")
	require.Nil(t, designGate.SplitPlan, "无 infera-split 块 → split_plan=null")
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "tasks_approval"
	}, "不拆分 → 任务门")

	// 任务门：gate 返回解析后的清单（2 项，tasks artifact 为 JSON）
	var tasksGate tasksGateJSON
	get(t, client, ts.URL+"/api/deliveries/"+d.ID+"/gate", &tasksGate)
	require.Equal(t, "tasks_approval", tasksGate.Gate)
	require.NotNil(t, tasksGate.Tasks, "infera-tasks 块应解析出清单")
	require.Len(t, *tasksGate.Tasks, 2)
	require.Equal(t, "写 big1.txt", (*tasksGate.Tasks)[0].Title)

	// 批准并覆盖清单（编辑器模式）：改 detail + 增补第三个任务
	override := append(*tasksGate.Tasks, store.TaskSpec{Title: "写 big3.txt", Detail: "写入 big3.txt"})
	override[1].Detail = "写入 big2.txt（人工修订）"
	tasksBody, err := json.Marshal(map[string]any{"tasks": override})
	require.NoError(t, err)
	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", string(tasksBody), nil)
	waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "任务门放行 → 逐任务实现 → code_review")

	post(t, client, ts.URL+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	det := waitFor(t, client, ts.URL, d.ID, func(det detailJSON) bool {
		return det.Delivery.Status == "completed"
	}, "应完成")

	// 产物与事件：覆盖后清单为最新 tasks artifact、每任务一条 task_done、合成 summary、
	// unit_test 通过说明三个文件都写了（逐任务实现真的跑了）。
	require.Equal(t, "large", det.Delivery.Complexity)
	kinds := map[string]int{}
	eventCount := map[string]int{}
	var summary, latestTasks string
	for _, a := range det.Artifacts {
		kinds[a.Kind]++
		switch a.Kind {
		case "summary":
			summary = a.Content
		case "tasks":
			latestTasks = a.Content
		}
	}
	for _, e := range det.Timeline {
		eventCount[e.EventType]++
	}
	require.Equal(t, 2, kinds["tasks"], "AI 原始清单 + 人工覆盖清单")
	require.Equal(t, `[{"title":"写 big1.txt","detail":"写入 big1.txt"},{"title":"写 big2.txt","detail":"写入 big2.txt（人工修订）"},{"title":"写 big3.txt","detail":"写入 big3.txt"}]`, latestTasks)
	require.Equal(t, 3, kinds["task_done"], "三个任务各一条完成标记")
	require.Equal(t, 3, eventCount["task_done"])
	require.Equal(t, "按任务清单完成 3 项实现：写 big1.txt、写 big2.txt、写 big3.txt", summary)
	require.Equal(t, 1, eventCount["tasks_overridden"])
	require.True(t, eventCount["complexity_set"] > 0)
	require.GreaterOrEqual(t, kinds["design"], 1)
	require.GreaterOrEqual(t, kinds["tests"], 1)
	require.GreaterOrEqual(t, kinds["diff"], 1)
}

// TestSplitConflictResumeE2E 冲突路径全链路：两个 wave1 子需求写同一文件（内容不同）→
// 父 merge_conflict（事件带 git 指引）→ 模拟人工解冲突推父分支 → POST merge/resume →
// wave2 启动 → 父最终 completed。
func TestSplitConflictResumeE2E(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL 未设置")
	}
	if err := db.Migrate(dbURL); err != nil {
		t.Fatal(err)
	}
	pool, err := db.Connect(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects, pipeline_bindings, agents, requirements, gate_cards, audit_log, project_settings`)
	require.NoError(t, err)
	st := store.NewPg(pool)

	// 根 spec 带 complexity 建议块（large）；design 产拆分建议块；
	// code_gen 写 same.txt，内容 = workdir 名（即 delivery id）——两个子需求必然冲突。
	fakeScript := filepath.Join(t.TempDir(), "fake-agent.sh")
	require.NoError(t, os.WriteFile(fakeScript, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  spec)
    case "$INFERA_PROMPT" in
      *ROOT-SPEC*) F=$(printf '\140\140\140'); printf '# 规格总览\n\n%sinfera-complexity\nlarge\n%s\n' "$F" "$F" ;;
      *) echo "# 子规格" ;;
    esac ;;
  design)
    F=$(printf '\140\140\140'); printf '# 设计正文\n\n%sinfera-split\n[{"title":"子A","description":"写 same.txt 与 a.txt","wave":1},{"title":"子B","description":"写 same.txt 与 b.txt","wave":1},{"title":"子C","description":"写入 c.txt","wave":2}]\n%s\n' "$F" "$F" ;;
  tasks) echo "任务清单" ;;
  test_gen) echo "tests" ;;
  code_gen)
    case "$INFERA_PROMPT" in
      *same.txt*) basename "$INFERA_WORKDIR" > "$INFERA_WORKDIR/same.txt" ;;
    esac
    for f in a.txt b.txt c.txt; do
      case "$INFERA_PROMPT" in
        *$f*) echo "$f" > "$INFERA_WORKDIR/$f" ;;
      esac
    done
    echo "code_gen done" ;;
  code_review) echo "LGTM" ;;
esac
`), 0o755))

	origin := newBare(t)
	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "true"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	eng.OnStartDelivery = func(id string) { go srv.RunDelivery(id) }
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err2 := client.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err2)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	var proj store.Project
	post(t, client, ts.URL+"/api/projects",
		fmt.Sprintf(`{"name":"conflict","repo_url":%q,"default_branch":"main"}`, origin), &proj)
	parent := seedDelivery(t, st, srv, proj.ID, "父需求", "ROOT-SPEC 冲突场景")
	waitFor(t, client, ts.URL, parent.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "父停在 spec 门")

	// spec 门按建议 large → 设计门拿拆分建议
	post(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/approve", `{}`, nil)
	waitFor(t, client, ts.URL, parent.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "design_approval"
	}, "进设计门")

	var gate splitGateJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/gate", &gate)
	require.Equal(t, "design_approval", gate.Gate)
	require.NotNil(t, gate.SplitPlan)
	splitBody, err := json.Marshal(map[string]any{"split": *gate.SplitPlan})
	require.NoError(t, err)
	post(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/approve", string(splitBody), nil)

	// 驱动子需求过门（wave1 完成即触发增量合并 → 第二个合并冲突暂停队列，
	// wave2 子需求保持 queued，父停在 merge_state=conflict）。
	driveChildrenUntilConflict(t, st, client, ts.URL, proj.ID, parent.ID)
	var conflictDet detailJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID, &conflictDet)
	var instructions string
	for _, e := range conflictDet.Timeline {
		if e.EventType == "merge_conflict" {
			var pl struct {
				Instructions string `json:"instructions"`
			}
			require.NoError(t, json.Unmarshal(e.Payload, &pl))
			instructions = pl.Instructions
		}
	}
	require.Contains(t, instructions, "git push origin infera/"+parent.ID[:8])

	// 模拟人工：clone origin → 从 main 建父分支 → 依次合两个子分支（解冲突）→ 推送。
	human := filepath.Join(t.TempDir(), "human")
	out, err := exec.Command("git", "clone", "-q", origin, human).CombinedOutput()
	require.NoError(t, err, string(out))
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = human
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=h", "GIT_AUTHOR_EMAIL=h@h", "GIT_COMMITTER_NAME=h", "GIT_COMMITTER_EMAIL=h@h")
		o, err := cmd.CombinedOutput()
		require.NoError(t, err, strings.Join(args, " ")+": "+string(o))
	}
	runGit("fetch", "origin")
	runGit("checkout", "-b", "infera/"+parent.ID[:8], "origin/main")
	// 按完成顺序合子分支；第二个 same.txt 冲突 → 手写解决内容。
	resolved := 0
	for _, d := range listDeliveries(t, st, proj.ID) {
		if d.ParentID != parent.ID || d.Status != "completed" {
			continue
		}
		runGit("fetch", "origin", "infera/"+d.ID[:8])
		merge := exec.Command("git", "merge", "--no-edit", "FETCH_HEAD")
		merge.Dir = human
		if err := merge.Run(); err != nil {
			require.NoError(t, os.WriteFile(filepath.Join(human, "same.txt"), []byte("resolved\n"), 0o644))
			runGit("add", "-A")
			runGit("commit", "-m", "resolve")
			resolved++
		}
	}
	require.Equal(t, 1, resolved, "应有且仅有一个子分支合并冲突")
	runGit("push", "origin", "infera/"+parent.ID[:8])

	// 继续恢复：wave2 启动 → 驱动到底
	post(t, client, ts.URL+"/api/deliveries/"+parent.ID+"/merge/resume", `{}`, nil)
	driveChildren(t, st, client, ts.URL, proj.ID, parent.ID)

	var final detailJSON
	get(t, client, ts.URL+"/api/deliveries/"+parent.ID, &final)
	require.Equal(t, "completed", final.Delivery.Status)
	require.Equal(t, "", final.Delivery.MergeState)

	// 父分支：same.txt 已解决 + wave1/wave2 全部产物
	merged := filepath.Join(t.TempDir(), "merged")
	out, err = exec.Command("git", "clone", "-q", "-b", "infera/"+parent.ID[:8], origin, merged).CombinedOutput()
	require.NoError(t, err, string(out))
	same, err := os.ReadFile(filepath.Join(merged, "same.txt"))
	require.NoError(t, err)
	require.Equal(t, "resolved\n", string(same))
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		_, err := os.Stat(filepath.Join(merged, f))
		require.NoError(t, err, "父分支应含 %s", f)
	}
}

// driveChildrenUntilConflict 只驱动子需求门禁，不碰父；父进入 conflict 即返回
// （wave2 子需求仍 queued，等恢复后才会启动）。
func driveChildrenUntilConflict(t *testing.T, st store.Store, c *http.Client, base, projectID, parentID string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		for _, d := range listDeliveries(t, st, projectID) {
			if d.ParentID == parentID && d.Status == "active" && d.PendingGate != "" {
				post(t, c, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
			}
		}
		var det detailJSON
		get(t, c, base+"/api/deliveries/"+parentID, &det)
		if det.Delivery.MergeState == "conflict" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("driveChildrenUntilConflict 超时；父末态 %+v", det.Delivery)
		}
		time.Sleep(150 * time.Millisecond)
	}
}
