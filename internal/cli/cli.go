// Package cli 是 mksctl 的命令列：解析子命令、接上各引擎套件，並決定結束碼。
//
// 結束碼：0 成功；1 操作錯誤；2 用法錯誤；3 安裝前檢查有差異（機器沒被改）。
// 3 跟 1 分開，呼叫端腳本才分得出「環境不符」與「真的壞了」。
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"gopkg.in/yaml.v3"

	"github.com/flysangel/mksctl"
	"github.com/flysangel/mksctl/internal/fetch"
	"github.com/flysangel/mksctl/internal/install"
	"github.com/flysangel/mksctl/internal/pkgindex"
	"github.com/flysangel/mksctl/internal/pkgmap"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/preflight"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/runner"
	"github.com/flysangel/mksctl/internal/selfupdate"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/flysangel/mksctl/internal/upstream"
)

// Version 由建置時的 -ldflags "-X github.com/flysangel/mksctl/internal/cli.Version=<tag>" 寫入。
// 保持 "dev" 表示本機建置，不查新版：開發中的版本沒有可比的 release。
var Version = "dev"

// 結束碼。
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
	exitDiffs = 3
)

// TokenPath 是 GitHub token 相對 sysroot 的路徑。
const TokenPath = "etc/mksctl/github-token"

// usageBody 是用法說明，不含前綴。依情境當首行訊息或縮排在 Error: 下方的 body。
const usageBody = `usage: mksctl <install|check|list|status|update|version> ...
  install <名稱>... [--set KEY=VALUE]... [--allow-unverified]
  check <名稱>... [--set KEY=VALUE]... [--allow-unverified]
  list [--platform <層>] [--all]
  status
  update [--tag vX.Y.Z]
  version
`

// usageText 是沒有子命令或子命令不認得時的整段輸出：用法本身就是那則錯誤訊息。
const usageText = "Error: " + usageBody

// indent 在每行前加兩格。多行訊息只有首行帶前綴，其餘當 body 縮排。
func indent(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return strings.Join(lines, "")
}

// env 收齊所有對外部世界的依賴。測試換掉它，就不需要真的 root、終端機或網路。
type env struct {
	geteuid    func() int
	isTerminal func(io.Reader) bool
	getenv     func(string) string
	sysroot    string
	now        func() time.Time
	http       *http.Client
	githubBase string
	apiBase    string
	fsys       fs.FS
	// runner 為 nil 時用正式的 runner.Runner；index 為 nil 時由 install 依平台挑。
	runner  install.Runner
	index   pkgindex.Index
	version string
	// selfPath 是 update 要換掉的檔，空字串時用正在跑的執行檔；verifier 為 nil 時走真的 sigstore。
	selfPath string
	verifier selfupdate.Verifier
}

func defaultEnv() env {
	sysroot := os.Getenv("MKSCTL_SYSROOT")
	if sysroot == "" {
		sysroot = "/"
	}
	return env{
		geteuid:    os.Geteuid,
		isTerminal: isTerminal,
		getenv:     os.Getenv,
		sysroot:    sysroot,
		now:        time.Now,
		fsys:       mksctl.FS,
		version:    Version,
	}
}

// isTerminal 用 ioctl 判斷，不看檔案模式：/dev/null 也是字元裝置，但它不是終端機。
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// Main 執行一次命令並回傳結束碼。args 不含程式名稱。
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(defaultEnv(), args, stdin, stdout, stderr)
}

// exitErr 讓子命令同時交出結束碼與訊息，訊息由 run 統一加 Error: 前綴。
type exitErr struct {
	code int
	msg  string
}

func (e *exitErr) Error() string { return e.msg }

func usageErr(format string, a ...any) error {
	return &exitErr{code: exitUsage, msg: fmt.Sprintf(format, a...)}
}

func run(e env, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sub, rest := args[0], args[1:]
	var err error
	notify := false
	switch sub {
	case "install", "check":
		err = cmdInstall(ctx, e, sub, rest, stdin, stdout, stderr)
		notify = true
	case "list":
		err = cmdList(e, rest, stdout)
		notify = true
	case "status":
		err = cmdStatus(e, rest, stdout)
		notify = true
	case "fetch":
		err = cmdFetch(e, rest, stdout)
	case "pkgname":
		err = cmdPkgname(e, rest, stdout)
	case "trust-record":
		// 隱藏子命令：不列在 usage，只給 bin/trust-refresh.sh 呼叫。
		err = cmdTrustRecord(ctx, e, rest, stdout)
	case "version":
		if len(rest) > 0 {
			err = usageErr("version 不收參數")
		} else {
			fmt.Fprintln(stdout, e.version)
		}
	case "update":
		err = cmdUpdate(ctx, e, rest, stdout)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usageBody)
		return exitOK
	default:
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	if err != nil {
		var ee *exitErr
		if errors.As(err, &ee) {
			if ee.msg != "" {
				fmt.Fprintf(stderr, "Error: %s\n", ee.msg)
			}
			if ee.code == exitUsage {
				fmt.Fprint(stderr, indent(usageBody))
			}
			return ee.code
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return exitError
	}
	// 只在成功時提示：失敗時使用者要先看懂錯誤，不要被更新提示岔開。
	if notify && e.version != "dev" {
		selfupdate.Notify(ctx, e.version, e.upstream(), stderr)
	}
	return exitOK
}

func (e env) upstream() *upstream.Client {
	return &upstream.Client{HTTP: e.http, GitHubBase: e.githubBase, GitHubAPIBase: e.apiBase, Token: readToken(e.sysroot)}
}

// requireRoot 要在讀任何檔之前呼叫：非 root 時，後面的錯誤（例如讀不到狀態檔）只會誤導使用者。
func requireRoot(e env) error {
	if e.geteuid() != 0 {
		return &exitErr{code: exitError, msg: "需要 root 權限，改用 sudo 執行"}
	}
	return nil
}

// readToken 讀 GitHub token。檔不存在或讀不到都回空字串：token 只用來避開限流，不是必要條件。
func readToken(sysroot string) string {
	b, err := os.ReadFile(filepath.Join(sysroot, TokenPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// setFlag 收集可重複的 --set。
type setFlag []string

func (s *setFlag) String() string     { return strings.Join(*s, ",") }
func (s *setFlag) Set(v string) error { *s = append(*s, v); return nil }

// parseInterspersed 讓旗標可以放在名稱前後。標準 flag 套件遇到第一個非旗標參數就停，
// 這裡把它收成位置參數後接著解析剩下的。
func parseInterspersed(fset *flag.FlagSet, args []string) ([]string, error) {
	fset.SetOutput(io.Discard) // 錯誤訊息由 run 統一印，不讓 flag 套件印英文用法。
	var pos []string
	for {
		if err := fset.Parse(args); err != nil {
			return nil, usageErr("%v", err)
		}
		args = fset.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// parseParams 把 --set 轉成參數表。值為 "-" 時從 stdin 讀，讓密碼不必出現在命令列與 ps 輸出裡。
func parseParams(sets []string, stdin io.Reader, isTTY func(io.Reader) bool) (map[string]string, error) {
	params := map[string]string{}
	stdinKey := ""
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, usageErr("--set 要寫成 KEY=VALUE：%q", s)
		}
		if v == "-" {
			if stdinKey != "" {
				return nil, usageErr("只有一個 stdin，%s 與 %s 不能都用 -", stdinKey, k)
			}
			stdinKey = k
			// 終端機不讀：等使用者輸入會讓命令看起來像當掉，而且輸入會回顯在螢幕上。
			if isTTY(stdin) {
				return nil, &exitErr{code: exitError, msg: fmt.Sprintf("--set %s=- 要從 stdin 讀值，但 stdin 是終端機；改用管線或重導向", k)}
			}
			b, err := io.ReadAll(stdin)
			if err != nil {
				return nil, fmt.Errorf("讀 stdin：%w", err)
			}
			// 只去掉一個結尾換行：echo 會補一個，值本身的其他空白要原樣保留。
			v = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		}
		params[k] = v
	}
	return params, nil
}

func cmdInstall(ctx context.Context, e env, sub string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := requireRoot(e); err != nil {
		return err
	}
	fset := flag.NewFlagSet(sub, flag.ContinueOnError)
	var sets setFlag
	fset.Var(&sets, "set", "")
	allow := fset.Bool("allow-unverified", false, "")
	names, err := parseInterspersed(fset, args)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return usageErr("%s 至少要一個配方名稱", sub)
	}
	params, err := parseParams(sets, stdin, e.isTerminal)
	if err != nil {
		return err
	}

	checkOnly := sub == "check"
	if checkOnly {
		fmt.Fprintln(stdout, "Info: 套件名稱要在寫入套件來源後才能檢查，check 不含這一項")
	}
	r := e.runner
	if r == nil {
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("找不到 mksctl 自身路徑：%w", err)
		}
		r = &runner.Runner{FS: e.fsys, Bin: bin, Stdout: stdout, Stderr: stderr}
	}
	deps := install.Deps{
		FS: e.fsys, TrustFS: e.fsys, Sysroot: e.sysroot,
		HTTP: e.http, GitHubBase: e.githubBase, GitHubAPIBase: e.apiBase, Token: readToken(e.sysroot),
		Index: e.index, Runner: r, Now: e.now, Stdout: stdout,
		TUFCacheDir: filepath.Join(e.sysroot, "var/cache/mksctl/tuf"),
	}
	diffs, err := install.Run(ctx, deps, install.Options{
		Names: names, Params: params, AllowUnverified: *allow, Sysroot: e.sysroot, CheckOnly: checkOnly,
	})
	if err != nil {
		return err
	}
	if len(diffs) > 0 {
		writeDifferences(stderr, diffs)
		return &exitErr{code: exitDiffs}
	}
	if checkOnly {
		fmt.Fprintf(stdout, "Success: 檢查通過：%s\n", strings.Join(names, "、"))
	} else {
		fmt.Fprintf(stdout, "Success: 安裝完成：%s\n", strings.Join(names, "、"))
	}
	return nil
}

// writeDifferences 印成對齊的表。Detail 的續行對齊到 Detail 欄，Fix 才不會跟下一筆差異混在一起。
func writeDifferences(w io.Writer, diffs []preflight.Difference) {
	fmt.Fprintf(w, "Error: 安裝前檢查有 %d 項差異，什麼都沒有安裝\n", len(diffs))
	recipeOf := func(d preflight.Difference) string {
		if d.Recipe == "" {
			return "-" // 跟單一配方無關的差異，用 - 佔位，欄位才對得齊。
		}
		return d.Recipe
	}
	w1, w2 := 0, 0
	for _, d := range diffs {
		w1 = max(w1, len(recipeOf(d)))
		w2 = max(w2, len(d.Kind))
	}
	indent := strings.Repeat(" ", 2+w1+2+w2+2)
	for _, d := range diffs {
		lines := strings.Split(d.Detail, "\n")
		fmt.Fprintf(w, "  %-*s  %-*s  %s\n", w1, recipeOf(d), w2, d.Kind, lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "%s%s\n", indent, l)
		}
	}
}

func cmdList(e env, args []string, stdout io.Writer) error {
	fset := flag.NewFlagSet("list", flag.ContinueOnError)
	layer := fset.String("platform", "", "")
	all := fset.Bool("all", false, "")
	pos, err := parseInterspersed(fset, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageErr("list 不收配方名稱：%s", strings.Join(pos, " "))
	}
	if *all && *layer != "" {
		return usageErr("--all 與 --platform 不能同時用")
	}
	cat, err := recipe.Load(e.fsys)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(cat))
	for n := range cat {
		names = append(names, n)
	}
	sort.Strings(names)
	if *all {
		for _, n := range names {
			fmt.Fprintf(stdout, "%s %s\n", n, cat[n].Venue)
		}
		return nil
	}

	var p platform.Platform
	if *layer != "" {
		if p, err = platformFromLayer(*layer); err != nil {
			return err
		}
	} else {
		if p, err = platform.Detect(e.sysroot); err != nil {
			return err
		}
	}
	defs, err := platform.LoadDefs(e.fsys)
	if err != nil {
		return err
	}
	p = defs.Apply(p)
	for _, n := range names {
		ok := true
		for _, req := range cat[n].Requires {
			ok = ok && p.Provides[req]
		}
		if ok {
			fmt.Fprintln(stdout, n)
		}
	}
	return nil
}

// platformFromLayer 用層名合成 os-release 再解析，規則才跟真實機器一致。
// 執行環境假設是 VM（有 init、不在容器）：harness 在主機上問的是「VM 裡能裝什麼」。
func platformFromLayer(layer string) (platform.Platform, error) {
	i := strings.LastIndex(layer, "-")
	if i <= 0 || i == len(layer)-1 {
		return platform.Platform{}, fmt.Errorf("平台層要寫成 <發行版>-<版本>，例如 ubuntu-26.04：%q", layer)
	}
	p, err := platform.Parse(fmt.Appendf(nil, "ID=%s\nVERSION_ID=%s\n", layer[:i], layer[i+1:]))
	if err != nil {
		return platform.Platform{}, err
	}
	if p.Layers[0] != layer {
		return platform.Platform{}, fmt.Errorf("平台層 %q 不是有效的層名，解析結果是 %q", layer, p.Layers[0])
	}
	p.InitRunning, p.InContainer = true, false
	return p, nil
}

func cmdStatus(e env, args []string, stdout io.Writer) error {
	if len(args) > 0 {
		return usageErr("status 不收參數")
	}
	st, err := state.Load(filepath.Join(e.sysroot, install.StatePath))
	if err != nil {
		return err
	}
	if len(st.Recipes) == 0 {
		fmt.Fprintln(stdout, "Info: 尚未安裝任何配方")
		return nil
	}
	names := make([]string, 0, len(st.Recipes))
	for n := range st.Recipes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		en := st.Recipes[n]
		v := en.Version
		if v == "" {
			v = "-" // 無版本的配方；用 - 佔位，每行才固定三欄，腳本好切。
		}
		fmt.Fprintf(stdout, "%s %s %s\n", n, v, en.InstalledAt.UTC().Format(time.RFC3339))
	}
	return nil
}

// loadRunFile 讀 install 交給配方的 run.json。只有配方執行中才有 MKSCTL_RUN。
func loadRunFile(e env, sub string) (install.RunFile, error) {
	var rf install.RunFile
	path := e.getenv("MKSCTL_RUN")
	if path == "" {
		return rf, fmt.Errorf("沒有 MKSCTL_RUN；%s 只能在 mksctl install 執行的配方裡呼叫", sub)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return rf, fmt.Errorf("讀 %s：%w", path, err)
	}
	if err := json.Unmarshal(b, &rf); err != nil {
		return rf, fmt.Errorf("%s 內容壞掉：%w", path, err)
	}
	return rf, nil
}

// cmdFetch 只印路徑、不加前綴：配方用 $(mksctl fetch <ID>) 取值。
func cmdFetch(e env, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageErr("用法：mksctl fetch <artifact ID>")
	}
	rf, err := loadRunFile(e, "fetch")
	if err != nil {
		return err
	}
	name := e.getenv("MKSCTL_RECIPE")
	if name == "" {
		// 沒有配方名就查不到鍵，與其回報「沒有 artifact」誤導使用者，不如直接說缺什麼。
		return errors.New("缺 MKSCTL_RECIPE（只能在配方內呼叫）")
	}
	// 鍵含配方名：配方只拿得到自己宣告的 artifact，不會誤用別的配方同名的檔。
	p, ok := rf.Artifacts[name+"/"+args[0]]
	if !ok {
		return fmt.Errorf("配方 %s 沒有 artifact %q；先在 recipe.yaml 的 artifacts 宣告", name, args[0])
	}
	fmt.Fprintln(stdout, p)
	return nil
}

// cmdPkgname 把邏輯名稱換成實際套件名稱，一行以空白分隔，配方可直接展開給套件管理員。
func cmdPkgname(e env, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageErr("用法：mksctl pkgname <邏輯名稱>...")
	}
	rf, err := loadRunFile(e, "pkgname")
	if err != nil {
		return err
	}
	pm := pkgmap.Map{}
	if b, err := fs.ReadFile(e.fsys, "pkgmap.yaml"); err == nil {
		if pm, err = pkgmap.Load(b); err != nil {
			return fmt.Errorf("解析 pkgmap.yaml：%w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var out []string
	for _, l := range args {
		real, err := pm.Lookup(l, rf.PkgmapLayers)
		if errors.Is(err, pkgmap.ErrUnavailable) {
			return fmt.Errorf("%s 在這個平台沒有對應的套件", l)
		}
		if err != nil {
			return err
		}
		out = append(out, real...)
	}
	fmt.Fprintln(stdout, strings.Join(out, " "))
	return nil
}

// cmdUpdate 把正在跑的 mksctl 換成新版。先驗 root：要寫 /usr/local/bin，非 root 一定失敗。
func cmdUpdate(ctx context.Context, e env, args []string, stdout io.Writer) error {
	if err := requireRoot(e); err != nil {
		return err
	}
	fset := flag.NewFlagSet("update", flag.ContinueOnError)
	tag := fset.String("tag", "", "")
	pos, err := parseInterspersed(fset, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return usageErr("update 不收位置參數：%s", strings.Join(pos, " "))
	}

	dest := e.selfPath
	if dest == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("找不到 mksctl 自身路徑：%w", err)
		}
		// 解開符號連結：換掉的是實體檔，連結本身不動。
		if dest, err = filepath.EvalSymlinks(exe); err != nil {
			return fmt.Errorf("解析 mksctl 自身路徑：%w", err)
		}
	}
	v := e.verifier
	if v == nil {
		v = selfupdate.SigstoreVerifier{TUFCacheDir: filepath.Join(e.sysroot, "var/cache/mksctl/tuf")}
	}
	u := &selfupdate.Updater{Client: e.upstream(), Verifier: v}
	res, err := u.Update(ctx, *tag, e.version, dest)
	if err != nil {
		return err
	}
	if res.UpToDate {
		fmt.Fprintf(stdout, "Info: 已是最新版 %s\n", res.To)
		return nil
	}
	fmt.Fprintf(stdout, "Success: mksctl 已從 %s 更新到 %s\n", res.From, res.To)
	return nil
}

// trustHashesPath 是 trust-record --write 要更新的檔，相對工作目錄（repo 根）。
const trustHashesPath = "trust/hashes.yaml"

// cmdTrustRecord 對配方每個 checksum.trust 的 artifact 查最新版、下載、印出該記進
// trust/hashes.yaml 的 YAML。不需要 root，也不寫 sysroot；只有 --write 會改 repo 的 trust/hashes.yaml。
// --catalog <目錄> 是測試用：從該目錄的 recipes/ 載入配方，取代嵌入的 FS，讓 shell 測試不必先建一個真的 trust 配方。
func cmdTrustRecord(ctx context.Context, e env, args []string, stdout io.Writer) error {
	fset := flag.NewFlagSet("trust-record", flag.ContinueOnError)
	write := fset.Bool("write", false, "")
	catalog := fset.String("catalog", "", "")
	pos, err := parseInterspersed(fset, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageErr("用法：mksctl trust-record [--write] <配方>")
	}
	name := pos[0]

	fsys := e.fsys
	if *catalog != "" {
		fsys = os.DirFS(*catalog)
	}
	cat, err := recipe.Load(fsys)
	if err != nil {
		return err
	}
	r, ok := cat[name]
	if !ok {
		return fmt.Errorf("沒有配方 %s", name)
	}

	version := ""
	if r.Latest != nil {
		latest, err := e.upstream().Latest(ctx, *r.Latest)
		if err != nil {
			return fmt.Errorf("查 %s 的最新版：%w", name, err)
		}
		version = upstream.Effective(latest, r.Exclude)
	}

	// 下載檔只是為了算雜湊，放暫存目錄，結束就刪。
	dir, err := os.MkdirTemp("", "mksctl-trust-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	f := &fetch.Fetcher{Upstream: e.upstream(), Dir: dir}

	rec := fetch.TrustDB{}
	for _, a := range r.Artifacts {
		if !a.Checksum.Trust {
			continue
		}
		ent, err := f.Record(ctx, a, version)
		if err != nil {
			return fmt.Errorf("%s：%w", a.ID, err)
		}
		rec[name+"/"+a.ID] = ent
	}
	if len(rec) == 0 {
		return fmt.Errorf("配方 %s 沒有 checksum.trust 的 artifact", name)
	}

	if !*write {
		b, err := yaml.Marshal(rec)
		if err != nil {
			return err
		}
		_, err = stdout.Write(b)
		return err
	}
	return mergeTrustFile(trustHashesPath, rec)
}

// mergeTrustFile 把 rec 併進 path：同鍵取代，其餘保留。yaml.v3 輸出時鍵按字母排序，所以順序固定；
// 檔頭連續的註解與空行原樣保留。寫入先寫同目錄暫存檔再 rename，中途失敗不會留下半個檔。
func mergeTrustFile(path string, rec fetch.TrustDB) error {
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("讀 %s（要在 repo 根執行）：%w", path, err)
	}
	db, err := fetch.LoadTrust(old)
	if err != nil {
		return err
	}
	if db == nil {
		db = fetch.TrustDB{}
	}
	for k, v := range rec {
		db[k] = v
	}
	var header strings.Builder
	for _, line := range strings.SplitAfter(string(old), "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			break
		}
		header.WriteString(line)
	}
	body, err := yaml.Marshal(db)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append([]byte(header.String()), body...), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
