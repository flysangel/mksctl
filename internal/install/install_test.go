package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flysangel/mksctl/internal/fetch"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/preflight"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ctx = context.Background()

const ubuntu2404 = "ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"

// countingTransport 數經過的請求，用來證明某些步驟完全沒碰網路。
type countingTransport struct {
	n    atomic.Int64
	base http.RoundTripper
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.base.RoundTrip(r)
}

func (c *countingTransport) Calls() int64 { return c.n.Load() }

// fakeIndex 模擬套件索引。afterSource 的套件只有在對應的來源檔已寫入、
// 而且之後又 Refresh 過，才會變成查得到，跟真的 apt 一樣。
type fakeIndex struct {
	sysroot     string
	mu          sync.Mutex
	refreshes   int
	has         map[string]bool
	afterSource map[string]string
}

func (f *fakeIndex) Refresh(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	for pkg, file := range f.afterSource {
		if _, err := os.Stat(filepath.Join(f.sysroot, "etc/apt/sources.list.d", file)); err == nil {
			f.has[pkg] = true
		}
	}
	return nil
}

func (f *fakeIndex) Has(_ context.Context, pkg string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.has[pkg], nil
}

func (f *fakeIndex) RefreshCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes
}

// fakeRunner 記錄執行過的配方、收到的環境變數，以及當下 run.json 的內容。
type fakeRunner struct {
	calls   []string
	envs    map[string]map[string]string
	runJSON map[string]RunFile
	// files 記下執行當下各 artifact 檔的內容，證明檔案在配方執行時還在。
	files map[string]string
	fail  map[string]bool
}

func (r *fakeRunner) Run(_ context.Context, _ platform.Platform, rc recipe.Recipe, env map[string]string) error {
	r.calls = append(r.calls, rc.Name)
	r.envs[rc.Name] = env
	if b, err := os.ReadFile(env["MKSCTL_RUN"]); err == nil {
		var rf RunFile
		if json.Unmarshal(b, &rf) == nil {
			r.runJSON[rc.Name] = rf
			for k, p := range rf.Artifacts {
				if data, err := os.ReadFile(p); err == nil {
					r.files[k] = string(data)
				}
			}
		}
	}
	if r.fail[rc.Name] {
		return fmt.Errorf("配方 %s 執行失敗：exit status 1", rc.Name)
	}
	return nil
}

type fixture struct {
	Deps     Deps
	HTTP     *countingTransport
	Index    *fakeIndex
	Runner   *fakeRunner
	Sysroot  string
	FetchDir string
	Stdout   *bytes.Buffer
	// versions 是假上游各 repo 的最新版，測試中可改。
	versions map[string]string
	mu       sync.Mutex
	paths    []string
}

func (fx *fixture) requested(prefix string) bool {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	for _, p := range fx.paths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

func (fx *fixture) statePath() string {
	return filepath.Join(fx.Sysroot, "var/lib/mksctl/state.json")
}

func (fx *fixture) state(t *testing.T) state.File {
	t.Helper()
	f, err := state.Load(fx.statePath())
	require.NoError(t, err)
	return f
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// catalog 是測試共用的配方；%[1]s 是假上游的網址。
var catalog = map[string]string{
	"helm": `venue: podman
latest:
  github: o/helm
artifacts:
  - id: bin
    url: "%[1]s/dl/helm-{{.Version}}.tar.gz"
    checksum:
      file: "%[1]s/dl/helm-{{.Version}}.tar.gz.sha256"
`,
	"broken1": `venue: podman
artifacts:
  - id: one
    url: "%[1]s/missing/one.bin"
    checksum:
      file: "%[1]s/missing/one.bin.sha256"
`,
	"broken2": `venue: podman
apt_source:
  file: broken2.sources
  uris: "%[1]s/broken2"
  suites: "{{.Codename}}"
  components: main
  signed_by: /etc/apt/keyrings/broken2.asc
  key_url: "%[1]s/broken2.gpg"
artifacts:
  - id: two
    url: "%[1]s/missing/two.bin"
    checksum:
      file: "%[1]s/missing/two.bin.sha256"
`,
	"docker": `venue: lxc
packages: [docker-ce]
apt_source:
  file: docker.sources
  uris: "%[1]s/docker"
  suites: "{{.Codename}}"
  components: stable
  signed_by: /etc/apt/keyrings/docker.asc
  key_url: "%[1]s/docker.gpg"
`,
	"unv": `venue: podman
unverified: true
artifacts:
  - id: bin
    url: "%[1]s/dl/unv.bin"
`,
	"a":         "venue: podman\nlatest:\n  github: o/a\n",
	"b":         "venue: podman\nlatest:\n  github: o/b\n",
	"c":         "venue: podman\nlatest:\n  github: o/c\n",
	"withparam": "venue: podman\nparams: [USER]\n",
	"other":     "venue: podman\n",
	"gpu":       "venue: vm\nrequires: [gpu]\n",
	"pkgs":      "venue: podman\npackages: [curl, nope, ghost]\n",
	"curlpkg":   "venue: podman\npackages: [curl]\n",
}

func fakeDeps(t *testing.T, osRelease string) *fixture {
	t.Helper()
	fx := &fixture{
		Sysroot:  t.TempDir(),
		FetchDir: filepath.Join(t.TempDir(), "fetch"),
		Stdout:   &bytes.Buffer{},
		versions: map[string]string{"helm": "v3.19.0", "a": "v1", "b": "v1", "c": "v1"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fx.mu.Lock()
		fx.paths = append(fx.paths, r.URL.Path)
		fx.mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/o/") && strings.HasSuffix(p, "/releases/latest"):
			name := strings.Split(p, "/")[2]
			fx.mu.Lock()
			v := fx.versions[name]
			fx.mu.Unlock()
			http.Redirect(w, r, "/o/"+name+"/releases/tag/"+v, http.StatusFound)
		case strings.HasPrefix(p, "/dl/helm-") && strings.HasSuffix(p, ".tar.gz.sha256"):
			name := strings.TrimSuffix(strings.TrimPrefix(p, "/dl/"), ".sha256")
			fmt.Fprintf(w, "%s  %s\n", sum("content:"+name), name)
		case strings.HasPrefix(p, "/dl/helm-") && strings.HasSuffix(p, ".tar.gz"):
			fmt.Fprint(w, "content:"+strings.TrimPrefix(p, "/dl/"))
		case p == "/dl/unv.bin":
			fmt.Fprint(w, "unverified-bytes")
		case p == "/docker.gpg" || p == "/broken2.gpg":
			fmt.Fprint(w, "KEY")
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	fsys := fstest.MapFS{
		"platforms/ubuntu-24.04.yaml": {Data: []byte("provides: []\n")},
		"platforms/ubuntu.yaml":       {Data: []byte("provides: []\n")},
		"platforms/apt.yaml":          {Data: []byte("provides: [apt, systemd]\n")},
		"platforms/common.yaml":       {Data: []byte("provides: []\n")},
		"platforms/alpine-3.22.yaml":  {Data: []byte("repositories: [\"https://dl-cdn.example/v3.22/community\"]\n")},
		"platforms/apk.yaml":          {Data: []byte("provides: [gpu]\n")},
		"pkgmap.yaml":                 {Data: []byte("nope:\n  apt: null\n")},
		"trust/hashes.yaml":           {Data: []byte("{}\n")},
	}
	for name, y := range catalog {
		fsys["recipes/"+name+"/recipe.yaml"] = &fstest.MapFile{Data: []byte(strings.ReplaceAll(y, "%[1]s", srv.URL))}
	}

	require.NoError(t, os.MkdirAll(filepath.Join(fx.Sysroot, "etc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(fx.Sysroot, "etc/os-release"), []byte(osRelease), 0o644))

	fx.HTTP = &countingTransport{base: http.DefaultTransport}
	fx.Index = &fakeIndex{sysroot: fx.Sysroot, has: map[string]bool{"curl": true}, afterSource: map[string]string{"docker-ce": "docker.sources"}}
	fx.Runner = &fakeRunner{
		envs: map[string]map[string]string{}, runJSON: map[string]RunFile{},
		files: map[string]string{}, fail: map[string]bool{},
	}
	fx.Deps = Deps{
		FS:            fsys,
		Sysroot:       fx.Sysroot,
		HTTP:          &http.Client{Transport: fx.HTTP},
		GitHubBase:    srv.URL,
		GitHubAPIBase: srv.URL,
		Index:         fx.Index,
		Runner:        fx.Runner,
		Now:           func() time.Time { return time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC) },
		Stdout:        fx.Stdout,
		FetchDir:      fx.FetchDir,
	}
	return fx
}

func kinds(diffs []preflight.Difference) []string {
	var out []string
	for _, d := range diffs {
		out = append(out, d.Recipe+"/"+string(d.Kind))
	}
	return out
}

func TestNoExactPlatformTouchesNothing(t *testing.T) {
	fx := fakeDeps(t, "ID=ubuntu\nVERSION_ID=\"28.04\"\n")
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	require.NotEmpty(t, diffs)
	assert.Equal(t, preflight.Kind("no-exact-platform"), diffs[0].Kind)
	assert.Equal(t, "ubuntu-28.04", diffs[0].Detail)
	assert.Zero(t, fx.HTTP.Calls())
	assert.Zero(t, fx.Index.RefreshCalls())
	assert.Empty(t, fx.Runner.calls)
	assert.NoFileExists(t, fx.statePath())
	assert.NoDirExists(t, fx.FetchDir)
}

func TestMissingCapabilityAndUnknownParamTouchNothing(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"gpu", "withparam"}, Params: map[string]string{"USER": "x", "TYPO": "y"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"/unknown-param", "gpu/missing-capability"}, kinds(diffs))
	assert.Zero(t, fx.HTTP.Calls())
	assert.Zero(t, fx.Index.RefreshCalls())
}

func TestFetchFailuresCollectedBeforeAnySourceWritten(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"broken1", "broken2"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"broken1/fetch-failed", "broken2/fetch-failed"}, kinds(diffs))
	assert.Contains(t, diffs[0].Detail, "one")
	assert.Contains(t, diffs[1].Detail, "two")
	assert.NoDirExists(t, filepath.Join(fx.Sysroot, "etc/apt/sources.list.d"))
	assert.NoFileExists(t, filepath.Join(fx.Sysroot, "etc/apt/keyrings/broken2.asc"))
	assert.False(t, fx.requested("/broken2.gpg"), "金鑰不該在檢查通過前下載")
	assert.Zero(t, fx.Index.RefreshCalls())
	assert.Empty(t, fx.Runner.calls)
	entries, _ := os.ReadDir(fx.FetchDir)
	assert.Empty(t, entries, "暫存目錄要清空")
}

func TestDockerCePassesAfterSourceWritten(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"docker"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.FileExists(t, filepath.Join(fx.Sysroot, "etc/apt/sources.list.d/docker.sources"))
	assert.FileExists(t, filepath.Join(fx.Sysroot, "etc/apt/keyrings/docker.asc"))
	assert.Equal(t, 1, fx.Index.RefreshCalls())
	assert.Equal(t, []string{"docker"}, fx.Runner.calls)
	e, ok := fx.state(t).Recipes["docker"]
	assert.True(t, ok)
	assert.Equal(t, "", e.Version)
}

func TestAptConflictStopsBeforeWrite(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	dir := filepath.Join(fx.Sysroot, "etc/apt/sources.list.d")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	uri := strings.TrimSuffix(fx.Deps.GitHubBase, "/") + "/docker"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old-docker.list"),
		[]byte("deb [signed-by=/etc/apt/keyrings/old.gpg] "+uri+" noble stable\n"), 0o644))

	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"docker"}})
	require.NoError(t, err)
	require.Equal(t, []string{"docker/apt-source-conflict"}, kinds(diffs))
	assert.Contains(t, diffs[0].Detail, "Fix: rm /etc/apt/sources.list.d/old-docker.list")
	assert.NoFileExists(t, filepath.Join(dir, "docker.sources"))
	assert.False(t, fx.requested("/docker.gpg"))
	assert.Zero(t, fx.Index.RefreshCalls())
}

func TestPackageNotFoundAfterRefresh(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"pkgs"}})
	require.NoError(t, err)
	assert.Equal(t, []preflight.Difference{
		{Recipe: "pkgs", Kind: preflight.PackageNotFound, Detail: "nope (apt 沒有這個套件)"},
		{Recipe: "pkgs", Kind: preflight.PackageNotFound, Detail: "ghost"},
	}, diffs)
	assert.Equal(t, 1, fx.Index.RefreshCalls())
	assert.Empty(t, fx.Runner.calls)
	assert.NoFileExists(t, fx.statePath())
}

func TestUnverifiedNeedsFlag(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"unv"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"unv/unverified"}, kinds(diffs))
	assert.Zero(t, fx.HTTP.Calls())

	diffs, err = Run(ctx, fx.Deps, Options{Names: []string{"unv"}, AllowUnverified: true})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Equal(t, []string{"unv"}, fx.Runner.calls)
	assert.Equal(t, "unverified-bytes", fx.Runner.files["unv/bin"])
}

func TestRunJSONEnvAndCleanup(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)

	env := fx.Runner.envs["helm"]
	assert.Equal(t, "helm", env["MKSCTL_RECIPE"])
	assert.Equal(t, "v3.19.0", env["MKSCTL_VERSION"])
	rf := fx.Runner.runJSON["helm"]
	assert.Equal(t, []string{"ubuntu-24.04", "ubuntu", "apt", "common"}, rf.Layers)
	assert.Equal(t, rf.Layers, rf.PkgmapLayers)
	assert.Contains(t, rf.Artifacts, "helm/bin")
	assert.Equal(t, "content:helm-v3.19.0.tar.gz", fx.Runner.files["helm/bin"])

	entries, err := os.ReadDir(fx.FetchDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "暫存目錄要清空")
	assert.Equal(t, "v3.19.0", fx.state(t).Recipes["helm"].Version)
	assert.Zero(t, fx.Index.RefreshCalls(), "只下載的配方不更新索引")
}

func TestExcludeUsesReplacement(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	mfs := fx.Deps.FS.(fstest.MapFS)
	f := mfs["recipes/helm/recipe.yaml"]
	f.Data = append(f.Data, []byte("exclude:\n  v3.19.0: v3.18.6\n")...)

	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Equal(t, "content:helm-v3.18.6.tar.gz", fx.Runner.files["helm/bin"])
	assert.Equal(t, "v3.18.6", fx.state(t).Recipes["helm"].Version)
}

func TestParamsOnlyToDeclaringRecipe(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"withparam", "other"}, Params: map[string]string{"USER": "bob"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Equal(t, "bob", fx.Runner.envs["withparam"]["MKSCTL_PARAM_USER"])
	assert.NotContains(t, fx.Runner.envs["other"], "MKSCTL_PARAM_USER")
}

func TestFailedRecipeNotRecordedAndRerunResumes(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	fx.Runner.fail["b"] = true
	opt := Options{Names: []string{"a", "b", "c"}}

	diffs, err := Run(ctx, fx.Deps, opt)
	require.Error(t, err)
	assert.Nil(t, diffs)
	assert.Equal(t, []string{"a", "b"}, fx.Runner.calls)
	got := fx.state(t).Recipes
	assert.Len(t, got, 1)
	assert.Equal(t, "v1", got["a"].Version)

	fx.Runner.fail["b"] = false
	fx.Runner.calls = nil
	fx.Stdout.Reset()
	diffs, err = Run(ctx, fx.Deps, opt)
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Contains(t, fx.Stdout.String(), "Info: a 已是最新版 v1，略過")
	assert.Equal(t, []string{"b", "c"}, fx.Runner.calls)
	assert.Len(t, fx.state(t).Recipes, 3)
}

func TestSkipWhenSameVersion(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	f := state.File{Recipes: map[string]state.Entry{"helm": {Version: "v3.19.0", InstalledAt: time.Now()}}}
	require.NoError(t, f.Save(fx.statePath()))

	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Empty(t, fx.Runner.calls)
	assert.Contains(t, fx.Stdout.String(), "Info: helm 已是最新版 v3.19.0，略過")
	assert.False(t, fx.requested("/dl/"), "略過的配方不該下載")
}

func TestAlpineRepositoriesWritten(t *testing.T) {
	fx := fakeDeps(t, "ID=alpine\nVERSION_ID=3.22.1\n")
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"curlpkg"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	b, err := os.ReadFile(filepath.Join(fx.Sysroot, "etc/apk/repositories"))
	require.NoError(t, err)
	assert.Equal(t, "https://dl-cdn.example/v3.22/community\n", string(b))
	assert.Equal(t, 1, fx.Index.RefreshCalls())
}

// 只下載執行檔、不裝套件的配方，不該改 /etc/apk/repositories，也不更新索引。
func TestAlpineDownloadOnlyTouchesNoRepositories(t *testing.T) {
	fx := fakeDeps(t, "ID=alpine\nVERSION_ID=3.22.1\n")
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm", "other"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.NoFileExists(t, filepath.Join(fx.Sysroot, "etc/apk/repositories"))
	assert.Zero(t, fx.Index.RefreshCalls())
	assert.Equal(t, []string{"helm", "other"}, fx.Runner.calls)
}

// 有配方要裝套件時，即使同一次也有只下載的配方，仍要寫套件庫並更新一次索引。
func TestAlpineMixedRunStillRefreshes(t *testing.T) {
	fx := fakeDeps(t, "ID=alpine\nVERSION_ID=3.22.1\n")
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm", "curlpkg"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.FileExists(t, filepath.Join(fx.Sysroot, "etc/apk/repositories"))
	assert.Equal(t, 1, fx.Index.RefreshCalls())
}

func TestUnknownRecipeIsError(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"nosuch"}})
	require.Error(t, err)
	assert.Nil(t, diffs)
}

// sysrootFiles 列出 sysroot 底下全部的檔與目錄，用來證明 check 什麼都沒留下。
func sysrootFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	require.NoError(t, filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		return nil
	}))
	return out
}

func TestCheckOnlyTouchesNothing(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	// FetchDir 留空：check 不能在 sysroot 底下建暫存目錄。
	fx.Deps.FetchDir = ""
	before := sysrootFiles(t, fx.Sysroot)
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"docker", "helm"}, CheckOnly: true})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.Equal(t, before, sysrootFiles(t, fx.Sysroot))
	assert.False(t, fx.requested("/docker.gpg"), "check 不下載 apt 金鑰")
	assert.True(t, fx.requested("/dl/helm-"), "check 仍要下載驗證")
	assert.Zero(t, fx.Index.RefreshCalls())
	assert.Empty(t, fx.Runner.calls)
}

func TestCheckOnlyReportsAptConflict(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	dir := filepath.Join(fx.Sysroot, "etc/apt/sources.list.d")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker.list"),
		[]byte("deb [signed-by=/usr/share/keyrings/other.gpg] "+fx.Deps.GitHubBase+"/docker noble stable\n"), 0o644))
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"docker"}, CheckOnly: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"docker/apt-source-conflict"}, kinds(diffs))
	assert.Empty(t, fx.Runner.calls)
}

func TestCheckOnlyTUFCacheOutsideSysroot(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	fx.Deps.FetchDir = ""
	fx.Deps.TUFCacheDir = filepath.Join(fx.Sysroot, "var/cache/mksctl/tuf")
	var got string
	fetcherHook = func(f *fetch.Fetcher) {
		got = f.TUFCacheDir
		// 模擬 sigstore 寫快取，證明這個目錄結束後會一起清掉。
		require.NoError(t, os.MkdirAll(got, 0o755))
	}
	t.Cleanup(func() { fetcherHook = nil })

	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}, CheckOnly: true})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	require.NotEmpty(t, got)
	rel, err := filepath.Rel(fx.Sysroot, got)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(rel, ".."), "TUF 快取不能在 sysroot 底下：%s", got)
	assert.NoDirExists(t, got)
	assert.NoDirExists(t, fx.Deps.TUFCacheDir)
}

func TestInstallKeepsConfiguredTUFCache(t *testing.T) {
	fx := fakeDeps(t, ubuntu2404)
	fx.Deps.TUFCacheDir = filepath.Join(fx.Sysroot, "var/cache/mksctl/tuf")
	var got string
	fetcherHook = func(f *fetch.Fetcher) { got = f.TUFCacheDir }
	t.Cleanup(func() { fetcherHook = nil })
	_, err := Run(ctx, fx.Deps, Options{Names: []string{"helm"}})
	require.NoError(t, err)
	assert.Equal(t, fx.Deps.TUFCacheDir, got)
}

func TestAlpineSkipsAptSource(t *testing.T) {
	fx := fakeDeps(t, "ID=alpine\nVERSION_ID=3.22.1\n")
	fx.Index.has["docker-ce"] = true
	diffs, err := Run(ctx, fx.Deps, Options{Names: []string{"docker"}})
	require.NoError(t, err)
	assert.Empty(t, diffs)
	assert.NoDirExists(t, filepath.Join(fx.Sysroot, "etc/apt/sources.list.d"))
	assert.NoDirExists(t, filepath.Join(fx.Sysroot, "etc/apt/keyrings"))
	assert.False(t, fx.requested("/docker.gpg"), "不該對金鑰網址發請求")
	assert.Equal(t, []string{"docker"}, fx.Runner.calls)
	assert.Contains(t, fx.Stdout.String(), "Info: docker 的 apt_source 在這個平台不適用，略過")
}
