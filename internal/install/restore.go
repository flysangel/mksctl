package install

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/flysangel/mksctl/internal/preflight"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/runner"
)

// Restore 把指名的配方還原。前三步與 Run 相同（平台、靜態檢查、下載驗證）；
// 不寫套件來源，也不查套件索引：還原不裝新套件。
//
// 只處理 opt.Names，不展開 needs：使用者說還原 b，不代表也要還原 b 依賴的 a。
// 執行順序是 resolve.Order 的反序，依賴者先還原，被依賴者後還原。
// 回傳值的意思與 Run 相同。沒裝過的配方照樣執行 restore（配方自己要冪等），紀錄不存在不算錯。
func Restore(ctx context.Context, deps Deps, opt Options) ([]preflight.Difference, error) {
	opt.CheckOnly = false
	sysroot := resolveSysroot(deps, opt)

	p, cat, defs, err := loadPlatform(deps, sysroot)
	if err != nil {
		return nil, err
	}
	order, err := orderRecipes(cat, defs, p, opt.Names)
	if err != nil {
		return nil, err
	}
	var recipes []recipe.Recipe
	for i := len(order) - 1; i >= 0; i-- {
		if slices.Contains(opt.Names, order[i]) {
			recipes = append(recipes, cat[order[i]])
		}
	}
	// Linux 配方的 restore 是選用的。缺函式要在執行任何配方之前發現，
	// 不然前面的配方已還原、後面的才失敗。Windows 配方的 recipe.ps1 一定有 restore（recipe.Validate 擋）。
	if p.ID != "windows" {
		for _, r := range recipes {
			if err := requireShRestore(deps.FS, r); err != nil {
				return nil, err
			}
		}
	}

	pr, diffs, err := prepare(ctx, deps, opt, sysroot, p, recipes, true)
	if err != nil || len(diffs) > 0 {
		return diffs, err
	}
	defer pr.close()

	runPath := filepath.Join(pr.fetchDir, RunFileName)
	if err := writeRunFile(runPath, RunFile{Artifacts: pr.artifacts, Layers: p.Layers, PkgmapLayers: p.Layers}); err != nil {
		return nil, err
	}
	return nil, execute(ctx, deps, p, pr.plans, opt.Params, runPath, pr.fetchDir, pr.states, runner.ActRestore)
}

// requireShRestore 確認 recipe.sh 定義了 <名稱>_restore。比對規則與 runner 組腳本時相同。
func requireShRestore(fsys fs.FS, r recipe.Recipe) error {
	data, err := fs.ReadFile(fsys, "recipes/"+r.Name+"/recipe.sh")
	if err != nil {
		return fmt.Errorf("配方 %s 不支援 restore（沒有 recipe.sh）", r.Name)
	}
	fn := r.Name + "_restore"
	re := regexp.MustCompile(`(?m)^\s*(function\s+)?` + regexp.QuoteMeta(fn) + `\s*(\(\s*\))?\s*\{`)
	if !re.Match(data) {
		return fmt.Errorf("配方 %s 不支援 restore（recipe.sh 沒有 %s）", r.Name, fn)
	}
	return nil
}
