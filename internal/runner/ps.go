package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
)

// PSRunner 把平台層與配方組成一支 PowerShell 腳本並執行。
// 組腳本的 composePS 不帶 build tag，Linux 上也能測；實際執行只在 Windows 有意義。
type PSRunner struct {
	FS     fs.FS
	Bin    string
	Stdout io.Writer
	Stderr io.Writer
	// PowerShell 是執行檔；空字串時用 powershell.exe。
	PowerShell string
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// psPreamble 是每支腳本的前文：遇錯即停、嚴格模式、主控台輸出改 UTF-8（PS 5.1 預設 OEM 碼頁，繁中會變 ?）、未處理的錯誤一律以 exit 1 收斂。
// 不用 Write-Error：Error: 走 stderr，exit 碼只用 1。
const psPreamble = "$ErrorActionPreference = 'Stop'\n" +
	"Set-StrictMode -Version Latest\n" +
	"[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $OutputEncoding = [Console]::OutputEncoding\n" +
	"trap { [Console]::Error.WriteLine(\"Error: $($_.Exception.Message)\"); exit 1 }\n"

// psQuote 用單引號字面值包住值；PowerShell 單引號內只有 ' 需要加倍。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Run 組腳本、寫成帶 BOM 的暫存檔，以 powershell.exe -File 執行。
// env（含 --set 的參數與機密）只放進子行程環境變數，不寫進暫存腳本，也不進 argv。
func (r *PSRunner) Run(ctx context.Context, p platform.Platform, rc recipe.Recipe, env map[string]string, act Action) error {
	for k := range env {
		if !envKey.MatchString(k) {
			return fmt.Errorf("環境變數名稱不合法：%q", k)
		}
	}
	recipeDir, err := os.MkdirTemp("", "mksctl-recipe-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(recipeDir)
	if err := extractFiles(r.FS, rc.Name, recipeDir); err != nil {
		return err
	}

	// 內建變數：字面值進腳本（不含機密）；路徑類走環境變數，給 helper 用 $env: 讀。
	vars := map[string]string{
		"OS_ID":      p.ID,
		"OS_VERSION": p.Version,
	}
	script, err := composePS(r.FS, p, rc.Name, vars, act)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp("", "mksctl-run-*.ps1")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(script); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// 同名時內建變數優先，與 bash runner 一致。
	merged := make(map[string]string, len(env)+2)
	for k, v := range env {
		merged[k] = v
	}
	merged["MKSCTL_BIN"] = r.Bin
	merged["MKSCTL_RECIPE_DIR"] = recipeDir

	ps := r.PowerShell
	if ps == "" {
		ps = "powershell.exe"
	}
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-NonInteractive", "-File", f.Name())
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	cmd.Env = mergeEnv(os.Environ(), merged)
	configureProc(cmd)
	cmd.Cancel = func() error { return killProc(cmd) }
	cmd.WaitDelay = 2 * time.Second
	if err := runCmd(cmd); err != nil {
		return fmt.Errorf("配方 %s 執行失敗：%w", rc.Name, err)
	}
	return nil
}

// composePS 組出帶 BOM 的腳本：前文、內建變數、各層 platforms/<層>.ps1（由通用到具體）、
// recipes/<名稱>/recipe.ps1，最後依 act 呼叫函式。
func composePS(fsys fs.FS, p platform.Platform, name string, vars map[string]string, act Action) ([]byte, error) {
	var b bytes.Buffer
	b.Write(utf8BOM)
	b.WriteString(psPreamble)

	keys := make([]string, 0, len(vars))
	for k := range vars {
		if !envKey.MatchString(k) {
			return nil, fmt.Errorf("變數名稱不合法：%q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "$%s = %s\n", k, psQuote(vars[k]))
	}

	for i := len(p.Layers) - 1; i >= 0; i-- {
		rel := "platforms/" + p.Layers[i] + ".ps1"
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			continue // 該層沒有定義檔就略過。
		}
		fmt.Fprintf(&b, "\n# --- %s ---\n%s\n", rel, bytes.TrimPrefix(data, utf8BOM))
	}

	data, err := fs.ReadFile(fsys, "recipes/"+name+"/recipe.ps1")
	if err != nil {
		return nil, fmt.Errorf("讀不到配方 %s：%w", name, err)
	}
	fns := []string{name + "_install", name + "_verify"}
	if act == ActRestore {
		fns = []string{name + "_restore"}
	}
	for _, fn := range fns {
		re := regexp.MustCompile(`(?mi)^\s*function\s+` + regexp.QuoteMeta(fn) + `\b`)
		if !re.Match(data) {
			if act == ActRestore {
				return nil, fmt.Errorf("配方 %s 不支援 restore", name)
			}
			return nil, fmt.Errorf("配方缺少函式 %s", fn)
		}
	}
	fmt.Fprintf(&b, "\n# --- recipes/%s/recipe.ps1 ---\n%s\n", name, bytes.TrimPrefix(data, utf8BOM))
	for _, fn := range fns {
		fmt.Fprintf(&b, "%s\n", fn)
	}
	return b.Bytes(), nil
}
