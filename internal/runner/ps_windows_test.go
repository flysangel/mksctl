//go:build windows

package runner

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func psRun(t *testing.T, body string, env map[string]string, ctx context.Context) (*bytes.Buffer, error) {
	t.Helper()
	fsys := fstest.MapFS{
		"platforms/windows.ps1":   {Data: []byte("")},
		"recipes/demo/recipe.ps1": {Data: []byte(body)},
	}
	var out bytes.Buffer
	r := &PSRunner{FS: fsys, Bin: "mksctl.exe", Stdout: &out, Stderr: &out}
	return &out, r.Run(ctx, winPlatform, recipe.Recipe{Name: "demo"}, env, ActInstall)
}

// 機密只在子行程環境變數：暫存腳本與 powershell.exe 的 CommandLine 都看不到。
func TestPSRunnerSecretNotVisible(t *testing.T) {
	secret := "p@ss'w\"o$rd`x"
	outFile := filepath.Join(t.TempDir(), "secret.txt")
	body := "function demo_install {\n" +
		"  [IO.File]::WriteAllText('" + strings.ReplaceAll(outFile, "'", "''") + "', $env:MKSCTL_PARAM_PW)\n" +
		"  Start-Sleep 2\n}\nfunction demo_verify {}\n"

	done := make(chan error, 1)
	go func() {
		_, err := psRun(t, body, map[string]string{"MKSCTL_PARAM_PW": secret}, context.Background())
		done <- err
	}()

	// 執行期間撈 powershell.exe 的 CommandLine 與暫存目錄的 *.ps1。
	deadline := time.Now().Add(1500 * time.Millisecond)
	var cmdlines string
	for time.Now().Before(deadline) {
		b, _ := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"(Get-CimInstance Win32_Process -Filter \"Name='powershell.exe'\").CommandLine -join \"`n\"").Output()
		cmdlines += string(b)
		time.Sleep(200 * time.Millisecond)
	}
	scripts, _ := filepath.Glob(filepath.Join(os.TempDir(), "mksctl-run-*.ps1"))
	var content string
	for _, s := range scripts {
		if b, err := os.ReadFile(s); err == nil {
			content += string(b)
		}
	}

	require.NoError(t, <-done)
	got, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Equal(t, secret, string(got))
	assert.NotContains(t, cmdlines, "p@ss")
	assert.NotContains(t, content, "p@ss")
}

// 取消後整棵行程樹被殺：配方起的孫行程（ping.exe）不能殘留。
func TestPSRunnerCancelKillsTree(t *testing.T) {
	body := "function demo_install {\n  Start-Process ping -ArgumentList '-n','60','127.0.0.1' -NoNewWindow\n  Start-Sleep 60\n}\nfunction demo_verify {}\n"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := psRun(t, body, nil, ctx)
		done <- err
	}()
	time.Sleep(3 * time.Second)
	cancel()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("取消後 runner 沒有結束")
	}
	time.Sleep(2 * time.Second)
	b, _ := exec.Command("tasklist", "/FI", "IMAGENAME eq ping.exe", "/FO", "CSV", "/NH").Output()
	assert.NotContains(t, strings.ToLower(string(b)), "ping.exe")
}
