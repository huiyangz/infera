package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWSPubSub(t *testing.T) {
	ts, srv, deliveryID := wsServer(t)
	ck := wsCookie(t, ts.URL)

	c, _, err := dialWS(t, ts.URL, ck, deliveryID, "")
	require.NoError(t, err)
	defer c.Close()
	time.Sleep(50 * time.Millisecond) // 等订阅生效

	srv.Publish(deliveryID, "spec", "stage_started")
	var msg map[string]string
	require.NoError(t, c.SetReadDeadline(time.Now().Add(2*time.Second)))
	require.NoError(t, c.ReadJSON(&msg))
	require.Equal(t, "spec", msg["stage"])
	require.Equal(t, "stage_started", msg["event"])
}

// 无 delivery 参数 → 升级前即被拒：握手失败且 HTTP 响应为 400（需先过认证）。
func TestWSRequiresDeliveryParam(t *testing.T) {
	ts, _, _ := wsServer(t)
	ck := wsCookie(t, ts.URL)

	_, resp, err := dialWS(t, ts.URL, ck, "", "")
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 400, resp.StatusCode)
}
