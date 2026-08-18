package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/mcp"
	"github.com/tokfinity/infera/internal/store"
)

// raceEngine 引擎替身：记录「同时进入引擎」的峰值。引擎自身无并发保护，
// 两条驾驶面（api 后台 driver / MCP 簿记）并发进入即 peak>1——对应生产里的
// 双写 UpdateDelivery / 双 advance / 事件乱序。替身不改 store：delivery 恒为
// active 无门禁，driver 每轮固定消耗 maxStarts 次 Continue，调用量可精确等待。
type raceEngine struct {
	fakeEngine
	wait time.Duration

	mu     sync.Mutex
	inside int
	peak   int
	total  int
}

func (e *raceEngine) track() func() {
	e.mu.Lock()
	e.inside++
	if e.inside > e.peak {
		e.peak = e.inside
	}
	e.mu.Unlock()
	time.Sleep(e.wait)
	return func() {
		e.mu.Lock()
		e.inside--
		e.total++
		e.mu.Unlock()
	}
}

func (e *raceEngine) Start(context.Context, string) error { e.track()(); return nil }
func (e *raceEngine) Continue(context.Context, string) error {
	e.track()()
	return nil
}
func (e *raceEngine) Approve(_ context.Context, id string, _ store.ApproveOpts) ([]store.Delivery, error) {
	e.track()()
	return nil, nil
}
func (e *raceEngine) Reject(context.Context, string, string) error { e.track()(); return nil }
func (e *raceEngine) SubmitLocal(context.Context, string, string) error {
	e.track()()
	return nil
}
func (e *raceEngine) LocalPrompt(context.Context, string) (string, string, error) {
	return "", "", nil
}

func (e *raceEngine) stats() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.peak, e.total
}

// TestMCPAndAPIShareDeliveryLock：MCP 簿记（approve_gate 等）与 api 后台
// driver 必须经同一份 per-delivery 锁串行化进引擎——两个 Server 各自的
// sync.Map 是两把独立锁，MCP 簿记可与 api driver 并发进入无并发保护的引擎。
// 装配复刻 main（同一 engine 接两面 + RunDelivery 注入 SetDrive），
// 两路并发打引擎替身，「同时在引擎内」的峰值必须恒为 1。
func TestMCPAndAPIShareDeliveryLock(t *testing.T) {
	st := store.NewMemory()
	ctx := context.Background()
	proj := &store.Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, proj))
	d := &store.Delivery{
		ProjectID: proj.ID, Title: "x", Status: "active",
		CurrentStage: "spec", WorkspaceReady: true,
	}
	require.NoError(t, st.CreateDelivery(ctx, d))

	eng := &raceEngine{wait: 2 * time.Millisecond}
	srv := NewServer(st, "secret-pass", nil)
	srv.SetEngine(eng)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	c := login(t, ts.URL)

	mcpSrv := mcp.New(st, eng, func(string) string { return "/tmp/infera-workdirs" }, "tok")
	mcpSrv.SetDrive(srv.RunDelivery)
	mcpSrv.SetLocks(srv.DeliveryLocks()) // 与 main 同款：两面共享 per-delivery 锁
	mts := httptest.NewServer(mcpSrv.Handler())
	t.Cleanup(mts.Close)

	// MCP tools/call approve_gate 请求（真 HTTP 面：走 act 的锁纪律）。
	mcpApprove := func(round int) {
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": round, "method": "tools/call",
			"params": map[string]any{"name": "approve_gate", "arguments": map[string]any{"delivery_id": d.ID}},
		})
		if err != nil {
			t.Error(err)
			return
		}
		req, err := http.NewRequest("POST", mts.URL, strings.NewReader(string(body)))
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer tok")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Error(err)
			return
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusOK {
			t.Errorf("mcp approve_gate: status %d", r.StatusCode)
		}
	}

	// 每轮期望的引擎调用：api approve = 1 Approve + 6 Continue（driveLocked
	// 上限），MCP approve_gate 同理；fake 不改 store，轮次间精确递增。
	const rounds = 6
	const perRound = 2 * (1 + maxStarts)
	for i := 0; i < rounds; i++ {
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			r, err := c.Post(ts.URL+"/api/deliveries/"+d.ID+"/approve", "application/json", strings.NewReader(`{}`))
			if err != nil {
				t.Error(err)
				return
			}
			_ = r.Body.Close()
		}()
		go func(round int) {
			defer wg.Done()
			<-start
			mcpApprove(round)
		}(i)
		close(start) // 两路同时起跑，制造最大重叠窗口
		wg.Wait()
		// 等两路的后台驱动 goroutine 全部跑完（调用量到齐）再进下一轮。
		want := (i + 1) * perRound
		require.Eventually(t, func() bool {
			_, total := eng.stats()
			return total == want
		}, 5*time.Second, 10*time.Millisecond, "round %d: 引擎调用应收敛到 %d", i, want)
	}

	peak, total := eng.stats()
	require.Equal(t, rounds*perRound, total, "引擎调用总数应确定性收敛")
	require.Equal(t, 1, peak, "MCP 簿记与 api driver 必须互斥进引擎（检测到 %d 路并发）", peak)
}
