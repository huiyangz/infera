package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tokfinity/infera/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Memory) {
	st := store.NewMemory()
	srv := NewServer(st, "secret-pass", nil)
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts, st
}

// login 返回带 cookie jar 的 client（模拟浏览器存 session cookie）。
// 注意：http.Client{} 无 jar 时会丢弃 Set-Cookie，cookie 会话无法通过。
func login(t *testing.T, base string) *http.Client {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar}
	r, err := client.Post(base+"/api/login", "application/json",
		bytes.NewBufferString(`{"password":"secret-pass"}`))
	require.NoError(t, err)
	require.Equal(t, 200, r.StatusCode)
	return client
}

func TestAuthGate(t *testing.T) {
	ts, _ := newServer(t)
	r, _ := http.Get(ts.URL + "/api/projects")
	require.Equal(t, 401, r.StatusCode)

	r, _ = http.Post(ts.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"wrong"}`))
	require.Equal(t, 401, r.StatusCode)

	c := login(t, ts.URL)
	r, _ = c.Get(ts.URL + "/api/projects")
	require.Equal(t, 200, r.StatusCode)

	var me struct {
		LoggedIn bool `json:"logged_in"`
	}
	r, _ = c.Get(ts.URL + "/api/me")
	require.NoError(t, json.NewDecoder(r.Body).Decode(&me))
	require.True(t, me.LoggedIn)
}

func TestProjectsPinnedAndStats(t *testing.T) {
	ts, st := newServer(t)
	c := login(t, ts.URL)
	ctx := context.Background()

	r, _ := c.Post(ts.URL+"/api/projects", "application/json",
		bytes.NewBufferString(`{"name":"demo","repo_url":"","default_branch":"main"}`))
	require.Equal(t, 200, r.StatusCode)
	var p store.Project
	require.NoError(t, json.NewDecoder(r.Body).Decode(&p))

	req, _ := http.NewRequest("PATCH", ts.URL+"/api/projects/"+p.ID, bytes.NewBufferString(`{"pinned":true}`))
	req.Header.Set("Content-Type", "application/json")
	r, _ = c.Do(req)
	require.Equal(t, 200, r.StatusCode)
	var patched store.Project
	require.NoError(t, json.NewDecoder(r.Body).Decode(&patched))
	require.True(t, patched.Pinned)

	require.NoError(t, st.CreateDelivery(ctx, &store.Delivery{ProjectID: p.ID, Title: "x", Status: "active", PendingGate: "spec_approval"}))

	r, _ = c.Get(ts.URL + "/api/projects?include=stats")
	var list []struct {
		store.Project
		Stats *store.ProjectStats `json:"stats"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&list))
	require.Len(t, list, 1)
	require.True(t, list[0].Pinned)
	require.NotNil(t, list[0].Stats)
	require.Equal(t, 1, list[0].Stats.Active)
	require.Equal(t, 1, list[0].Stats.Pending)

	// 404
	r, _ = c.Get(ts.URL + "/api/projects/00000000-0000-0000-0000-000000000000")
	require.Equal(t, 404, r.StatusCode)
}
