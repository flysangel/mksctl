// Package resolve 把使用者要求的配方展開成依賴在前的執行順序。
package resolve

import (
	"fmt"
	"strings"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
)

// Order 回傳去重後、依賴在前的配方名稱。
// 同層兄弟照要求或清單的順序排，結果才可重現。
// 聚合器的 steps 取 p.Layers 中第一個有定義的層；沒有任何一層定義時，聚合器不帶步驟。
func Order(c recipe.Catalog, p platform.Platform, names []string) ([]string, error) {
	var out, stack []string
	done := map[string]bool{}
	onStack := map[string]bool{}

	var visit func(name string) error
	visit = func(name string) error {
		if done[name] {
			return nil
		}
		if onStack[name] {
			// 從該名稱第一次入堆疊處起算，路徑才是循環本身，不含前面無關的祖先。
			i := 0
			for stack[i] != name {
				i++
			}
			path := append(append([]string{}, stack[i:]...), name)
			return fmt.Errorf("循環依賴：%s", strings.Join(path, " -> "))
		}
		r, ok := c[name]
		if !ok {
			return fmt.Errorf("找不到配方 %q", name)
		}
		onStack[name] = true
		stack = append(stack, name)

		deps := append([]string{}, r.Needs...)
		for _, layer := range p.Layers {
			if steps, ok := r.Steps[layer]; ok {
				deps = append(deps, steps...)
				break
			}
		}
		for _, d := range deps {
			if err := visit(d); err != nil {
				return err
			}
		}

		stack = stack[:len(stack)-1]
		onStack[name] = false
		done[name] = true
		out = append(out, name)
		return nil
	}

	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}
