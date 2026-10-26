package selfupdate

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flysangel/mksctl/internal/upstream"
	"github.com/stretchr/testify/assert"
)

func latestServer(t *testing.T, tag string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/flysangel/mksctl/releases/latest" {
			http.Redirect(w, r, "/flysangel/mksctl/releases/tag/"+tag, http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNotifyOfflineIsSilent(t *testing.T) {
	// 伺服器收到請求後不回應，直到用戶端放棄。
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	var errOut bytes.Buffer
	start := time.Now()
	Notify(context.Background(), "v0.1.0", &upstream.Client{GitHubBase: srv.URL}, &errOut)
	assert.Less(t, time.Since(start), 3500*time.Millisecond)
	assert.Empty(t, errOut.String())
}

func TestNotifyRefusedIsSilent(t *testing.T) {
	var errOut bytes.Buffer
	Notify(context.Background(), "v0.1.0", &upstream.Client{GitHubBase: "http://127.0.0.1:1"}, &errOut)
	assert.Empty(t, errOut.String())
}

func TestNotifyPrintsInstallLine(t *testing.T) {
	srv := latestServer(t, "v0.2.0")
	var errOut bytes.Buffer
	Notify(context.Background(), "v0.1.0", &upstream.Client{GitHubBase: srv.URL}, &errOut)
	assert.Equal(t, "Info: 有新版 v0.2.0，執行 curl -fsSL https://github.com/flysangel/mksctl/releases/latest/download/install.sh | sudo bash\n", errOut.String())
}

func TestNotifySilentWhenNotNewer(t *testing.T) {
	for _, tc := range []struct{ current, latest string }{
		{"v0.2.0", "v0.2.0"},
		{"v0.3.0", "v0.2.0"},
		{"v0.10.0", "v0.9.9"},
		{"v0.1.0", "nightly"}, // 看不懂的版本不提示，免得叫人降版。
	} {
		srv := latestServer(t, tc.latest)
		var errOut bytes.Buffer
		Notify(context.Background(), tc.current, &upstream.Client{GitHubBase: srv.URL}, &errOut)
		assert.Empty(t, errOut.String(), tc)
	}
}

func TestNewer(t *testing.T) {
	assert.True(t, newer("v0.10.0", "v0.9.0"))
	assert.True(t, newer("v1.0.0", "v0.99.99"))
	assert.False(t, newer("v1.0.0", "v1.0.0"))
	assert.False(t, newer("v1.0", "v0.1.0"))
	assert.False(t, newer("v1.0.0-rc1", "v0.1.0"))
	assert.False(t, newer("v+1.0.0", "v0.1.0"))
}
