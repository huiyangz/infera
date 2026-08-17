package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPRunnerRoundtrip(t *testing.T) {
	var gotBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, jsonDecodeBody(r, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":"远端产物"}`))
	}))
	defer ts.Close()

	res, err := NewHTTP(ts.URL).Run(context.Background(), Request{Role: "spec", Prompt: "写规格", Workdir: "/w"})
	require.NoError(t, err)
	require.Equal(t, "远端产物", res.Output)
	require.Equal(t, "spec", gotBody["role"])
	require.Equal(t, "写规格", gotBody["prompt"])
	require.Equal(t, "/w", gotBody["workdir"])
}

func TestHTTPRunnerNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom-boom-boom "+strings.Repeat("x", 1000), http.StatusBadGateway)
	}))
	defer ts.Close()

	_, err := NewHTTP(ts.URL).Run(context.Background(), Request{Role: "spec"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
	// 响应体回显被截断到 512 字节
	require.Contains(t, err.Error(), "boom-boom-boom")
	require.Less(t, len(err.Error()), 600)
}

func TestHTTPRunnerBadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer ts.Close()

	_, err := NewHTTP(ts.URL).Run(context.Background(), Request{Role: "spec"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "解析响应失败")
}

func jsonDecodeBody(r *http.Request, v *map[string]string) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
