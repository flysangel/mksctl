package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flysangel/mksctl/internal/recipe"
)

// countingServer 記錄每個路徑被打了幾次，用來驗證 Latest 的快取。
type countingServer struct {
	*httptest.Server
	mu    sync.Mutex
	count map[string]int
}

func (s *countingServer) hits(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count[path]
}

func newCountingServer(t *testing.T, h http.HandlerFunc) *countingServer {
	t.Helper()
	s := &countingServer{count: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.count[r.URL.Path]++
		s.mu.Unlock()
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func redirectServer(t *testing.T, from, to string) *countingServer {
	t.Helper()
	return newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == from {
			http.Redirect(w, r, to, http.StatusFound)
			return
		}
		// 轉址目標不該被打到：Latest 不跟隨轉址。
		w.WriteHeader(http.StatusTeapot)
	})
}

func TestLatestGitHubReadsRedirect(t *testing.T) {
	srv := redirectServer(t, "/stackrox/kube-linter/releases/latest", "/stackrox/kube-linter/releases/tag/v0.8.3")
	c := &Client{HTTP: srv.Client(), GitHubBase: srv.URL}
	v, err := c.Latest(context.Background(), recipe.LatestSpec{GitHub: "stackrox/kube-linter"})
	require.NoError(t, err)
	assert.Equal(t, "v0.8.3", v)
	v, err = c.Latest(context.Background(), recipe.LatestSpec{GitHub: "stackrox/kube-linter"})
	require.NoError(t, err)
	assert.Equal(t, "v0.8.3", v)
	assert.Equal(t, 1, srv.hits("/stackrox/kube-linter/releases/latest"))
	assert.Equal(t, 0, srv.hits("/stackrox/kube-linter/releases/tag/v0.8.3"))
}

func TestLatestGitHubNoRedirectFails(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := &Client{HTTP: srv.Client(), GitHubBase: srv.URL}
	_, err := c.Latest(context.Background(), recipe.LatestSpec{GitHub: "a/b"})
	require.Error(t, err)
}

func TestLatestURLTrimsBody(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("  v3.19.0\n"))
	})
	c := &Client{HTTP: srv.Client()}
	for range 2 {
		v, err := c.Latest(context.Background(), recipe.LatestSpec{URL: srv.URL + "/stable.txt"})
		require.NoError(t, err)
		assert.Equal(t, "v3.19.0", v)
	}
	assert.Equal(t, 1, srv.hits("/stable.txt"))
}

func TestLatestConcurrentQueriesOnce(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3"))
	})
	c := &Client{HTTP: srv.Client()}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			v, err := c.Latest(context.Background(), recipe.LatestSpec{URL: srv.URL + "/v"})
			assert.NoError(t, err)
			assert.Equal(t, "1.2.3", v)
		})
	}
	wg.Wait()
	assert.Equal(t, 1, srv.hits("/v"))
}

func TestEffectiveUsesReplacement(t *testing.T) {
	assert.Equal(t, "3.16.4", Effective("3.17.0", map[string]string{"3.17.0": "3.16.4"}))
	assert.Equal(t, "3.18.0", Effective("3.18.0", map[string]string{"3.17.0": "3.16.4"}))
	assert.Equal(t, "3.18.0", Effective("3.18.0", nil))
}
