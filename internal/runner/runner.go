// Package runner 把平台層與配方組成一支 bash 腳本並執行。
package runner

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
)

// Runner 執行配方。Bin 是 mksctl 自身路徑，配方用它呼叫 fetch。
type Runner struct {
	FS     fs.FS
	Bin    string
	Stdout io.Writer
	Stderr io.Writer
}

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// quote 用單引號包住值；值內的單引號改寫成 '\”。
// 單引號內 bash 不做任何展開，值不會被當成指令。
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Run 依序 source 各層與配方，再呼叫 <名稱>_install、<名稱>_verify。
// env 疊在內建變數之上，同名時以 env 為準。
func (r *Runner) Run(ctx context.Context, p platform.Platform, rc recipe.Recipe, env map[string]string) error {
	recipeDir, err := os.MkdirTemp("", "mksctl-recipe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(recipeDir)
	if err := r.extractFiles(rc.Name, recipeDir); err != nil {
		return err
	}

	initRunning := "0"
	if p.InitRunning {
		initRunning = "1"
	}
	vars := map[string]string{
		"OS_ID":               p.ID,
		"OS_VERSION":          p.Version,
		"OS_CODENAME":         p.Codename,
		"MKSCTL_BIN":          r.Bin,
		"MKSCTL_RECIPE_DIR":   recipeDir,
		"MKSCTL_INIT_RUNNING": initRunning,
	}
	for k, v := range env {
		vars[k] = v
	}

	script, err := r.compose(p, rc.Name, vars)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "mksctl-run-*.sh")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(script); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "bash", f.Name())
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	cmd.Env = os.Environ()
	// 配方會產生子行程（例如 sleep）；只殺 bash 的話，子行程握著輸出管線，
	// Wait 會卡到它自己結束。所以自成一組行程，取消時整組一起殺。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("配方 %s 執行失敗：%w", rc.Name, err)
	}
	return nil
}

func (r *Runner) compose(p platform.Platform, name string, vars map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString("#!/bin/bash\nset -euo pipefail\n")

	keys := make([]string, 0, len(vars))
	for k := range vars {
		if !envKey.MatchString(k) {
			return "", fmt.Errorf("環境變數名稱不合法：%q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s\n", k, quote(vars[k]))
	}

	// Layers 由具體到通用；組裝要反過來，具體層才能覆寫通用層。
	// 檔案內容直接內嵌進腳本，不先解到磁碟：嵌入的 FS 沒有實體路徑可 source，
	// 這些檔又都是不設 set 旗標的函式庫，內嵌與 source 效果相同。
	for i := len(p.Layers) - 1; i >= 0; i-- {
		rel := "platforms/" + p.Layers[i] + ".sh"
		data, err := fs.ReadFile(r.FS, rel)
		if err != nil {
			continue // 該層沒有定義檔就略過。
		}
		fmt.Fprintf(&b, "\n# --- %s ---\n%s\n", rel, data)
	}
	data, err := fs.ReadFile(r.FS, "recipes/"+name+"/recipe.sh")
	if err != nil {
		return "", fmt.Errorf("讀不到配方 %s：%w", name, err)
	}
	fmt.Fprintf(&b, "\n# --- recipes/%s/recipe.sh ---\n%s\n", name, data)

	// 先確認兩個函式都在，才開始動系統；缺一個就不該裝一半。
	for _, fn := range []string{name + "_install", name + "_verify"} {
		fmt.Fprintf(&b, "declare -F %[1]s >/dev/null || { echo %[2]s >&2; exit 1; }\n",
			quote(fn), quote("Error: 配方缺少函式 "+fn))
	}
	fmt.Fprintf(&b, "%s_install\n%s_verify\n", name, name)
	return b.String(), nil
}

// extractFiles 把 recipes/<名稱>/files/ 解到 dst；沒有附屬檔時 dst 維持空目錄。
func (r *Runner) extractFiles(name, dst string) error {
	root := "recipes/" + name + "/files"
	if _, err := fs.Stat(r.FS, root); err != nil {
		return nil
	}
	return fs.WalkDir(r.FS, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(strings.TrimPrefix(p, root)))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(r.FS, p)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if info, err := d.Info(); err == nil && info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
}
