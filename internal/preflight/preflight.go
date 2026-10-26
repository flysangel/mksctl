// Package preflight 做安裝前檢查。它只回報差異，不改機器；
// 呼叫端看到任何差異就整批停下，讓使用者一次看完要處理的事。
package preflight

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/flysangel/mksctl/internal/identity"
	"github.com/flysangel/mksctl/internal/pkgindex"
	"github.com/flysangel/mksctl/internal/pkgmap"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
)

// Kind 是差異的種類。值會原樣印給使用者，也給腳本比對，不要改字。
type Kind string

const (
	NoExactPlatform   Kind = "no-exact-platform"
	MissingCapability Kind = "missing-capability"
	UnknownParam      Kind = "unknown-param"
	Unverified        Kind = "unverified"
	FetchFailed       Kind = "fetch-failed"
	AptSourceConflict Kind = "apt-source-conflict"
	PackageNotFound   Kind = "package-not-found"
	// 以下四種只在 Windows 產生。NoUserProfile 由 install 產生：LOCALAPPDATA 是空的，user 配方沒有狀態檔可寫。
	NeedsElevation  Kind = "needs-elevation"
	SystemUserScope Kind = "system-user-scope"
	WrongPlatform   Kind = "wrong-platform"
	NoUserProfile   Kind = "no-user-profile"
)

// Difference 是一項差異。Recipe 為空表示跟單一配方無關（例如平台檔、參數）。
type Difference struct {
	Recipe string
	Kind   Kind
	Detail string
}

// StaticInput 是不碰網路與系統就能檢查的輸入。Recipes 是已排好順序的全部配方。
type StaticInput struct {
	Platform        platform.Platform
	Recipes         []recipe.Recipe
	Params          map[string]string
	AllowUnverified bool
	// Identity 與 GOOS 只給 Windows 檢查用：身分由呼叫端取得，這裡不讀機器，
	// 所以 SYSTEM 首開機的整批停止不需要碰任何機器上的檔。
	Identity identity.Identity
	GOOS     string
}

// Static 檢查平台檔、能力、參數與 unverified。
func Static(in StaticInput) []Difference {
	var diffs []Difference
	p := in.Platform

	// 平台檔缺了是整台機器的事，不是某個配方的事，所以只報一次。
	if !p.HasExactLayer {
		layer := ""
		if len(p.Layers) > 0 {
			layer = p.Layers[0]
		}
		diffs = append(diffs, Difference{Kind: NoExactPlatform, Detail: layer})
	}

	declared := map[string]bool{}
	for _, r := range in.Recipes {
		if in.GOOS == "windows" {
			diffs = append(diffs, windowsIdentity(in.Identity, r)...)
		}
		for _, req := range r.Requires {
			if !p.Provides[req] {
				diffs = append(diffs, Difference{Recipe: r.Name, Kind: MissingCapability, Detail: req})
			}
		}
		for _, k := range r.Params {
			declared[k] = true
		}
		if r.Unverified && !in.AllowUnverified {
			diffs = append(diffs, Difference{Recipe: r.Name, Kind: Unverified, Detail: "下載檔沒有雜湊來源，要加 --allow-unverified 才能裝"})
		}
	}

	// 參數名拼錯時配方會拿到空值，靜默裝出錯的結果，所以沒有任何配方宣告的鍵一律擋。
	keys := make([]string, 0, len(in.Params))
	for k := range in.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !declared[k] {
			diffs = append(diffs, Difference{Kind: UnknownParam, Detail: k})
		}
	}
	Sort(diffs)
	return diffs
}

// Packages 用 names（配方名 → 邏輯套件名）查對照表，再問套件索引。
// 索引要先 Refresh 過，剛寫入的來源才查得到。
func Packages(ctx context.Context, idx pkgindex.Index, m pkgmap.Map, p platform.Platform, names map[string][]string) []Difference {
	var diffs []Difference
	for rname, logicals := range names {
		for _, logical := range logicals {
			real, err := m.Lookup(logical, p.Layers)
			if errors.Is(err, pkgmap.ErrUnavailable) {
				diffs = append(diffs, Difference{Recipe: rname, Kind: PackageNotFound,
					Detail: fmt.Sprintf("%s (%s 沒有這個套件)", logical, unavailableLayer(m, logical, p.Layers))})
				continue
			}
			if err != nil {
				diffs = append(diffs, Difference{Recipe: rname, Kind: PackageNotFound, Detail: fmt.Sprintf("%s (%v)", logical, err)})
				continue
			}
			for _, name := range real {
				ok, err := idx.Has(ctx, name)
				switch {
				case err != nil:
					// 查詢失敗也算差異：沒確認套件在，就不能保證裝得起來。
					diffs = append(diffs, Difference{Recipe: rname, Kind: PackageNotFound, Detail: fmt.Sprintf("%s (查詢失敗：%v)", name, err)})
				case !ok:
					diffs = append(diffs, Difference{Recipe: rname, Kind: PackageNotFound, Detail: name})
				}
			}
		}
	}
	Sort(diffs)
	return diffs
}

// unavailableLayer 找出把 logical 標成 null 的那一層，讓使用者知道是哪個平台缺。
// 規則與 pkgmap.Lookup 相同：取 layers 中第一個有定義的層。
func unavailableLayer(m pkgmap.Map, logical string, layers []string) string {
	for _, l := range layers {
		if _, ok := m[logical][l]; ok {
			return l
		}
	}
	return ""
}

// Sort 依配方再依種類排序。用穩定排序，同配方同種類的差異保留產生順序，
// 也就是配方裡寫的順序。
func Sort(diffs []Difference) {
	sort.SliceStable(diffs, func(i, j int) bool {
		if diffs[i].Recipe != diffs[j].Recipe {
			return diffs[i].Recipe < diffs[j].Recipe
		}
		return diffs[i].Kind < diffs[j].Kind
	})
}

// windowsIdentity 檢查一個配方在目前身分下能不能裝。
// SYSTEM 的 HKCU 是 S-1-5-18 自己的 hive，寫了互動使用者吃不到，所以 user 配方一律擋。
func windowsIdentity(id identity.Identity, r recipe.Recipe) []Difference {
	if !r.HasPs1 && len(r.Registry) == 0 {
		return []Difference{{Recipe: r.Name, Kind: WrongPlatform, Detail: "這個配方沒有 recipe.ps1 或 registry，不是 Windows 配方"}}
	}
	if r.Scope == "user" {
		if id.System {
			return []Difference{{Recipe: r.Name, Kind: SystemUserScope, Detail: "scope: user 的配方不能以 SYSTEM 執行；請以目標使用者身分登入後再跑"}}
		}
		return nil
	}
	if !id.Elevated {
		return []Difference{{Recipe: r.Name, Kind: NeedsElevation, Detail: "machine 配方需要系統管理員權限；請用「以系統管理員身分執行」開啟終端機"}}
	}
	return nil
}
