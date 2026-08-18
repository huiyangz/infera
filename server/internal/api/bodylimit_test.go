package api

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDecodeRejectsOversizedBody：请求体超过 1MiB 上限必须 400 拒绝，
// 不得整包读入内存再解析（内存放大防护）。用「合法 JSON 前垫 2MiB 空白」
// 构造——没有上限时会解码成功甚至登录成功（200）。
func TestDecodeRejectsOversizedBody(t *testing.T) {
	ts, _ := newServer(t)

	body := strings.Repeat(" ", 2<<20) + `{"password":"secret-pass"}`
	r, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader([]byte(body)))
	require.NoError(t, err)
	defer r.Body.Close()
	require.Equal(t, http.StatusBadRequest, r.StatusCode, "超限 body 必须在解析前被拒")

	// 对照：1MiB 内的合法请求不受影响（错误密码 → 401，说明已进入正常解码）。
	r2, err := http.Post(ts.URL+"/api/login", "application/json",
		strings.NewReader(`{"password":"wrong"}`))
	require.NoError(t, err)
	defer r2.Body.Close()
	require.Equal(t, http.StatusUnauthorized, r2.StatusCode)

	// 边界：恰好在上限之内（<1MiB）的 body 正常处理。
	bigButOk := strings.Repeat(" ", (1<<20)-64) + `{"password":"wrong"}`
	r3, err := http.Post(ts.URL+"/api/login", "application/json", strings.NewReader(bigButOk))
	require.NoError(t, err)
	defer r3.Body.Close()
	require.Equal(t, http.StatusUnauthorized, r3.StatusCode, "未超限的 body 照常解析")
}
