package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
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
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects`)
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

	ws := workspace.New(t.TempDir(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f hello.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr)
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
	post(t, client, base+"/api/projects", `{"name":"e2e"}`, &proj)
	require.Empty(t, proj.RepoURL)

	// 2. 建需求 → 引擎异步推进到 spec 门禁
	var d store.Delivery
	post(t, client, base+"/api/projects/"+proj.ID+"/deliveries", `{"title":"打招呼","description":"写 hello"}`, &d)
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "应停在 spec 审批门")

	// 3. gate 拿到 spec artifact
	var gate gateJSON
	get(t, client, base+"/api/deliveries/"+d.ID+"/gate", &gate)
	require.Equal(t, "spec_approval", gate.Gate)
	require.Contains(t, gate.AgentOutput.Output, "规格")

	// 4. 批准 spec → code_review 门禁（经过 test_gen/code_gen/unit_test）
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "应停在代码审查门")

	// 5. 批准 review → completed
	post(t, client, base+"/api/deliveries/"+d.ID+"/approve", `{}`, nil)
	waitFor(t, client, base, d.ID, func(det detailJSON) bool {
		return det.Delivery.Status == "completed"
	}, "应完成")

	// 6. artifacts 齐全：spec/tests/diff/test_output
	var det detailJSON
	get(t, client, base+"/api/deliveries/"+d.ID, &det)
	kinds := map[string]int{}
	for _, a := range det.Artifacts {
		kinds[a.Kind]++
	}
	require.GreaterOrEqual(t, kinds["spec"], 1)
	require.GreaterOrEqual(t, kinds["tests"], 1)
	require.GreaterOrEqual(t, kinds["diff"], 1)
	require.GreaterOrEqual(t, kinds["test_output"], 1)

	// 7. 事件链完整
	eventTypes := []string{}
	for _, e := range det.Timeline {
		eventTypes = append(eventTypes, e.EventType)
	}
	require.Contains(t, eventTypes, "workspace_ready")
	require.Contains(t, eventTypes, "gate_pending")
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
	_, err = pool.Exec(context.Background(), `TRUNCATE events, artifacts, stage_runs, deliveries, projects`)
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

	ws := workspace.New(t.TempDir(), time.Hour)
	ar := agent.NewLocal([]string{"sh", fakeScript})
	tr := &testrunner.Local{Script: "test -f hello.txt"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr)
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
	post(t, client, ts.URL+"/api/projects", `{"name":"loop"}`, &proj)
	var d store.Delivery
	post(t, client, ts.URL+"/api/projects/"+proj.ID+"/deliveries", `{"title":"回环","description":"x"}`, &d)

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
}

// post 发 JSON POST，断言 200；out 非 nil 时解码响应体。
func post(t *testing.T, c *http.Client, url, body string, out any) {
	t.Helper()
	r, err := c.Post(url, "application/json", bytes.NewBufferString(body))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusOK, r.StatusCode, "POST %s -> %d", url, r.StatusCode)
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
