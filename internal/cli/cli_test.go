package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flysangel/mksctl/internal/install"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/preflight"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ubuntu2404 = "ID=ubuntu\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"

// fakeRunner 記下執行過的配方與環境變數，不真的跑 bash。
type fakeRunner struct {
	calls []string
	envs  map[string]map[string]string
}

func (r *fakeRunner) Run(_ context.Context, _ platform.Platform, rc recipe.Recipe, env map[string]string) error {
	r.calls = append(r.calls, rc.Name)
	r.envs[rc.Name] = env
	return nil
}

// fakeIndex 讓 install 不去跑真的 apt-get。
type fakeIndex struct{ refreshes int }

func (f *fakeIndex) Refresh(context.Context) error             { f.refreshes++; return nil }
func (f *fakeIndex) Has(context.Context, string) (bool, error) { return true, nil }

type harness struct {
	env    env
	runner *fakeRunner
	index  *fakeIndex
	stdin  io.Reader
}

type option func(*harness)

func withUID(uid int) option { return func(h *harness) { h.env.geteuid = func() int { return uid } } }

func withTTYStdin() option {
	return func(h *harness) { h.env.isTerminal = func(io.Reader) bool { return true } }
}

func withStdin(s string) option { return func(h *harness) { h.stdin = strings.NewReader(s) } }

func withOSRelease(s string) option {
	return func(h *harness) {
		if err := os.WriteFile(filepath.Join(h.env.sysroot, "etc/os-release"), []byte(s), 0o644); err != nil {
			panic(err)
		}
	}
}

func withEnv(kv map[string]string) option {
	return func(h *harness) { h.env.getenv = func(k string) string { return kv[k] } }
}

func withFS(files map[string]string) option {
	return func(h *harness) {
		m := fstest.MapFS{}
		for k, v := range files {
			m[k] = &fstest.MapFile{Data: []byte(v)}
		}
		h.env.fsys = m
	}
}

// defaultFS 是測試共用的配方與平台定義。
var defaultFS = map[string]string{
	"platforms/ubuntu-24.04.yaml":    "provides: []\n",
	"platforms/ubuntu-26.04.yaml":    "provides: []\n",
	"platforms/ubuntu.yaml":          "provides: []\n",
	"platforms/apt.yaml":             "provides: [systemd]\n",
	"platforms/common.yaml":          "provides: []\n",
	"platforms/apk.yaml":             "provides: [gpu]\n",
	"pkgmap.yaml":                    "docker:\n  apt: [docker-ce, docker-ce-cli]\n  apk: [docker]\nnope:\n  apt: null\n",
	"trust/hashes.yaml":              "{}\n",
	"recipes/helm/recipe.yaml":       "venue: podman\n",
	"recipes/xfce/recipe.yaml":       "venue: vm\nparams: [XRDP_PASSWD, XRDP_USER]\n",
	"recipes/sd/recipe.yaml":         "venue: lxc\nrequires: [systemd, init, kernel]\n",
	"recipes/gpu/recipe.yaml":        "venue: vm\nrequires: [gpu]\n",
	"recipes/withlatest/recipe.yaml": "venue: podman\nlatest:\n  github: o/x\n",
}

func newHarness(t *testing.T, opts ...option) *harness {
	t.Helper()
	sysroot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(sysroot, "etc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sysroot, "etc/os-release"), []byte(ubuntu2404), 0o644))
	h := &harness{
		runner: &fakeRunner{envs: map[string]map[string]string{}},
		index:  &fakeIndex{},
		stdin:  strings.NewReader(""),
	}
	h.env = env{
		geteuid:    func() int { return 0 },
		isTerminal: func(io.Reader) bool { return false },
		getenv:     func(string) string { return "" },
		sysroot:    sysroot,
		now:        func() time.Time { return time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC) },
		http:       &http.Client{Timeout: 5 * time.Second},
		githubBase: "http://127.0.0.1:1", // 測試預設不連外。
		apiBase:    "http://127.0.0.1:1",
		runner:     h.runner,
		index:      h.index,
		version:    "dev",
	}
	withFS(defaultFS)(h)
	for _, o := range opts {
		o(h)
	}
	return h
}

func (h *harness) run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(h.env, args, h.stdin, &out, &errOut)
	return code, out.String(), errOut.String()
}

func runCLI(t *testing.T, args ...any) (int, string, string) {
	t.Helper()
	var opts []option
	var strs []string
	for _, a := range args {
		switch v := a.(type) {
		case option:
			opts = append(opts, v)
		case string:
			strs = append(strs, v)
		}
	}
	return newHarness(t, opts...).run(strs...)
}

func TestNotRootFailsFirst(t *testing.T) {
	code, _, errOut := runCLI(t, withUID(1000), "install", "helm")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Error: 需要 root")
}

func TestNotRootCheckedBeforeReadingFiles(t *testing.T) {
	for _, sub := range []string{"install", "check", "update"} {
		h := newHarness(t, withUID(1000))
		h.env.sysroot = filepath.Join(t.TempDir(), "不存在")
		code, _, errOut := h.run(sub, "helm")
		assert.Equal(t, 1, code, sub)
		assert.Equal(t, "Error: 需要 root 權限，改用 sudo 執行\n", errOut, sub)
	}
}

func TestSetStdinFromTerminalFails(t *testing.T) {
	code, _, errOut := runCLI(t, withTTYStdin(), "install", "xfce", "--set", "XRDP_PASSWD=-")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Error:")
}

func TestSetStdinReadsValue(t *testing.T) {
	h := newHarness(t, withStdin("s3cret\n"))
	code, out, errOut := h.run("install", "--set", "XRDP_PASSWD=-", "xfce", "--set", "XRDP_USER=bigred")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Success: 安裝完成：xfce")
	assert.Equal(t, "s3cret", h.runner.envs["xfce"]["MKSCTL_PARAM_XRDP_PASSWD"])
	assert.Equal(t, "bigred", h.runner.envs["xfce"]["MKSCTL_PARAM_XRDP_USER"])
}

func TestTwoStdinParamsFail(t *testing.T) {
	code, _, errOut := runCLI(t, withStdin("a\n"), "install", "xfce", "--set", "XRDP_PASSWD=-", "--set", "XRDP_USER=-")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "Error:")
}

func TestSetWithoutEqualsIsUsage(t *testing.T) {
	code, _, errOut := runCLI(t, "install", "xfce", "--set", "XRDP_PASSWD")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "Error:")
}

func TestDifferencesExit3(t *testing.T) {
	h := newHarness(t, withOSRelease("ID=ubuntu\nVERSION_ID=\"28.04\"\n"))
	code, _, errOut := h.run("install", "helm")
	assert.Equal(t, 3, code)
	lines := strings.Split(errOut, "\n")
	assert.Equal(t, "Error: 安裝前檢查有 1 項差異，什麼都沒有安裝", lines[0])
	assert.Equal(t, "  -  no-exact-platform  ubuntu-28.04", lines[1])
	assert.Empty(t, h.runner.calls)
}

func TestFormatDifferencesAlignsAndIndents(t *testing.T) {
	var b bytes.Buffer
	writeDifferences(&b, []preflight.Difference{
		{Recipe: "docker", Kind: preflight.PackageNotFound, Detail: "docker-ce"},
		{Recipe: "ntp", Kind: preflight.NoExactPlatform, Detail: "ubuntu-28.04"},
		{Recipe: "k", Kind: preflight.AptSourceConflict, Detail: "a.list：衝突\nFix: rm a.list"},
	})
	want := "Error: 安裝前檢查有 3 項差異，什麼都沒有安裝\n" +
		"  docker  package-not-found    docker-ce\n" +
		"  ntp     no-exact-platform    ubuntu-28.04\n" +
		"  k       apt-source-conflict  a.list：衝突\n" +
		"                               Fix: rm a.list\n"
	assert.Equal(t, want, b.String())
}

func TestOperationalErrorExit1(t *testing.T) {
	code, _, errOut := runCLI(t, "install", "沒有這個")
	assert.Equal(t, 1, code)
	assert.True(t, strings.HasPrefix(errOut, "Error: "), errOut)
}

func TestInstallSuccess(t *testing.T) {
	h := newHarness(t)
	// sd 需要 init，sysroot 要看起來有 systemd 在跑。
	require.NoError(t, os.MkdirAll(filepath.Join(h.env.sysroot, "run/systemd/system"), 0o755))
	code, out, errOut := h.run("install", "helm", "sd")
	require.Equal(t, 0, code, errOut)
	assert.Contains(t, out, "Success: 安裝完成：helm、sd\n")
	assert.Equal(t, []string{"helm", "sd"}, h.runner.calls)
}

func TestInstallTokenFromSysroot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc/mksctl"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/mksctl/github-token"), []byte(" tok\n"), 0o600))
	assert.Equal(t, "tok", readToken(root))
}

func TestInstallQueriesUpstream(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/o/x/releases/tag/v1", http.StatusFound)
	}))
	defer srv.Close()
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL
	code, _, errOut := h.run("install", "withlatest")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "MKSCTL_VERSION=v1", "MKSCTL_VERSION="+h.runner.envs["withlatest"]["MKSCTL_VERSION"])
}

func TestTokenAbsentIsEmpty(t *testing.T) {
	assert.Equal(t, "", readToken(t.TempDir()))
}

func TestInstallNoNamesIsUsage(t *testing.T) {
	code, _, _ := runCLI(t, "install")
	assert.Equal(t, 2, code)
}

func TestCheckRunsNothing(t *testing.T) {
	h := newHarness(t)
	code, out, errOut := h.run("check", "helm", "xfce", "--set", "XRDP_USER=u")
	require.Equal(t, 0, code, errOut)
	assert.Empty(t, h.runner.calls)
	assert.Zero(t, h.index.refreshes)
	assert.Equal(t, 1, strings.Count(out, "Info: 套件名稱要在寫入套件來源後才能檢查，check 不含這一項"))
	assert.Contains(t, out, "Success: 檢查通過：helm、xfce")
	assert.NoFileExists(t, filepath.Join(h.env.sysroot, install.StatePath))
}

func TestCheckDifferencesExit3(t *testing.T) {
	code, _, errOut := runCLI(t, "check", "xfce", "--set", "TYPO=x")
	assert.Equal(t, 3, code)
	assert.Contains(t, errOut, "unknown-param")
}

func TestUsageExit2(t *testing.T) {
	code, _, errOut := runCLI(t)
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "Error: usage: mksctl <install|check|list|status|update|version> ...")
	code, _, _ = runCLI(t, "bogus")
	assert.Equal(t, 2, code)
}

func TestListThisMachine(t *testing.T) {
	// 主機視角：有 init、不在容器，所以 sd 需要的 init 與 kernel 都有。
	h := newHarness(t)
	require.NoError(t, os.MkdirAll(filepath.Join(h.env.sysroot, "run/systemd/system"), 0o755))
	code, out, errOut := h.run("list")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "helm\nsd\nwithlatest\nxfce\n", out)
}

func TestListThisMachineWithoutInit(t *testing.T) {
	code, out, _ := runCLI(t, "list")
	assert.Equal(t, 0, code)
	assert.Equal(t, "helm\nwithlatest\nxfce\n", out)
}

func TestListPlatform(t *testing.T) {
	code, out, errOut := runCLI(t, "list", "--platform", "ubuntu-26.04")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "helm\nsd\nwithlatest\nxfce\n", out)
	code, out, errOut = runCLI(t, "list", "--platform", "alpine-3.22")
	require.Equal(t, 0, code, errOut)
	assert.Equal(t, "gpu\nhelm\nwithlatest\nxfce\n", out)
}

func TestListPlatformBad(t *testing.T) {
	code, _, errOut := runCLI(t, "list", "--platform", "fedora-40")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Error:")
}

func TestListAll(t *testing.T) {
	code, out, _ := runCLI(t, "list", "--all")
	assert.Equal(t, 0, code)
	assert.Equal(t, "gpu vm\nhelm podman\nsd lxc\nwithlatest podman\nxfce vm\n", out)
}

func TestListAllEmptyCatalog(t *testing.T) {
	code, out, _ := runCLI(t, withFS(map[string]string{"recipes/.keep": ""}), "list", "--all")
	assert.Equal(t, 0, code)
	assert.Empty(t, out)
}

func TestListDoesNotNeedRoot(t *testing.T) {
	code, _, _ := runCLI(t, withUID(1000), "list", "--all")
	assert.Equal(t, 0, code)
}

func TestStatus(t *testing.T) {
	h := newHarness(t, withUID(1000))
	code, out, _ := h.run("status")
	assert.Equal(t, 0, code)
	assert.Equal(t, "Info: 尚未安裝任何配方\n", out)

	st := state.File{Recipes: map[string]state.Entry{
		"helm":   {Version: "v3.19.0", InstalledAt: time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)},
		"docker": {Version: "", InstalledAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)},
	}}
	require.NoError(t, st.Save(filepath.Join(h.env.sysroot, install.StatePath)))
	code, out, _ = h.run("status")
	assert.Equal(t, 0, code)
	assert.Equal(t, "docker - 2026-10-25T00:00:00Z\nhelm v3.19.0 2026-10-26T01:02:03Z\n", out)
}

func writeRun(t *testing.T, rf install.RunFile) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "run.json")
	b, err := json.Marshal(rf)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, b, 0o600))
	return p
}

func TestFetch(t *testing.T) {
	p := writeRun(t, install.RunFile{Artifacts: map[string]string{"helm/bin": "/x/helm.tgz", "other/bin": "/y"}})
	e := withEnv(map[string]string{"MKSCTL_RUN": p, "MKSCTL_RECIPE": "helm"})
	code, out, _ := runCLI(t, withUID(1000), e, "fetch", "bin")
	assert.Equal(t, 0, code)
	assert.Equal(t, "/x/helm.tgz\n", out)

	code, out, errOut := runCLI(t, withUID(1000), e, "fetch", "nope")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "Error:")
}

func TestFetchWithoutRunEnv(t *testing.T) {
	code, _, errOut := runCLI(t, "fetch", "bin")
	assert.Equal(t, 1, code)
	assert.Contains(t, errOut, "Error:")
}

func TestPkgname(t *testing.T) {
	p := writeRun(t, install.RunFile{PkgmapLayers: []string{"ubuntu-24.04", "ubuntu", "apt", "common"}})
	e := withEnv(map[string]string{"MKSCTL_RUN": p})
	code, out, _ := runCLI(t, withUID(1000), e, "pkgname", "docker", "curl")
	assert.Equal(t, 0, code)
	assert.Equal(t, "docker-ce docker-ce-cli curl\n", out)

	code, out, errOut := runCLI(t, e, "pkgname", "nope")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Contains(t, errOut, "Error:")
}

func TestVersion(t *testing.T) {
	code, out, _ := runCLI(t, withUID(1000), "version")
	assert.Equal(t, 0, code)
	assert.Equal(t, "dev\n", out)
}

// passVerifier 讓 attestation 一律通過；真的 sigstore 驗證由 internal/fetch 的測試涵蓋。
type passVerifier struct{}

func (passVerifier) Verify(context.Context, []byte, []byte) error { return nil }

func TestUpdateAlreadyLatest(t *testing.T) {
	var hit int
	srv := newVersionServer(t, &hit)
	h := newHarness(t)
	h.env.version = "v0.2.0"
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL
	h.env.selfPath = filepath.Join(t.TempDir(), "mksctl")
	code, out, errOut := h.run("update")
	assert.Equal(t, 0, code, errOut)
	assert.Equal(t, "Info: 已是最新版 v0.2.0\n", out)
	assert.NoFileExists(t, h.env.selfPath)
}

func TestUpdateReplacesSelf(t *testing.T) {
	const bin = "new"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(bin)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/flysangel/mksctl/releases/download/v0.3.0/mksctl-linux-amd64":
			fmt.Fprint(w, bin)
		case "/flysangel/mksctl/releases/download/v0.3.0/checksums.txt":
			fmt.Fprintf(w, "%s  mksctl-linux-amd64\n", digest)
		case "/repos/flysangel/mksctl/attestations/sha256:" + digest:
			fmt.Fprint(w, `{"attestations":[{"bundle":{}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	h := newHarness(t)
	h.env.version = "v0.2.0"
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL
	h.env.selfPath = filepath.Join(t.TempDir(), "mksctl")
	h.env.verifier = passVerifier{}
	require.NoError(t, os.WriteFile(h.env.selfPath, []byte("old"), 0o755))

	code, out, errOut := h.run("update", "--tag", "v0.3.0")
	assert.Equal(t, 0, code, errOut)
	assert.Equal(t, "Success: mksctl 已從 v0.2.0 更新到 v0.3.0\n", out)
	b, err := os.ReadFile(h.env.selfPath)
	require.NoError(t, err)
	assert.Equal(t, bin, string(b))
}

func TestUpdateRejectsPositional(t *testing.T) {
	code, _, errOut := runCLI(t, "update", "v0.3.0")
	assert.Equal(t, 2, code)
	assert.Contains(t, errOut, "Error: update 不收位置參數：v0.3.0\n")
}

// newVersionServer 回報最新版 v0.2.0，並記錄有沒有被查過。
func newVersionServer(t *testing.T, hit *int) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/flysangel/mksctl/releases/latest" {
			*hit++
			http.Redirect(w, r, "/flysangel/mksctl/releases/tag/v0.2.0", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNotifyAfterListButNotVersion(t *testing.T) {
	var hit int
	srv := newVersionServer(t, &hit)
	h := newHarness(t)
	h.env.version = "v0.1.0"
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL

	code, _, errOut := h.run("list", "--all")
	assert.Equal(t, 0, code)
	assert.Contains(t, errOut, "Info: 有新版 v0.2.0")

	for _, args := range [][]string{{"version"}, {"fetch", "x"}, {"pkgname", "x"}} {
		before := hit
		_, _, errOut = h.run(args...)
		assert.Equal(t, before, hit, args)
		assert.NotContains(t, errOut, "有新版", args)
	}
}

func TestNotifySkippedForDev(t *testing.T) {
	var hit int
	srv := newVersionServer(t, &hit)
	h := newHarness(t)
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL
	code, _, _ := h.run("list", "--all")
	assert.Equal(t, 0, code)
	assert.Zero(t, hit)
}

func TestNotifySkippedOnFailure(t *testing.T) {
	var hit int
	srv := newVersionServer(t, &hit)
	h := newHarness(t, withOSRelease("ID=ubuntu\nVERSION_ID=\"28.04\"\n"))
	h.env.version = "v0.1.0"
	h.env.githubBase, h.env.apiBase = srv.URL, srv.URL
	code, _, _ := h.run("install", "helm")
	assert.Equal(t, 3, code)
	assert.Zero(t, hit)
}

func TestUsageErrorHasOneErrorLine(t *testing.T) {
	code, _, errOut := runCLI(t, "install")
	assert.Equal(t, 2, code)
	assert.Equal(t, 1, strings.Count(errOut, "Error:"), errOut)
	assert.True(t, strings.HasPrefix(errOut, "Error: install 至少要一個配方名稱\n  usage: mksctl <install|check|list|status|update|version> ...\n"), errOut)
}

func TestFetchWithoutRecipeEnv(t *testing.T) {
	p := writeRun(t, install.RunFile{Artifacts: map[string]string{"helm/bin": "/x"}})
	code, out, errOut := runCLI(t, withEnv(map[string]string{"MKSCTL_RUN": p}), "fetch", "bin")
	assert.Equal(t, 1, code)
	assert.Empty(t, out)
	assert.Equal(t, "Error: 缺 MKSCTL_RECIPE（只能在配方內呼叫）\n", errOut)
}
