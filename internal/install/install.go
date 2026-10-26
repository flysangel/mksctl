// Package install 串起安裝前檢查與配方執行。
//
// 核心承諾：最後一道檢查（套件索引）通過前，機器上唯一允許的改動是
// 寫套件來源與更新索引。任何差異都整批回傳，讓使用者一次看完，機器保持原樣。
package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/flysangel/mksctl/internal/apkrepo"
	"github.com/flysangel/mksctl/internal/aptsource"
	"github.com/flysangel/mksctl/internal/fetch"
	"github.com/flysangel/mksctl/internal/pkgindex"
	"github.com/flysangel/mksctl/internal/pkgmap"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/preflight"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/resolve"
	"github.com/flysangel/mksctl/internal/runner"
	"github.com/flysangel/mksctl/internal/state"
	"github.com/flysangel/mksctl/internal/upstream"
)

// RunFileName 是交給配方子程序的執行資訊檔名，放在下載暫存目錄裡。
const RunFileName = "run.json"

// StatePath 是狀態檔相對 sysroot 的路徑。
const StatePath = "var/lib/mksctl/state.json"

// RunFile 是 run.json 的內容。mksctl fetch 與 pkgname 子命令讀它：
// 子程序不重查平台、不重下載，才保證拿到的是這次已驗證的檔。
type RunFile struct {
	// Artifacts 的鍵是 "<配方>/<artifact ID>"，值是已驗證檔案的絕對路徑。
	Artifacts map[string]string `json:"artifacts"`
	// Layers 是平台層，由具體到通用。
	Layers []string `json:"layers"`
	// PkgmapLayers 是查套件對照表用的層。目前與 Layers 相同，分開放是讓兩邊日後可各自調整，
	// 讀的一方不必改格式。
	PkgmapLayers []string `json:"pkgmap_layers"`
}

// Runner 執行一個配方。*runner.Runner 符合這個介面；測試換成假的。
type Runner interface {
	Run(ctx context.Context, p platform.Platform, r recipe.Recipe, env map[string]string) error
}

// 編譯期確認正式的 runner 符合介面，簽章改了這裡就編不過。
var _ Runner = (*runner.Runner)(nil)

// Deps 是 Run 用到的外部資源。全部可替換，測試才能不碰網路與真實系統。
type Deps struct {
	// FS 是 repo 根：recipes/、platforms/、pkgmap.yaml、trust/。
	FS fs.FS
	// Sysroot 是系統路徑的前綴，正式執行為 "/"。Options.Sysroot 非空時以它為準。
	Sysroot string
	// OSRelease 為 nil 時讀 <sysroot>/etc/os-release。
	OSRelease []byte
	// HTTP 給查最新版、下載與 apt 金鑰共用，測試才能數請求次數。nil 用預設值。
	HTTP *http.Client
	// GitHubBase、GitHubAPIBase 空字串時用 GitHub 正式網址；Token 見 upstream.Client。
	GitHubBase    string
	GitHubAPIBase string
	Token         string
	// Index 為 nil 時用 pkgindex.For(平台)。
	Index  pkgindex.Index
	Runner Runner
	// Now 為 nil 時用 time.Now。
	Now func() time.Time
	// Stdout 收 Info 訊息；nil 時丟棄。
	Stdout io.Writer
	// FetchDir 是下載暫存目錄。不存在時 Run 會建；結束時清空內容。
	// 空字串時在 <sysroot>/var/cache/mksctl/fetch/ 底下建一個這次專用的目錄，結束時整個刪掉。
	FetchDir string
	// 以下原樣交給 fetch.Fetcher。TrustFS 為 nil 時用 FS。
	Arch        string
	TrustFS     fs.FS
	TUFCacheDir string
	TrustedRoot root.TrustedMaterial
}

// Options 是使用者這次的要求。
type Options struct {
	Names           []string
	Params          map[string]string
	AllowUnverified bool
	Sysroot         string
	// CheckOnly 為 true 時，跑完第 4 步（apt 來源衝突）就返回，不寫來源、不更新索引、
	// 不執行配方、不存狀態檔。套件名稱要等寫入來源後才查得到，所以 check 不含第 6 步。
	CheckOnly bool
}

// fetcherHook 只給測試用：讀出 Run 交給 fetcher 的設定，不必真的跑 sigstore 與網路。
var fetcherHook func(*fetch.Fetcher)

// plan 是一個配方這次要怎麼處理。
type plan struct {
	r       recipe.Recipe
	version string
	action  state.Action
}

// Run 照固定順序做完檢查再安裝。回傳值有三種：
// 有差異時回 (diffs, nil)；操作錯誤回 (nil, err)；成功回 (nil, nil)。
func Run(ctx context.Context, deps Deps, opt Options) (diffs []preflight.Difference, err error) {
	sysroot := opt.Sysroot
	if sysroot == "" {
		sysroot = deps.Sysroot
	}
	if sysroot == "" {
		sysroot = "/"
	}

	// 1. 讀平台、排順序。這一步只讀檔。
	p, cat, defs, err := loadPlatform(deps, sysroot)
	if err != nil {
		return nil, err
	}
	if errs := recipe.Validate(cat, defs); len(errs) > 0 {
		return nil, fmt.Errorf("配方目錄有錯：%w", errors.Join(errs...))
	}
	order, err := resolve.Order(cat, p, opt.Names)
	if err != nil {
		return nil, err
	}
	recipes := make([]recipe.Recipe, len(order))
	for i, n := range order {
		recipes[i] = cat[n]
	}

	// 2. 靜態檢查：不碰網路與系統。
	if d := preflight.Static(preflight.StaticInput{
		Platform: p, Recipes: recipes, Params: opt.Params, AllowUnverified: opt.AllowUnverified,
	}); len(d) > 0 {
		return d, nil
	}

	pm, trust, err := loadTables(deps.FS)
	if err != nil {
		return nil, err
	}
	statePath := filepath.Join(sysroot, StatePath)
	st, err := state.Load(statePath)
	if err != nil {
		return nil, err
	}

	// 3. 查最新版並下載驗證。暫存目錄不算改機器：結束時一定清掉。
	fetchDir, cleanup, err := prepareFetchDir(deps.FetchDir, sysroot, opt.CheckOnly)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	up := &upstream.Client{HTTP: deps.HTTP, GitHubBase: deps.GitHubBase, GitHubAPIBase: deps.GitHubAPIBase, Token: deps.Token}
	trustFS := deps.TrustFS
	if trustFS == nil {
		trustFS = deps.FS
	}
	tufDir := deps.TUFCacheDir
	if opt.CheckOnly {
		// check 承諾不寫任何東西，sigstore 的 TUF 快取也一樣：改放這次的暫存目錄，結束時一起清掉。
		// 代價是 check 每次都從頭抓 TUF 中繼資料。
		tufDir = filepath.Join(fetchDir, ".tuf-cache")
	}
	fetcher := &fetch.Fetcher{
		Upstream: up, Trust: trust, Arch: deps.Arch,
		TrustFS: trustFS, TUFCacheDir: tufDir, TrustedRoot: deps.TrustedRoot,
	}
	if fetcherHook != nil {
		fetcherHook(fetcher)
	}
	plans, artifacts, diffs, err := fetchAll(ctx, fetcher, up, st, recipes, fetchDir, opt.AllowUnverified)
	if err != nil {
		return nil, err
	}
	if len(diffs) > 0 {
		preflight.Sort(diffs)
		return diffs, nil
	}

	// 4. 查 apt 來源衝突。這時還沒寫任何東西。
	for _, pl := range plans {
		if pl.action != state.Run || pl.r.AptSource == nil {
			continue
		}
		cs, err := aptsource.FindConflicts(sysroot, *pl.r.AptSource, p)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			diffs = append(diffs, preflight.Difference{
				Recipe: pl.r.Name, Kind: preflight.AptSourceConflict,
				Detail: fmt.Sprintf("%s：%s\nFix: %s", c.File, c.Reason, c.Fix),
			})
		}
	}
	if len(diffs) > 0 {
		preflight.Sort(diffs)
		return diffs, nil
	}
	if opt.CheckOnly {
		return nil, nil
	}

	// 5. 寫套件來源與 alpine 套件庫，再更新索引。這是檢查通過前唯一會改機器的步驟：
	//    不先寫來源，就無法確認來源裡的套件（例如 docker-ce）查得到。
	idx := deps.Index
	if idx == nil {
		idx = pkgindex.For(p)
	}
	if err := prepareSources(ctx, deps.HTTP, sysroot, p, defs, plans, idx); err != nil {
		return nil, err
	}

	// 6. 問套件索引。
	names := map[string][]string{}
	for _, pl := range plans {
		if pl.action == state.Run && len(pl.r.Packages) > 0 {
			names[pl.r.Name] = pl.r.Packages
		}
	}
	if d := preflight.Packages(ctx, idx, pm, p, names); len(d) > 0 {
		return d, nil
	}

	// 7. 依序執行。每成功一個就存狀態檔，後面失敗時，重跑可從失敗的配方接著做。
	runPath := filepath.Join(fetchDir, RunFileName)
	if err := writeRunFile(runPath, RunFile{Artifacts: artifacts, Layers: p.Layers, PkgmapLayers: p.Layers}); err != nil {
		return nil, err
	}
	return nil, execute(ctx, deps, p, plans, opt.Params, runPath, st, statePath)
}

func loadPlatform(deps Deps, sysroot string) (platform.Platform, recipe.Catalog, platform.Defs, error) {
	osr := deps.OSRelease
	if osr == nil {
		b, err := os.ReadFile(filepath.Join(sysroot, "etc/os-release"))
		if err != nil {
			return platform.Platform{}, nil, nil, fmt.Errorf("讀不到 os-release：%w", err)
		}
		osr = b
	}
	p, err := platform.Parse(osr)
	if err != nil {
		return platform.Platform{}, nil, nil, err
	}
	p.InitRunning, p.InContainer = platform.DetectRuntime(sysroot)
	defs, err := platform.LoadDefs(deps.FS)
	if err != nil {
		return platform.Platform{}, nil, nil, err
	}
	p = defs.Apply(p)
	cat, err := recipe.Load(deps.FS)
	if err != nil {
		return platform.Platform{}, nil, nil, err
	}
	return p, cat, defs, nil
}

// loadTables 讀套件對照表與 trust。檔不存在當成空表：兩者都是選用的資料。
func loadTables(fsys fs.FS) (pkgmap.Map, fetch.TrustDB, error) {
	pm := pkgmap.Map{}
	if b, err := fs.ReadFile(fsys, "pkgmap.yaml"); err == nil {
		if pm, err = pkgmap.Load(b); err != nil {
			return nil, nil, fmt.Errorf("解析 pkgmap.yaml：%w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	var trust fetch.TrustDB
	if b, err := fs.ReadFile(fsys, "trust/hashes.yaml"); err == nil {
		if trust, err = fetch.LoadTrust(b); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	return pm, trust, nil
}

// prepareFetchDir 回傳暫存目錄與清理函式。指定的目錄只清內容、不刪本身，
// 呼叫端可能要重用它；自己建的目錄整個刪掉。
// checkOnly 時改用系統暫存目錄：check 承諾不改機器，連 sysroot 底下的快取目錄都不建。
func prepareFetchDir(dir, sysroot string, checkOnly bool) (string, func(), error) {
	if dir == "" && checkOnly {
		d, err := os.MkdirTemp("", "mksctl-check-*")
		if err != nil {
			return "", nil, err
		}
		return d, func() { _ = os.RemoveAll(d) }, nil
	}
	if dir == "" {
		base := filepath.Join(sysroot, "var/cache/mksctl/fetch")
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", nil, err
		}
		d, err := os.MkdirTemp(base, "run-*")
		if err != nil {
			return "", nil, err
		}
		return d, func() { _ = os.RemoveAll(d) }, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	return dir, func() {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}, nil
}

// fetchAll 對每個配方決定版本與動作，並下載要執行的配方的 artifacts。
// 失敗全部收集成差異再一起回傳，使用者才能一次看到所有下載問題。
func fetchAll(ctx context.Context, f *fetch.Fetcher, up *upstream.Client, st state.File,
	recipes []recipe.Recipe, fetchDir string, allowUnverified bool,
) ([]plan, map[string]string, []preflight.Difference, error) {
	var plans []plan
	var diffs []preflight.Difference
	artifacts := map[string]string{}
	for _, r := range recipes {
		version := ""
		if r.Latest != nil {
			latest, err := up.Latest(ctx, *r.Latest)
			if err != nil {
				diffs = append(diffs, preflight.Difference{Recipe: r.Name, Kind: preflight.FetchFailed, Detail: fmt.Sprintf("查最新版失敗：%v", err)})
				continue
			}
			version = upstream.Effective(latest, r.Exclude)
		}
		act := state.Decide(st, r, version)
		plans = append(plans, plan{r: r, version: version, action: act})
		if act == state.Skip {
			continue // 略過的配方不下載，也不寫來源。
		}
		// 每個配方一個子目錄：不同配方的 artifact 檔名可能相同。
		f.Dir = filepath.Join(fetchDir, r.Name)
		if err := os.MkdirAll(f.Dir, 0o755); err != nil {
			return nil, nil, nil, err
		}
		// 只對標了 unverified 的配方放行；其他配方即使帶了旗標也照常驗證。
		f.AllowUnverified = r.Unverified && allowUnverified
		for _, a := range r.Artifacts {
			res, err := f.Get(ctx, a, version)
			if err != nil {
				diffs = append(diffs, preflight.Difference{Recipe: r.Name, Kind: preflight.FetchFailed, Detail: fmt.Sprintf("%s：%v", a.ID, err)})
				continue
			}
			artifacts[r.Name+"/"+a.ID] = res.Path
		}
	}
	// 使用者中斷不是差異，是操作錯誤；不然會被當成「下載失敗」誤導使用者去查網路。
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	return plans, artifacts, diffs, nil
}

func prepareSources(ctx context.Context, hc *http.Client, sysroot string, p platform.Platform,
	defs platform.Defs, plans []plan, idx pkgindex.Index,
) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	for _, pl := range plans {
		if pl.action != state.Run || pl.r.AptSource == nil {
			continue
		}
		if err := aptsource.Write(ctx, sysroot, *pl.r.AptSource, p, hc); err != nil {
			return fmt.Errorf("配方 %s 寫 apt 來源：%w", pl.r.Name, err)
		}
	}
	var repos []string
	for _, l := range p.Layers {
		for _, r := range defs[l].Repositories {
			if !slices.Contains(repos, r) {
				repos = append(repos, r)
			}
		}
	}
	if len(repos) > 0 {
		if _, err := apkrepo.Ensure(sysroot, repos); err != nil {
			return fmt.Errorf("寫 alpine 套件庫：%w", err)
		}
	}
	if slices.Contains(p.Layers, "apt") || slices.Contains(p.Layers, "apk") {
		if err := idx.Refresh(ctx); err != nil {
			return err
		}
	}
	return nil
}

func writeRunFile(path string, rf RunFile) error {
	b, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func execute(ctx context.Context, deps Deps, p platform.Platform, plans []plan, params map[string]string,
	runPath string, st state.File, statePath string,
) error {
	if deps.Runner == nil {
		return errors.New("install: 沒有設定 Runner")
	}
	out := deps.Stdout
	if out == nil {
		out = io.Discard
	}
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	for _, pl := range plans {
		r := pl.r
		if pl.action == state.Skip {
			fmt.Fprintf(out, "Info: %s 已是最新版 %s，略過\n", r.Name, pl.version)
			continue
		}
		env := map[string]string{
			"MKSCTL_RUN":     runPath,
			"MKSCTL_RECIPE":  r.Name,
			"MKSCTL_VERSION": pl.version,
		}
		// 參數只給宣告它的配方，避免某個配方意外讀到給別人的值（例如密碼）。
		for _, k := range r.Params {
			if v, ok := params[k]; ok {
				env["MKSCTL_PARAM_"+k] = v
			}
		}
		if err := deps.Runner.Run(ctx, p, r, env); err != nil {
			return err
		}
		st.Recipes[r.Name] = state.Entry{Version: pl.version, InstalledAt: now()}
		if err := st.Save(statePath); err != nil {
			return fmt.Errorf("配方 %s 已裝好，但寫狀態檔失敗：%w", r.Name, err)
		}
	}
	return nil
}
