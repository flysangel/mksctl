package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flysangel/mksctl/internal/upstream"
)

const newBinary = "new mksctl binary"

// releaseServer 模擬 GitHub 的 release 下載與 attestation API，並記下收到的請求。
type releaseServer struct {
	*httptest.Server
	mu        sync.Mutex
	paths     []string
	authz     map[string]string
	checksums string // 空字串時用 newBinary 的正確雜湊
	latest    string // 空字串時最新版是 v0.2.0
}

func (s *releaseServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newReleaseServer(t *testing.T) *releaseServer {
	t.Helper()
	s := &releaseServer{authz: map[string]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.authz[r.URL.Path] = r.Header.Get("Authorization")
		checksums, latest := s.checksums, s.latest
		s.mu.Unlock()
		if latest == "" {
			latest = "v0.2.0"
		}
		if checksums == "" {
			checksums = sum(newBinary) + "  mksctl-linux-amd64\n"
		}
		p := r.URL.Path
		switch {
		case p == "/flysangel/mksctl/releases/latest":
			http.Redirect(w, r, "/flysangel/mksctl/releases/tag/"+latest, http.StatusFound)
		case strings.HasSuffix(p, "/mksctl-linux-amd64"):
			fmt.Fprint(w, newBinary)
		case strings.HasSuffix(p, "/checksums.txt"):
			fmt.Fprint(w, checksums)
		case p == "/repos/flysangel/mksctl/attestations/sha256:"+sum(newBinary):
			fmt.Fprint(w, `{"attestations":[{"bundle":{"mediaType":"fake"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// fakeVerifier 不碰真的 sigstore：真的驗證路徑由 internal/fetch 的測試涵蓋。
type fakeVerifier struct {
	err    error
	calls  int
	bundle string
	digest []byte
}

func (f *fakeVerifier) Verify(_ context.Context, bundleJSON, digest []byte) error {
	f.calls++
	f.bundle, f.digest = string(bundleJSON), digest
	return f.err
}

func setup(t *testing.T, v Verifier) (*releaseServer, *Updater, string) {
	t.Helper()
	srv := newReleaseServer(t)
	dest := filepath.Join(t.TempDir(), "mksctl")
	require.NoError(t, os.WriteFile(dest, []byte("old"), 0o755))
	u := &Updater{
		Client:   &upstream.Client{GitHubBase: srv.URL, GitHubAPIBase: srv.URL, Token: "tkn"},
		Verifier: v,
	}
	return srv, u, dest
}

func assertUnchanged(t *testing.T, dest string) {
	t.Helper()
	b, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, "old", string(b))
	entries, err := os.ReadDir(filepath.Dir(dest))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "暫存檔要清掉")
}

func TestUpdateRejectsBadAttestation(t *testing.T) {
	v := &fakeVerifier{err: errors.New("簽章不符")}
	_, u, dest := setup(t, v)
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "簽章不符")
	assert.Equal(t, 1, v.calls)
	assertUnchanged(t, dest)
}

func TestUpdateDefaultIsLatest(t *testing.T) {
	v := &fakeVerifier{}
	srv, u, dest := setup(t, v)
	res, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.NoError(t, err)
	assert.Equal(t, Result{From: "v0.1.0", To: "v0.2.0"}, res)
	// 從查到的那一版下載，回報的版本才等於裝上去的版本。
	assert.Contains(t, srv.seen(), "/flysangel/mksctl/releases/download/v0.2.0/mksctl-linux-amd64")
	assert.Contains(t, srv.seen(), "/flysangel/mksctl/releases/download/v0.2.0/checksums.txt")
	assert.NotContains(t, srv.seen(), "/flysangel/mksctl/releases/latest/download/mksctl-linux-amd64")

	b, err := os.ReadFile(dest)
	require.NoError(t, err)
	assert.Equal(t, newBinary, string(b))
	fi, err := os.Stat(dest)
	require.NoError(t, err)
	if runtime.GOOS != "windows" { // Windows 沒有 Unix 權限位元，Perm() 只分唯讀與可寫（0444／0666）。
		assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm())
	}

	assert.Equal(t, sum(newBinary), hex.EncodeToString(v.digest))
	assert.JSONEq(t, `{"mediaType":"fake"}`, v.bundle)
}

func TestUpdateTagDownloadsThatTag(t *testing.T) {
	srv, u, dest := setup(t, &fakeVerifier{})
	res, err := u.Update(context.Background(), "v0.0.1", "v0.2.0", dest)
	require.NoError(t, err)
	assert.Equal(t, Result{From: "v0.2.0", To: "v0.0.1"}, res)
	assert.Contains(t, srv.seen(), "/flysangel/mksctl/releases/download/v0.0.1/mksctl-linux-amd64")
	assert.NotContains(t, srv.seen(), "/flysangel/mksctl/releases/latest")
}

func TestUpdateRejectsBadTag(t *testing.T) {
	srv, u, dest := setup(t, &fakeVerifier{})
	_, err := u.Update(context.Background(), "../v1", "v0.1.0", dest)
	require.Error(t, err)
	assert.Empty(t, srv.seen())
	assertUnchanged(t, dest)
}

func TestUpdateRejectsBadLatestTag(t *testing.T) {
	srv, u, dest := setup(t, &fakeVerifier{})
	srv.latest = "v0.3.0-rc1"
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.ErrorContains(t, err, "v0.3.0-rc1")
	assert.Equal(t, []string{"/flysangel/mksctl/releases/latest"}, srv.seen())
	assertUnchanged(t, dest)
}

func TestUpdateAlreadyLatest(t *testing.T) {
	v := &fakeVerifier{}
	srv, u, dest := setup(t, v)
	res, err := u.Update(context.Background(), "", "v0.2.0", dest)
	require.NoError(t, err)
	assert.Equal(t, Result{From: "v0.2.0", To: "v0.2.0", UpToDate: true}, res)
	assert.Equal(t, []string{"/flysangel/mksctl/releases/latest"}, srv.seen())
	assertUnchanged(t, dest)
}

func TestUpdateChecksumMismatch(t *testing.T) {
	v := &fakeVerifier{}
	srv, u, dest := setup(t, v)
	srv.checksums = sum("tampered") + "  mksctl-linux-amd64\n"
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "雜湊不符")
	assert.Zero(t, v.calls)
	assertUnchanged(t, dest)
}

func TestUpdateRequiresChecksumLine(t *testing.T) {
	v := &fakeVerifier{}
	srv, u, dest := setup(t, v)
	srv.checksums = sum(newBinary) + "  other-file\n"
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "沒有 mksctl-linux-amd64")
	assert.Zero(t, v.calls)
	assertUnchanged(t, dest)
}

func TestUpdateSendsTokenToAPI(t *testing.T) {
	srv, u, dest := setup(t, &fakeVerifier{})
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.NoError(t, err)
	srv.mu.Lock()
	defer srv.mu.Unlock()
	assert.Equal(t, "Bearer tkn", srv.authz["/repos/flysangel/mksctl/attestations/sha256:"+sum(newBinary)])
}

func TestUpdateNoAttestation(t *testing.T) {
	v := &fakeVerifier{}
	srv, u, dest := setup(t, v)
	// 雜湊對得上 checksums.txt，但 API 查不到這個 digest 的 attestation。
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/mksctl-linux-amd64"):
			fmt.Fprint(w, newBinary)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprint(w, sum(newBinary)+"  mksctl-linux-amd64\n")
		case r.URL.Path == "/flysangel/mksctl/releases/latest":
			http.Redirect(w, r, "/flysangel/mksctl/releases/tag/v0.2.0", http.StatusFound)
		default:
			fmt.Fprint(w, `{"attestations":[]}`)
		}
	})
	_, err := u.Update(context.Background(), "", "v0.1.0", dest)
	require.Error(t, err)
	assert.Zero(t, v.calls)
	assertUnchanged(t, dest)
}
