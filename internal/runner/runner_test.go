package runner

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 讀真的 common.sh，測試才真的練到它。
func commonSh(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../platforms/common.sh")
	require.NoError(t, err)
	return b
}

func run(t *testing.T, fsys fstest.MapFS, layers []string, name string, env map[string]string) (string, string, error) {
	t.Helper()
	var out, errb bytes.Buffer
	r := &Runner{FS: fsys, Bin: "/bin/false", Stdout: &out, Stderr: &errb}
	err := r.Run(context.Background(), platform.Platform{Layers: layers}, recipe.Recipe{Name: name}, env)
	return out.String(), errb.String(), err
}

func demoFS(t *testing.T, body string) fstest.MapFS {
	return fstest.MapFS{
		"platforms/common.sh":    {Data: commonSh(t)},
		"recipes/demo/recipe.sh": {Data: []byte(body)},
	}
}

func TestRunCallsInstallThenVerify(t *testing.T) {
	fsys := demoFS(t, "demo_install() { echo install \"${MKSCTL_PARAM_USER}\"; }\ndemo_verify() { echo verify; }\n")
	out, _, err := run(t, fsys, []string{"common"}, "demo", map[string]string{"MKSCTL_PARAM_USER": "bob"})
	require.NoError(t, err)
	assert.Equal(t, "install bob\nverify\n", out)
}

func TestRunVerifyFailureIsError(t *testing.T) {
	fsys := demoFS(t, "demo_install() { echo i; }\ndemo_verify() { return 1; }\n")
	_, _, err := run(t, fsys, []string{"common"}, "demo", nil)
	require.Error(t, err)
}

func TestRunMissingFunctionIsErrorBeforeExec(t *testing.T) {
	fsys := demoFS(t, "echo TOPLEVEL_MARK\ndemo_install() { echo INSTALL_MARK; }\n")
	out, errs, err := run(t, fsys, []string{"common"}, "demo", nil)
	require.Error(t, err)
	assert.NotContains(t, out, "TOPLEVEL_MARK")
	assert.NotContains(t, out, "INSTALL_MARK")
	// CLI 會加 Error: 前綴，錯誤字串本身不能再帶，否則印出來是 Error: Error:。
	assert.NotContains(t, err.Error(), "Error:")
	assert.Contains(t, err.Error(), "demo_verify")
	_ = errs
}

func TestRunEnvExportedAndQuoted(t *testing.T) {
	fsys := demoFS(t, "demo_install() { echo \"${OS_ID}|${OS_VERSION}|${OS_CODENAME}|${MKSCTL_INIT_RUNNING}|${MKSCTL_PARAM_X}\"; }\ndemo_verify() { :; }\n")
	var out bytes.Buffer
	r := &Runner{FS: fsys, Bin: "/bin/false", Stdout: &out, Stderr: io.Discard}
	p := platform.Platform{ID: "ubuntu", Version: "26.04", Codename: "resolute", Layers: []string{"common"}, InitRunning: true}
	err := r.Run(context.Background(), p, recipe.Recipe{Name: "demo"}, map[string]string{"MKSCTL_PARAM_X": "a'b $c \"d\""})
	require.NoError(t, err)
	assert.Equal(t, "ubuntu|26.04|resolute|1|a'b $c \"d\"\n", out.String())
}

func TestRunLayerOrderGeneralToSpecific(t *testing.T) {
	fsys := demoFS(t, "demo_install() { echo \"${WHO}\"; }\ndemo_verify() { :; }\n")
	fsys["platforms/apt.sh"] = &fstest.MapFile{Data: []byte("WHO=apt\n")}
	fsys["platforms/ubuntu.sh"] = &fstest.MapFile{Data: []byte("WHO=ubuntu\n")}
	// ubuntu-26.04.sh 不存在，要被略過。
	out, _, err := run(t, fsys, []string{"ubuntu-26.04", "ubuntu", "apt", "common"}, "demo", nil)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu\n", out)
}

func TestRunRecipeDirExtractsFiles(t *testing.T) {
	fsys := demoFS(t, "demo_install() { cat \"${MKSCTL_RECIPE_DIR}/hello.txt\"; echo \"${MKSCTL_RECIPE_DIR}\" >&2; }\ndemo_verify() { :; }\n")
	fsys["recipes/demo/files/hello.txt"] = &fstest.MapFile{Data: []byte("hi\n")}
	out, errs, err := run(t, fsys, []string{"common"}, "demo", nil)
	require.NoError(t, err)
	assert.Equal(t, "hi\n", out)
	_, statErr := os.Stat(strings.TrimSpace(errs))
	assert.True(t, os.IsNotExist(statErr), "暫存目錄要在 Run 後移除")
}

func TestRunRecipeDirExistsWhenNoFiles(t *testing.T) {
	fsys := demoFS(t, "demo_install() { [[ -d \"${MKSCTL_RECIPE_DIR}\" ]] && echo ok; }\ndemo_verify() { :; }\n")
	out, _, err := run(t, fsys, []string{"common"}, "demo", nil)
	require.NoError(t, err)
	assert.Equal(t, "ok\n", out)
}

func TestRunContextCancelKills(t *testing.T) {
	fsys := demoFS(t, "demo_install() { sleep 30 & wait; }\ndemo_verify() { :; }\n")
	ctx, cancel := context.WithTimeout(context.Background(), 300_000_000)
	defer cancel()
	r := &Runner{FS: fsys, Bin: "/bin/false", Stdout: io.Discard, Stderr: io.Discard}
	start := time.Now()
	err := r.Run(ctx, platform.Platform{Layers: []string{"common"}}, recipe.Recipe{Name: "demo"}, nil)
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "沒有殺整組行程時會卡到 sleep 結束")
}

func TestRunSetEAppliesToRecipe(t *testing.T) {
	fsys := demoFS(t, "demo_install() { false; echo NOT_REACHED; }\ndemo_verify() { :; }\n")
	out, _, err := run(t, fsys, []string{"common"}, "demo", nil)
	require.Error(t, err)
	assert.NotContains(t, out, "NOT_REACHED")
}

func TestCommonSvcEnable(t *testing.T) {
	fsys := demoFS(t, "demo_install() { svc_enable foo; }\ndemo_verify() { :; }\n")
	fsys["platforms/stub.sh"] = &fstest.MapFile{Data: []byte("_svc_enable() { echo enable \"$1\"; }\n_svc_start() { echo start \"$1\"; }\n")}
	layers := []string{"stub", "common"}

	out, _, err := run(t, fsys, layers, "demo", nil)
	require.NoError(t, err)
	assert.Equal(t, "enable foo\n", out)

	out, _, err = run(t, fsys, layers, "demo", map[string]string{"MKSCTL_INIT_RUNNING": "1"})
	require.NoError(t, err)
	assert.Equal(t, "enable foo\nstart foo\n", out)
}

func TestCommonFetchCallsBin(t *testing.T) {
	fsys := demoFS(t, "demo_install() { fetch helm; }\ndemo_verify() { :; }\n")
	var out bytes.Buffer
	r := &Runner{FS: fsys, Bin: "/bin/echo", Stdout: &out, Stderr: io.Discard}
	err := r.Run(context.Background(), platform.Platform{Layers: []string{"common"}}, recipe.Recipe{Name: "demo"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "fetch helm\n", out.String())
}

// 呼叫端把函式放在 || 左側時，bash 會關掉 set -e；install_bin 必須自己 return 1。
func TestInstallBinFailsUnderOrContext(t *testing.T) {
	body := "step() { install_bin \"$1\" foo || return 1; echo AFTER; }\n" +
		"demo_install() { local rc=0; step \"${SRC}\" || rc=$?; echo \"rc=${rc}\"; return \"${rc}\"; }\n" +
		"demo_verify() { :; }\n"
	dir := t.TempDir()
	good := dir + "/good"
	require.NoError(t, os.WriteFile(good, []byte("x"), 0o644))

	cases := map[string]map[string]string{
		"來源不存在":   {"SRC": dir + "/missing", "MKSCTL_BIN_DIR": dir},
		"目標目錄不存在": {"SRC": good, "MKSCTL_BIN_DIR": dir + "/nodir"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			out, _, err := run(t, demoFS(t, body), []string{"common"}, "demo", env)
			require.Error(t, err)
			assert.Contains(t, out, "rc=1")
			assert.NotContains(t, out, "AFTER")
		})
	}
}
