package api

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

// wsServer 建带一个真实 delivery 的服务（/ws 的存在性校验需要真行）。
func wsServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	ctx := context.Background()
	proj := &store.Project{Name: "demo", RepoURL: "https://github.com/x/y", DefaultBranch: "main"}
	require.NoError(t, st.CreateProject(ctx, proj))
	d := &store.Delivery{ProjectID: proj.ID, Title: "x", Status: "active", CurrentStage: "spec"}
	require.NoError(t, st.CreateDelivery(ctx, d))
	return ts, srv, d.ID
}

// wsCookie 从登录 client 的 jar 取 session cookie 头（websocket 握手需显式携带）。
func wsCookie(t *testing.T, base string) string {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	c := &http.Client{Jar: jar}
	r, err := c.Post(base+"/api/login", "application/json",
		strings.NewReader(`{"password":"secret-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	u, err := url.Parse(base)
	require.NoError(t, err)
	for _, ck := range jar.Cookies(u) {
		if ck.Name == "infera_session" {
			return "infera_session=" + ck.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

// dialWS 以给定 cookie 与 Origin 拨 /ws?delivery=<id>。
func dialWS(t *testing.T, tsURL, cookie, delivery, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if cookie != "" {
		header.Set("Cookie", cookie)
	}
	if origin != "" {
		header.Set("Origin", origin)
	}
	u := "ws" + tsURL[4:] + "/ws?delivery=" + delivery
	return websocket.DefaultDialer.Dial(u, header)
}

// TestWSRequiresAuth：/ws 必须在认证组——未登录不得升级、不得订阅事件流。
func TestWSRequiresAuth(t *testing.T) {
	ts, _, deliveryID := wsServer(t)
	_, resp, err := dialWS(t, ts.URL, "", deliveryID, "")
	require.Error(t, err, "未登录必须握手失败")
	require.NotNil(t, resp)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestWSUnknownDelivery404：delivery 不存在 → 404（不得让订阅任意 id）。
func TestWSUnknownDelivery404(t *testing.T) {
	ts, _, _ := wsServer(t)
	ck := wsCookie(t, ts.URL)
	c, resp, err := dialWS(t, ts.URL, ck, "00000000-0000-0000-0000-000000000000", "")
	require.Error(t, err)
	require.Nil(t, c)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// TestWSRejectsForeignOrigin：跨站 Origin 拒绝（CSRF-adjacent：浏览器被第三方
// 页面驱动连 /ws）。非浏览器客户端（无 Origin）放行——同 gorilla 默认语义。
func TestWSRejectsForeignOrigin(t *testing.T) {
	ts, _, deliveryID := wsServer(t)
	ck := wsCookie(t, ts.URL)
	c, resp, err := dialWS(t, ts.URL, ck, deliveryID, "http://evil.example")
	require.Error(t, err)
	require.Nil(t, c)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	// 同源 Origin（含端口）必须放行——不能把正常前端拒之门外。
	same, _, err := dialWS(t, ts.URL, ck, deliveryID, ts.URL)
	require.NoError(t, err)
	_ = same.Close()
}

// TestWSConcurrentPublishSafe：Publish 对同一 conn 的并发写必须串行化
// （gorilla 不允许并发 WriteMessage——会 panic "concurrent call to
// WriteMessage"/损坏帧）。多 goroutine 高频广播，订阅端必须原样收全。
func TestWSConcurrentPublishSafe(t *testing.T) {
	ts, srv, deliveryID := wsServer(t)
	ck := wsCookie(t, ts.URL)
	c, _, err := dialWS(t, ts.URL, ck, deliveryID, "")
	require.NoError(t, err)
	defer c.Close()
	time.Sleep(50 * time.Millisecond) // 等订阅生效

	const workers, perWorker = 8, 25
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				srv.Publish(deliveryID, "spec", "stage_started")
			}
		}()
	}
	wg.Wait()

	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	got := 0
	for got < workers*perWorker {
		var msg map[string]string
		require.NoError(t, c.ReadJSON(&msg), "并发写损坏连接（已收 %d 条）", got)
		require.Equal(t, "spec", msg["stage"])
		require.Equal(t, "stage_started", msg["event"])
		got++
	}
	require.Equal(t, workers*perWorker, got)
}
