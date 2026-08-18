package api

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestWSWriteDeadlineDisconnectsStalledPeer：对端停读（标签页挂起/网络半断）
// 后内核缓冲写满，无写超时的 WriteMessage 会永久阻塞——Publish 是引擎的
// 同步通知路径，一条僵死连接就能反压引擎。写必须带超时并在失败后断开
// （读循环随后退出 → unsubscribe 清理）。
func TestWSWriteDeadlineDisconnectsStalledPeer(t *testing.T) {
	orig := wsWriteWait
	wsWriteWait = 150 * time.Millisecond
	t.Cleanup(func() { wsWriteWait = orig })

	ts, srv, deliveryID := wsServer(t)
	ck := wsCookie(t, ts.URL)
	c, _, err := dialWS(t, ts.URL, ck, deliveryID, "")
	require.NoError(t, err)
	defer c.Close()
	time.Sleep(50 * time.Millisecond) // 等订阅生效

	// 收缩两端 TCP 缓冲：对端停读时几个 64KB 帧内必然写满（不依赖大流量）。
	require.NoError(t, c.NetConn().(*net.TCPConn).SetReadBuffer(4096))
	srv.hub.mu.Lock()
	var wc *wsClient
	for k := range srv.hub.subs[deliveryID] {
		wc = k
	}
	srv.hub.mu.Unlock()
	require.NotNil(t, wc, "订阅表里应有该连接")
	require.NoError(t, wc.conn.NetConn().(*net.TCPConn).SetWriteBuffer(4096))

	big := strings.Repeat("x", 64<<10)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 64; i++ { // 4MB 上限，远超收缩后的内核缓冲
			if err := wc.write(websocket.TextMessage, []byte(big)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case err := <-done:
		require.Error(t, err, "对端停读后写应因超时失败（而非全部成功）")
	case <-time.After(5 * time.Second):
		t.Fatal("写对端停读的连接永久阻塞（缺写超时）")
	}

	// 写失败后连接必须关闭并从订阅表清理（读循环退出 → unsubscribe）。
	require.Eventually(t, func() bool {
		srv.hub.mu.Lock()
		defer srv.hub.mu.Unlock()
		return len(srv.hub.subs[deliveryID]) == 0
	}, 3*time.Second, 20*time.Millisecond, "写失败后应断开并退订僵死连接")
}
