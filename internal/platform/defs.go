package platform

import (
	"fmt"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// LayerDef 是 platforms/<層>.yaml 的內容。
type LayerDef struct {
	Provides     []string `yaml:"provides"`
	Repositories []string `yaml:"repositories"`
}

// RuntimeCapabilities 是由執行環境決定、不寫在 platforms/*.yaml 的能力名稱。
// 配方驗證把它們視為一定有平台提供。
var RuntimeCapabilities = []string{"init", "kernel"}

// Defs 以層名為鍵。
type Defs map[string]LayerDef

// LoadDefs 讀 platforms/*.yaml，層名取檔名去掉副檔名。
func LoadDefs(fsys fs.FS) (Defs, error) {
	files, err := fs.Glob(fsys, "platforms/*.yaml")
	if err != nil {
		return nil, err
	}
	defs := Defs{}
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		var d LayerDef
		if err := yaml.Unmarshal(b, &d); err != nil {
			return nil, fmt.Errorf("解析 %s：%w", f, err)
		}
		defs[strings.TrimSuffix(path.Base(f), ".yaml")] = d
	}
	return defs, nil
}

// Apply 回傳填好 HasExactLayer 與 Provides 的副本。
// 沒有定義檔的層不貢獻任何能力，不算錯誤：新版本發行時可先靠通用層運作。
func (d Defs) Apply(p Platform) Platform {
	p.HasExactLayer = len(p.Layers) > 0
	if len(p.Layers) > 0 {
		_, p.HasExactLayer = d[p.Layers[0]]
	}

	p.Provides = map[string]bool{}
	for _, layer := range p.Layers {
		for _, c := range d[layer].Provides {
			p.Provides[c] = true
		}
	}
	// RuntimeCapabilities 取決於執行環境，不由層定義。容器共用 host kernel，所以不提供 kernel。
	runtime := map[string]bool{"init": p.InitRunning, "kernel": !p.InContainer}
	for _, c := range RuntimeCapabilities {
		if runtime[c] {
			p.Provides[c] = true
		}
	}
	return p
}
