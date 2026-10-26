// Package recipe 載入並驗證 recipes/<名稱>/recipe.yaml 組成的配方目錄。
package recipe

import "github.com/flysangel/mksctl/internal/upstream"

// Recipe 是一份配方。Name 取自目錄名，不是 YAML 欄位，避免兩處不一致。
type Recipe struct {
	Name string `yaml:"-"`
	// Needs 是必須先跑完的其他配方。
	Needs []string `yaml:"needs"`
	// Steps 以平台層名為鍵，值是該層要先跑的配方，依序執行。
	Steps    map[string][]string `yaml:"steps"`
	Requires []string            `yaml:"requires"`
	Params   []string            `yaml:"params"`
	// Packages 是配方會用 pkg_install 裝的邏輯名稱，給安裝前檢查用。
	Packages  []string          `yaml:"packages"`
	Latest    *LatestSpec       `yaml:"latest"`
	Artifacts []Artifact        `yaml:"artifacts"`
	Exclude   map[string]string `yaml:"exclude"`
	// Unverified 明示此配方的下載沒有雜湊來源，由作者擔保。
	Unverified bool   `yaml:"unverified"`
	Venue      string `yaml:"venue"`
	// AptSources 是要加的第三方 apt 來源。用清單是因為有的配方要兩個來源（例如套件與它的相依）。
	// 舊的單筆欄位 apt_source 不再接受：KnownFields 會報錯，不會被靜默忽略。
	AptSources []AptSource `yaml:"apt_sources"`
	// Versioned 由 Latest 推得（不是 YAML 欄位）：沒有 latest 的配方每次都執行。
	Versioned bool `yaml:"-"`
}

// LatestSpec 的 GitHub、URL、Provider 三選一。型別定義在 upstream，理由見該處註解。
type LatestSpec = upstream.LatestSpec

// Artifact 是配方要下載的一個檔。URL 是含 {{.Version}}、{{.Arch}} 的樣板。
type Artifact struct {
	ID       string       `yaml:"id"`
	URL      string       `yaml:"url"`
	Checksum ChecksumSpec `yaml:"checksum"`
}

// ChecksumSpec 列出雜湊來源，至少要有一種（除非配方標 unverified）。
// 試的順序見 fetch.Fetcher.Get。
type ChecksumSpec struct {
	Sigstore *SigstoreSpec `yaml:"sigstore"`
	// Provider 是 upstream.Providers 裡的名稱，給在 API 裡公布雜湊的上游（例如 go.dev）。
	Provider     string `yaml:"provider"`
	File         string `yaml:"file"`
	Trust        bool   `yaml:"trust"`
	GitHubDigest bool   `yaml:"github_digest"`
}

func (c ChecksumSpec) any() bool {
	return c.Sigstore != nil || c.Provider != "" || c.File != "" || c.Trust || c.GitHubDigest
}

// SigstoreSpec 描述 sigstore 簽章驗證。Key 與 Identity/Issuer 擇一：
// 上游用 cosign 金鑰簽時填 Key；用 keyless（Fulcio 憑證）簽時填 Identity 與 Issuer。
type SigstoreSpec struct {
	Bundle   string `yaml:"bundle"`
	Identity string `yaml:"identity"`
	Issuer   string `yaml:"issuer"`
	// Key 是嵌入 FS 裡的公鑰路徑，例如 trust/keys/kube-linter-cosign.pub。
	// 公鑰釘在 repo，不向上游抓：能換掉上游檔案的人，也能換掉上游的公鑰。
	Key string `yaml:"key"`
}

// AptSource 描述要加的 apt 來源（deb822 格式）。
type AptSource struct {
	File       string `yaml:"file"`
	URIs       string `yaml:"uris"`
	Suites     string `yaml:"suites"`
	Components string `yaml:"components"`
	SignedBy   string `yaml:"signed_by"`
	KeyURL     string `yaml:"key_url"`
}

// Catalog 以配方名為鍵。
type Catalog map[string]Recipe
