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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/agent"
	"github.com/tokfinity/infera/internal/api"
	"github.com/tokfinity/infera/internal/db"
	"github.com/tokfinity/infera/internal/engine"
	"github.com/tokfinity/infera/internal/git"
	"github.com/tokfinity/infera/internal/orchestration"
	"github.com/tokfinity/infera/internal/persist"
	"github.com/tokfinity/infera/internal/store"
	"github.com/tokfinity/infera/internal/testrunner"
	"github.com/tokfinity/infera/internal/workspace"
)

// TestAgentBindings 编排配置全链路（项目级绑定是唯一来源，全局默认已删除）：
//  1. 项目 A 绑定脚本 A（全部节点）→ 交付跑通，test_gen 产物带 A 标记；
//  2. 项目 B 全量绑定（test_gen→脚本B，其余→A）→ B 的 test_gen 产物带 B 标记，A 不受影响；
//  3. 直插一条遗留全局绑定行 + 删掉 A 的 test_gen 项目绑定 → 新交付 blocked
//     且 stage_failed 写明缺 test_gen（全局行不得兜底）。
func TestAgentBindings(t *testing.T) {
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

	// 两个 fake agent 脚本：test_gen 角色输出各自标记；其余角色输出通用文本。
	scriptA := filepath.Join(t.TempDir(), "agent-a.sh")
	require.NoError(t, os.WriteFile(scriptA, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  test_gen) echo "tests from AGENT_A" ;;
  *) echo "output from AGENT_A ($INFERA_ROLE)" ;;
esac
`), 0o755))
	scriptB := filepath.Join(t.TempDir(), "agent-b.sh")
	require.NoError(t, os.WriteFile(scriptB, []byte(`#!/bin/sh
case "$INFERA_ROLE" in
  test_gen) echo "tests from AGENT_B" ;;
  *) echo "output from AGENT_B ($INFERA_ROLE)" ;;
esac
`), 0o755))

	ws := workspace.New(t.TempDir(), git.New(), time.Hour)
	ar := agent.NewLocal([]string{"sh", scriptA}) // 兜底（编排解析前的单 runner）
	tr := &testrunner.Local{Script: "true"}
	srv := api.NewServer(st, "e2e-pass", nil)
	eng := engine.New(st, ar, ws, tr).WithPersister(persist.NewLocal(git.New(), ""))
	eng.ResolveRunner = func(ctx context.Context, projectID, node string) (agent.Runner, error) {
		agents, _, err := orchestration.Resolve(ctx, st, projectID)
		if err != nil {
			return nil, err
		}
		a, ok := agents[node]
		if !ok {
			return nil, &orchestration.ErrIncompleteBindings{Missing: []string{node}}
		}
		return orchestration.RunnerFor(a)
	}
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	base := ts.URL

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	r, err := client.Post(base+"/api/login", "application/json", bytes.NewBufferString(`{"password":"e2e-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	_ = r.Body.Close()

	// 注册两个 cli agent（含 R10 双道审查节点的全量绑定随后按项目下发）。
	idA := createAgentE2E(t, client, base, "default-cli", scriptA)
	idB := createAgentE2E(t, client, base, "agent-b", scriptB)
	fullA := map[string]string{
		"spec": idA, "test_gen": idA, "code_gen": idA, "code_review": idA,
		"spec_conformance": idA, "code_quality": idA,
	}

	// --- 1. 项目 A 全量绑定脚本 A：交付跑通 ---
	var projA, projB store.Project
	post(t, client, base+"/api/projects",
		fmt.Sprintf(`{"name":"a","repo_url":%q,"default_branch":"main"}`, newBare(t)), &projA)
	post(t, client, base+"/api/projects",
		fmt.Sprintf(`{"name":"b","repo_url":%q,"default_branch":"main"}`, newBare(t)), &projB)
	putProjectBindings(t, client, base, projA.ID, fullA)

	dA := seedDelivery(t, st, srv, projA.ID, "A", "x")
	waitFor(t, client, base, dA.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "A 停在 spec 门")
	post(t, client, base+"/api/deliveries/"+dA.ID+"/approve", `{}`, nil)
	det := waitFor(t, client, base, dA.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "A 到 code_review 门")
	require.Contains(t, artifactContent(det, "tests"), "tests from AGENT_A")

	// --- 2. 项目 B 绑定 test_gen→B、其余→A ---
	putProjectBindings(t, client, base, projB.ID, func() map[string]string {
		b := map[string]string{}
		for k, v := range fullA {
			b[k] = v
		}
		b["test_gen"] = idB
		return b
	}())
	dB := seedDelivery(t, st, srv, projB.ID, "B", "x")
	waitFor(t, client, base, dB.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "B 停在 spec 门")
	post(t, client, base+"/api/deliveries/"+dB.ID+"/approve", `{}`, nil)
	det = waitFor(t, client, base, dB.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "B 到 code_review 门")
	require.Contains(t, artifactContent(det, "tests"), "tests from AGENT_B", "B 的 test_gen 应来自 agent-b")
	// B 的其余节点绑定的是 A
	require.Contains(t, artifactContent(det, "summary"), "AGENT_A", "B 的 code_gen 走项目绑定的 agent A")

	// A 不受影响：A 的既有 delivery 已在上面断言过 AGENT_A；
	// A 的新交付仍走自己的项目绑定。
	dA2 := seedDelivery(t, st, srv, projA.ID, "A2", "x")
	waitFor(t, client, base, dA2.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "spec_approval"
	}, "A2 停在 spec 门")
	post(t, client, base+"/api/deliveries/"+dA2.ID+"/approve", `{}`, nil)
	det = waitFor(t, client, base, dA2.ID, func(det detailJSON) bool {
		return det.Delivery.PendingGate == "code_review"
	}, "A2 到 code_review 门")
	require.Contains(t, artifactContent(det, "tests"), "tests from AGENT_A", "A 不受 B 覆盖影响")

	// --- 3. 遗留全局行不兜底 + 删 A 的 test_gen 项目绑定 → 新交付 blocked ---
	// 直插一条 0009 之前的遗留全局绑定行（test_gen→B）：若解析仍走全局兜底，
	// test_gen 会解析成功，交付不会 blocked——以此钉死「项目绑定是唯一来源」。
	_, err = pool.Exec(context.Background(),
		`INSERT INTO pipeline_bindings (id, project_id, node, agent_id)
		 VALUES (gen_random_uuid(), NULL, 'test_gen', $1)`, idB)
	require.NoError(t, err)
	require.NoError(t, st.DeleteBinding(context.Background(), projA.ID, "test_gen"))
	dC := seedDelivery(t, st, srv, projA.ID, "C", "x")
	det = waitFor(t, client, base, dC.ID, func(det detailJSON) bool {
		return det.Delivery.Status == "blocked"
	}, "缺绑定的新交付应 blocked（全局遗留行不得兜底）")
	var stageFailed string
	for _, e := range det.Timeline {
		if e.EventType == "stage_failed" {
			stageFailed = string(e.Payload)
		}
	}
	require.Contains(t, stageFailed, "test_gen", "stage_failed 应写明缺 test_gen")
}

// --- E2E 辅助 ---

func createAgentE2E(t *testing.T, c *http.Client, base, name, script string) string {
	t.Helper()
	cfg := map[string]any{"command": []string{"sh", script}}
	raw, err := json.Marshal(map[string]any{"name": name, "runner": "cli", "config": cfg})
	require.NoError(t, err)
	r, err := c.Post(base+"/api/agents", "application/json", bytes.NewReader(raw))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusCreated, r.StatusCode)
	var a struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&a))
	require.NotEmpty(t, a.ID)
	return a.ID
}

func putProjectBindings(t *testing.T, c *http.Client, base, projectID string, b map[string]string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"bindings": b})
	require.NoError(t, err)
	req, _ := http.NewRequest(http.MethodPut, base+"/api/projects/"+projectID+"/pipeline", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, 200, resp.StatusCode)
}

// artifactContent 取 detail 里指定 kind 的最新产物内容。
func artifactContent(det detailJSON, kind string) string {
	var content string
	for _, a := range det.Artifacts {
		if a.Kind == kind {
			content = a.Content
		}
	}
	return content
}
