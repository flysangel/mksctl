package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/upstream"
)

// 測試檔不能寫死 64 位雜湊（TestNoSha256OutsideTrust 會擋），一律執行時算。
func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

var (
	payload = []byte("helm-payload")
	other   = []byte("signature-bytes")
)

// fileServer 依路徑回固定內容；值為 nil 時回 404。
func fileServer(t *testing.T, files map[string][]byte, check func(*http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		b, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newFetcher(t *testing.T, srv *httptest.Server) *Fetcher {
	t.Helper()
	return &Fetcher{
		Upstream: &upstream.Client{HTTP: srv.Client(), GitHubBase: srv.URL, GitHubAPIBase: srv.URL},
		Dir:      t.TempDir(),
	}
}

func dirEmpty(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, ents)
}

func TestGetChecksumFileAnchored(t *testing.T) {
	// .asc 那行排前面：沒錨定的比對會先撞到它而誤判。
	sums := fmt.Sprintf("%s  helm.tar.gz.asc\n%s  helm.tar.gz\n", hexSum(other), hexSum(payload))
	srv := fileServer(t, map[string][]byte{
		"/v3.19.0/helm.tar.gz":     payload,
		"/v3.19.0/helm.tar.gz.asc": other,
		"/v3.19.0/checksums.txt":   []byte(sums),
	}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{
		ID:       "helm",
		URL:      srv.URL + "/{{.Version}}/helm.tar.gz",
		Checksum: recipe.ChecksumSpec{File: srv.URL + "/{{.Version}}/checksums.txt"},
	}
	res, err := f.Get(context.Background(), a, "v3.19.0")
	require.NoError(t, err)
	assert.Equal(t, "helm", res.ID)
	assert.Equal(t, "v3.19.0", res.Version)
	assert.Equal(t, hexSum(payload), res.SHA256)
	b, err := os.ReadFile(res.Path)
	require.NoError(t, err)
	assert.Equal(t, payload, b)
	assert.Equal(t, f.Dir, filepath.Dir(res.Path))
}

func TestGetChecksumFileFormats(t *testing.T) {
	for name, body := range map[string]string{
		"binary": fmt.Sprintf("%s *helm.tar.gz\n", hexSum(payload)),
		"only":   hexSum(payload) + "\n",
		"upper":  strings.ToUpper(hexSum(payload)) + "  helm.tar.gz\n",
	} {
		t.Run(name, func(t *testing.T) {
			srv := fileServer(t, map[string][]byte{
				"/helm.tar.gz":           payload,
				"/helm.tar.gz.sha256sum": []byte(body),
			}, nil)
			f := newFetcher(t, srv)
			a := recipe.Artifact{ID: "helm", URL: srv.URL + "/helm.tar.gz",
				Checksum: recipe.ChecksumSpec{File: "{{.URL}}.sha256sum"}}
			_, err := f.Get(context.Background(), a, "v1")
			require.NoError(t, err)
		})
	}
}

func TestGetChecksumMismatchDeletesFile(t *testing.T) {
	sums := fmt.Sprintf("%s  helm.tar.gz\n", hexSum(other))
	srv := fileServer(t, map[string][]byte{
		"/helm.tar.gz":   payload,
		"/checksums.txt": []byte(sums),
	}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "helm", URL: srv.URL + "/helm.tar.gz",
		Checksum: recipe.ChecksumSpec{File: srv.URL + "/checksums.txt"}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrChecksumMismatch)
	dirEmpty(t, f.Dir)
}

func TestGetChecksumFileMissingEntry(t *testing.T) {
	srv := fileServer(t, map[string][]byte{
		"/helm.tar.gz":   payload,
		"/checksums.txt": []byte(fmt.Sprintf("%s  helm.tar.gz.asc\n", hexSum(payload))),
	}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "helm", URL: srv.URL + "/helm.tar.gz",
		Checksum: recipe.ChecksumSpec{File: srv.URL + "/checksums.txt"}}
	_, err := f.Get(context.Background(), a, "v1")
	require.Error(t, err)
	dirEmpty(t, f.Dir)
}

func TestGetTrustMismatch(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	f.Trust = TrustDB{"tool": {URL: srv.URL + "/tool", Size: int64(len(payload)), SHA256: hexSum(other)}}
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{Trust: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrTrustMismatch)
	dirEmpty(t, f.Dir)
}

func TestGetTrustMatch(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	db, err := LoadTrust([]byte(fmt.Sprintf("tool:\n  url: %s/tool\n  size: %d\n  sha256: %s\n",
		srv.URL, len(payload), hexSum(payload))))
	require.NoError(t, err)
	f.Trust = db
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{Trust: true}}
	res, err := f.Get(context.Background(), a, "v1")
	require.NoError(t, err)
	assert.Equal(t, hexSum(payload), res.SHA256)
}

func TestGetTrustNoEntryFallsThrough(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{Trust: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrUnverified)
	dirEmpty(t, f.Dir)
}

func releaseJSON(name, digest string) []byte {
	b, _ := json.Marshal(map[string]any{"assets": []map[string]string{
		{"name": name + ".sig", "digest": "sha256:" + hexSum(other)},
		{"name": name, "digest": digest},
	}})
	return b
}

func TestGetGitHubDigestSendsToken(t *testing.T) {
	var auth atomic.Value
	srv := fileServer(t, map[string][]byte{
		"/o/r/releases/download/v1/tool": payload,
		"/repos/o/r/releases/tags/v1":    releaseJSON("tool", "sha256:"+hexSum(payload)),
	}, func(r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			auth.Store(r.Header.Get("Authorization"))
		}
	})
	f := newFetcher(t, srv)
	f.Upstream.Token = "s3cret"
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/o/r/releases/download/{{.Version}}/tool",
		Checksum: recipe.ChecksumSpec{GitHubDigest: true}}
	res, err := f.Get(context.Background(), a, "v1")
	require.NoError(t, err)
	assert.Equal(t, "Bearer s3cret", auth.Load())
	assert.Equal(t, hexSum(payload), res.SHA256)
}

func TestGetGitHubDigestMismatch(t *testing.T) {
	srv := fileServer(t, map[string][]byte{
		"/o/r/releases/download/v1/tool": payload,
		"/repos/o/r/releases/tags/v1":    releaseJSON("tool", "sha256:"+hexSum(other)),
	}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/o/r/releases/download/{{.Version}}/tool",
		Checksum: recipe.ChecksumSpec{GitHubDigest: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrChecksumMismatch)
	dirEmpty(t, f.Dir)
}

func TestGetGitHubDigestNotGitHubURLIsUnverified(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{GitHubDigest: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrUnverified)
}

func TestGetNoSourceIsUnverified(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	_, err := f.Get(context.Background(), recipe.Artifact{ID: "tool", URL: srv.URL + "/tool"}, "v1")
	require.ErrorIs(t, err, ErrUnverified)
	dirEmpty(t, f.Dir)
}

func TestGetAllowUnverifiedOnlyWhenNoSource(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload, "/tool.sha256": []byte(strings.Repeat("0", 64) + "  tool\n")}, nil)
	f := newFetcher(t, srv)
	f.AllowUnverified = true
	res, err := f.Get(context.Background(), recipe.Artifact{ID: "tool", URL: srv.URL + "/tool"}, "v1")
	require.NoError(t, err)
	assert.Equal(t, hexSum(payload), res.SHA256)

	// 有設雜湊來源時，AllowUnverified 不能讓驗證失敗的檔過關。
	_, err = f.Get(context.Background(), recipe.Artifact{ID: "tool", URL: srv.URL + "/tool",
		Checksum: recipe.ChecksumSpec{File: srv.URL + "/tool.sha256"}}, "v1")
	require.ErrorIs(t, err, ErrChecksumMismatch)
}

func TestGetRetriesTransientErrors(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tool" && n.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/tool" {
			_, _ = w.Write(payload)
			return
		}
		_, _ = w.Write([]byte(hexSum(payload)))
	}))
	t.Cleanup(srv.Close)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{File: "{{.URL}}.sha256"}}
	_, err := f.Get(context.Background(), a, "v1")
	require.NoError(t, err)
	assert.Equal(t, int32(3), n.Load())
}

func loadTestRoot(t *testing.T) root.TrustedMaterial {
	t.Helper()
	tr, err := root.NewTrustedRootFromPath("testdata/trusted_root.json")
	require.NoError(t, err)
	return tr
}

// kube-linter 用 cosign 金鑰簽章：bundle 裡只有公鑰提示，沒有憑證。
var kubeLinterSpec = recipe.SigstoreSpec{Key: "trust/keys/kube-linter-cosign.pub"}

// cosign 自己的 release 用 keyless（Fulcio 憑證）簽，SAN 是 Google 服務帳號。
var cosignSpec = recipe.SigstoreSpec{
	Identity: `^keyless@projectsigstore\.iam\.gserviceaccount\.com$`,
	Issuer:   "https://accounts.google.com",
}

func readFixture(t *testing.T, name string) (bundle, digest []byte) {
	t.Helper()
	bundle, err := os.ReadFile("testdata/" + name + ".sigstore.json")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/" + name + ".sha256")
	require.NoError(t, err)
	digest, err = hex.DecodeString(strings.TrimSpace(string(want)))
	require.NoError(t, err)
	return bundle, digest
}

// repoFS 讀 repo 根，測試用的就是真正釘在 trust/keys/ 的公鑰。
func repoFS() fs.FS { return os.DirFS("../..") }

func TestGetSigstoreBundle(t *testing.T) {
	bundle, digest := readFixture(t, "kube-linter-linux")
	f := &Fetcher{TrustedRoot: loadTestRoot(t), TrustFS: repoFS()}
	require.NoError(t, f.verifySigstore(context.Background(), bundle, digest, kubeLinterSpec))

	digest[0] ^= 0x01
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, kubeLinterSpec), ErrUnverified)
}

func TestGetSigstoreKeyMissing(t *testing.T) {
	bundle, digest := readFixture(t, "kube-linter-linux")
	f := &Fetcher{TrustedRoot: loadTestRoot(t), TrustFS: repoFS()}
	spec := recipe.SigstoreSpec{Key: "trust/keys/nope.pub"}
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, spec), ErrUnverified)
}

func TestGetSigstoreKeyless(t *testing.T) {
	bundle, digest := readFixture(t, "cosign-linux-amd64")
	f := &Fetcher{TrustedRoot: loadTestRoot(t)}
	require.NoError(t, f.verifySigstore(context.Background(), bundle, digest, cosignSpec))

	bad := cosignSpec
	bad.Identity = "^https://github.com/stackrox/kube-linter/"
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, bad), ErrUnverified)

	badIssuer := cosignSpec
	badIssuer.Issuer = "https://token.actions.githubusercontent.com"
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, badIssuer), ErrUnverified)

	digest[0] ^= 0x01
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, cosignSpec), ErrUnverified)
}

// 信任根拿掉 ctlogs 後，keyless 驗不了 SCT 就要失敗：證明 keyless 有要求 SCT，
// 不是只看透明紀錄與時間戳。
func TestGetSigstoreKeylessRequiresSCT(t *testing.T) {
	bundle, digest := readFixture(t, "cosign-linux-amd64")
	raw, err := os.ReadFile("testdata/trusted_root.json")
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.Contains(t, m, "ctlogs")
	delete(m, "ctlogs")
	raw, err = json.Marshal(m)
	require.NoError(t, err)
	tr, err := root.NewTrustedRootFromJSON(raw)
	require.NoError(t, err)
	f := &Fetcher{TrustedRoot: tr}
	require.ErrorIs(t, f.verifySigstore(context.Background(), bundle, digest, cosignSpec), ErrUnverified)
}

func TestGetSigstoreFailureIsHardAndDeletes(t *testing.T) {
	bundle, err := os.ReadFile("testdata/kube-linter-linux.sigstore.json")
	require.NoError(t, err)
	// File 也設了而且對得上：sigstore 失敗不能退回 File。
	srv := fileServer(t, map[string][]byte{
		"/kube-linter-linux":               payload,
		"/kube-linter-linux.sigstore.json": bundle,
		"/kube-linter-linux.sha256":        []byte(hexSum(payload)),
	}, nil)
	f := newFetcher(t, srv)
	f.TrustedRoot = loadTestRoot(t)
	f.TrustFS = repoFS()
	spec := kubeLinterSpec
	spec.Bundle = "{{.URL}}.sigstore.json"
	a := recipe.Artifact{ID: "kube-linter", URL: srv.URL + "/kube-linter-linux",
		Checksum: recipe.ChecksumSpec{Sigstore: &spec, File: "{{.URL}}.sha256"}}
	_, err = f.Get(context.Background(), a, "v0.8.3")
	require.ErrorIs(t, err, ErrUnverified)
	assert.ErrorContains(t, err, "artifact does not match digest")
	dirEmpty(t, f.Dir)
}

func TestErrorsAreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrTrustMismatch, ErrChecksumMismatch))
	assert.False(t, errors.Is(ErrUnverified, ErrChecksumMismatch))
}

// sumProvider 依 (版本, 檔名) 回固定雜湊，模擬 godev 這類在 API 裡公布雜湊的上游。
type sumProvider struct {
	sums map[string]string
	got  []string
}

func (p *sumProvider) Latest(context.Context, *upstream.Client) (string, error) { return "v1", nil }

func (p *sumProvider) Checksum(_ context.Context, _ *upstream.Client, version, filename string) (string, error) {
	p.got = append(p.got, version+"/"+filename)
	s, ok := p.sums[version+"/"+filename]
	if !ok {
		return "", fmt.Errorf("沒有 %s", filename)
	}
	return s, nil
}

func registerProvider(t *testing.T, name string, p upstream.Provider) {
	t.Helper()
	upstream.Providers[name] = p
	t.Cleanup(func() { delete(upstream.Providers, name) })
}

func TestGetProviderChecksum(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/v1/tool.tar.gz": payload}, nil)
	p := &sumProvider{sums: map[string]string{"v1/tool.tar.gz": hexSum(payload)}}
	registerProvider(t, "test-sum", p)
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/{{.Version}}/tool.tar.gz",
		Checksum: recipe.ChecksumSpec{Provider: "test-sum"}}
	res, err := f.Get(context.Background(), a, "v1")
	require.NoError(t, err)
	assert.Equal(t, hexSum(payload), res.SHA256)
	assert.Equal(t, []string{"v1/tool.tar.gz"}, p.got)

	p.sums["v1/tool.tar.gz"] = hexSum(other)
	f = newFetcher(t, srv)
	_, err = f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrChecksumMismatch)
	dirEmpty(t, f.Dir)
}

// provider 查不到雜湊時不往下試 trust 或 GitHub digest：作者指定了來源，查不到就是有問題。
func TestGetProviderMissingIsUnverified(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool.tar.gz": payload}, nil)
	registerProvider(t, "test-sum", &sumProvider{sums: map[string]string{}})
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool.tar.gz",
		Checksum: recipe.ChecksumSpec{Provider: "test-sum", Trust: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrUnverified)
	dirEmpty(t, f.Dir)
}

// provider 排在 file 前面：兩者都設時由 provider 決定，file 不會被下載。
func TestGetProviderBeforeFile(t *testing.T) {
	var fileHit atomic.Bool
	srv := fileServer(t, map[string][]byte{"/tool.tar.gz": payload}, func(r *http.Request) {
		if r.URL.Path == "/tool.tar.gz.sha256" {
			fileHit.Store(true)
		}
	})
	registerProvider(t, "test-sum", &sumProvider{sums: map[string]string{"v1/tool.tar.gz": hexSum(payload)}})
	f := newFetcher(t, srv)
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool.tar.gz",
		Checksum: recipe.ChecksumSpec{Provider: "test-sum", File: "{{.URL}}.sha256"}}
	_, err := f.Get(context.Background(), a, "v1")
	require.NoError(t, err)
	assert.False(t, fileHit.Load())
}

// TUF 暫時失敗不能被記住：第一次失敗、第二次成功，第二次呼叫要拿到信任根。
func TestTrustedRootRetriesAfterError(t *testing.T) {
	calls := 0
	good := loadTestRoot(t)
	f := &Fetcher{fetchRoot: func(context.Context, string) (root.TrustedMaterial, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("tuf 暫時失敗")
		}
		return good, nil
	}}
	_, err := f.trustedMaterial(context.Background())
	require.Error(t, err)
	got, err := f.trustedMaterial(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	// 成功之後就快取，不再重抓。
	_, err = f.trustedMaterial(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

// 錯誤訊息要直接告訴使用者下一步，不必先去查文件。
func TestTrustMismatchMessageMentionsRefresh(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool": payload}, nil)
	f := newFetcher(t, srv)
	f.Recipe = "gitkraken"
	f.Trust = TrustDB{"tool": {URL: srv.URL + "/tool", Size: int64(len(payload)), SHA256: hexSum(other)}}
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool", Checksum: recipe.ChecksumSpec{Trust: true}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrTrustMismatch)
	assert.Contains(t, err.Error(), "上游的檔已更新，執行 task trust:refresh -- gitkraken 重新驗證")
}

// 其他雜湊來源的錯誤不該帶這句：那些來源跟 trust:refresh 無關。
func TestChecksumMismatchHasNoRefreshHint(t *testing.T) {
	srv := fileServer(t, map[string][]byte{
		"/helm.tar.gz":   payload,
		"/checksums.txt": []byte(fmt.Sprintf("%s  helm.tar.gz\n", hexSum(other))),
	}, nil)
	f := newFetcher(t, srv)
	f.Recipe = "helm"
	a := recipe.Artifact{ID: "helm", URL: srv.URL + "/helm.tar.gz",
		Checksum: recipe.ChecksumSpec{File: srv.URL + "/checksums.txt"}}
	_, err := f.Get(context.Background(), a, "v1")
	require.ErrorIs(t, err, ErrChecksumMismatch)
	assert.NotContains(t, err.Error(), "trust:refresh")
}

// 網址變了（例如版本升級）而 trust 還記著舊網址時，錯誤要帶修法。
func TestTrustMissingMessageMentionsRefresh(t *testing.T) {
	srv := fileServer(t, map[string][]byte{"/tool-v2": payload}, nil)
	f := newFetcher(t, srv)
	f.Recipe = "gitkraken"
	f.Trust = TrustDB{"tool": {URL: srv.URL + "/tool-v1", Size: int64(len(payload)), SHA256: hexSum(payload)}}
	a := recipe.Artifact{ID: "tool", URL: srv.URL + "/tool-v2", Checksum: recipe.ChecksumSpec{Trust: true}}
	_, err := f.Get(context.Background(), a, "v2")
	require.ErrorIs(t, err, ErrUnverified)
	require.ErrorIs(t, err, ErrTrustMissing)
	assert.Contains(t, err.Error(), "上游的檔已更新或尚未記錄，執行 task trust:refresh -- gitkraken 重新驗證")
	dirEmpty(t, f.Dir)
}
