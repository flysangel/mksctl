package recipe

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"

	"github.com/flysangel/mksctl/internal/platform"
	"gopkg.in/yaml.v3"
)

// Load 只讀 recipes/*/recipe.yaml，其他檔（例如 .keep）一律忽略。
// 未知欄位報錯：欄位拼錯時靜默忽略，配方會悄悄少一項檢查。
func Load(fsys fs.FS) (Catalog, error) {
	files, err := fs.Glob(fsys, "recipes/*/recipe.yaml")
	if err != nil {
		return nil, err
	}
	c := Catalog{}
	for _, f := range files {
		name := path.Base(path.Dir(f))
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		var r Recipe
		if err := dec.Decode(&r); err != nil {
			return nil, fmt.Errorf("配方 %s 解析失敗：%w", name, err)
		}
		r.Name = name
		r.Versioned = r.Latest != nil
		c[name] = r
	}
	return c, nil
}

// Validate 回傳全部錯誤，不在第一個就停，讓作者一次看完。
func Validate(c Catalog, defs platform.Defs) []error {
	provided := map[string]bool{}
	for _, p := range platform.RuntimeCapabilities {
		provided[p] = true // 執行時才決定，定義檔看不到
	}
	for _, d := range defs {
		for _, p := range d.Provides {
			provided[p] = true
		}
	}

	var errs []error
	add := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	names := make([]string, 0, len(c))
	for n := range c {
		names = append(names, n)
	}
	sort.Strings(names) // 錯誤順序固定，輸出才好比對

	for _, n := range names {
		r := c[n]
		for _, d := range r.Needs {
			if _, ok := c[d]; !ok {
				add("配方 %s：needs 的 %q 不存在", n, d)
			}
		}
		layers := make([]string, 0, len(r.Steps))
		for l := range r.Steps {
			layers = append(layers, l)
		}
		sort.Strings(layers)
		for _, l := range layers {
			if _, ok := defs[l]; !ok {
				add("配方 %s：steps 的平台層 %q 沒有定義", n, l)
			}
			for _, s := range r.Steps[l] {
				if _, ok := c[s]; !ok {
					add("配方 %s：steps.%s 的 %q 不存在", n, l, s)
				}
			}
		}
		for _, req := range r.Requires {
			if !provided[req] {
				add("配方 %s：requires 的能力 %q 沒有任何平台層提供", n, req)
			}
		}
		if !slices.Contains([]string{"podman", "lxc", "vm"}, r.Venue) {
			add("配方 %s：venue %q 不是 podman、lxc、vm 之一", n, r.Venue)
		}
		if !r.Unverified {
			for _, a := range r.Artifacts {
				if !a.Checksum.any() {
					add("配方 %s：artifact %q 沒有雜湊來源，也沒標 unverified", n, a.ID)
				}
			}
		}
		if l := r.Latest; l != nil && (l.GitHub == "") == (l.URL == "") {
			add("配方 %s：latest 的 github 與 url 要擇一", n)
		}
	}
	return errs
}
