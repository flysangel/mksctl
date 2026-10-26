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
	// AptMirror 是發行版自己的套件庫要改指向的鏡像。只在這次要裝 apt 套件時寫入，
	// 只下載執行檔的安裝不該改使用者的來源。
	AptMirror *AptMirror `yaml:"apt_mirror"`
	// AptConf 的鍵是 /etc/apt/apt.conf.d/ 底下的檔名，值是檔案內容。寫入時機同 AptMirror。
	AptConf map[string]string `yaml:"apt_conf"`
}

// AptMirror 描述發行版套件庫的鏡像（deb822 欄位）。Suites 每一項是樣板，可用 {{.Codename}}。
type AptMirror struct {
	URIs       string   `yaml:"uris"`
	Suites     []string `yaml:"suites"`
	Components string   `yaml:"components"`
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

// AptMirror 回傳最具體一層的鏡像設定；沒有任何一層設定時回 nil。
// 取單一層而不合併：鏡像的欄位彼此相依（URI 與 suite 要對得上），混搭兩層會產出壞來源。
func (d Defs) AptMirror(p Platform) *AptMirror {
	for _, l := range p.Layers {
		if m := d[l].AptMirror; m != nil {
			return m
		}
	}
	return nil
}

// AptConf 回傳最具體一層的 apt 設定；沒有任何一層設定時回 nil。
// 跟 AptMirror 一樣取單一層，具體層才能整組取代通用層的設定。
func (d Defs) AptConf(p Platform) map[string]string {
	for _, l := range p.Layers {
		if c := d[l].AptConf; c != nil {
			return c
		}
	}
	return nil
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
