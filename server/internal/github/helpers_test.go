package github

import (
	"io"
	"net/http"
	"strings"
)

// stubTransport 截获请求不触网（离线单测离线性的保证之一）。
type stubTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return s.roundTrip(r)
}

// jsonResp 构造一个 JSON HTTP 响应（配合 stubTransport 使用）。
func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
