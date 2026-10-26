package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// ErrNoChecksum：這個 provider 的上游不公布雜湊。配方要改用 checksum.file 之類的來源。
var ErrNoChecksum = errors.New("upstream: 這個 provider 不提供雜湊")

// Provider 處理版本號或雜湊不在 GitHub 轉址、也不是純文字檔的上游，
// 例如 go.dev 的 JSON 或 TDF 的目錄清單。配方用名稱指定，不必為每個上游加 YAML 欄位。
type Provider interface {
	// Latest 回傳最新版本字串。
	Latest(ctx context.Context, c *Client) (string, error)
	// Checksum 回傳 version 版中 filename 的 sha256。上游不公布雜湊時回 ErrNoChecksum。
	Checksum(ctx context.Context, c *Client, version, filename string) (string, error)
}

// Providers 以配方裡寫的名稱為鍵。recipe.Validate 用它擋掉拼錯的名稱。
var Providers = map[string]Provider{
	"godev":           godev{URL: "https://go.dev/dl/?mode=json"},
	"jetbrains-iic":   jetbrains{URL: "https://data.services.jetbrains.com/products/releases?code=IIC&latest=true&type=release", Code: "IIC"},
	"tdf-libreoffice": tdf{URL: "https://download.documentfoundation.org/libreoffice/stable/"},
}

// bodyLimit 是 provider 回應的上限。go.dev 的 JSON 約數十 KB；
// 限制讀取量，避免網址填錯時把大檔讀進記憶體。
const bodyLimit = 4 << 20

// body 取回 u 的內文。同一個 Client 對同一個網址只打一次：
// godev 的 Latest 與 Checksum 讀同一份 JSON，不必下載兩次。
func (c *Client) body(ctx context.Context, u string) (string, error) {
	return c.memo(ctx, "body:"+u, func() (string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		resp, err := c.HTTPClient().Do(req)
		if err != nil {
			return "", fmt.Errorf("upstream: 查 %s：%w", u, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("upstream: %s 回 HTTP %d", u, resp.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
		if err != nil {
			return "", fmt.Errorf("upstream: 讀 %s：%w", u, err)
		}
		return string(b), nil
	})
}

// godev 讀 go.dev/dl 的 JSON。清單由新到舊，可能夾著 rc 或 beta（stable: false）。
type godev struct{ URL string }

type godevRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
	Files   []struct {
		Filename string `json:"filename"`
		SHA256   string `json:"sha256"`
	} `json:"files"`
}

func (g godev) releases(ctx context.Context, c *Client) ([]godevRelease, error) {
	b, err := c.body(ctx, g.URL)
	if err != nil {
		return nil, err
	}
	var rs []godevRelease
	if err := json.Unmarshal([]byte(b), &rs); err != nil {
		return nil, fmt.Errorf("upstream: 解析 %s：%w", g.URL, err)
	}
	return rs, nil
}

// Latest 取第一筆 stable，跳過 rc 與 beta：配方只裝正式版。
func (g godev) Latest(ctx context.Context, c *Client) (string, error) {
	rs, err := g.releases(ctx, c)
	if err != nil {
		return "", err
	}
	for _, r := range rs {
		if r.Stable && r.Version != "" {
			return r.Version, nil
		}
	}
	return "", fmt.Errorf("upstream: %s 沒有 stable 版", g.URL)
}

// Checksum 在同版本的 files 裡找檔名完全相符的一筆。不限 stable：
// exclude 可能把版本換成清單裡的另一筆。清單只列最近幾版，太舊的版本會查不到。
func (g godev) Checksum(ctx context.Context, c *Client, version, filename string) (string, error) {
	rs, err := g.releases(ctx, c)
	if err != nil {
		return "", err
	}
	for _, r := range rs {
		if r.Version != version {
			continue
		}
		for _, f := range r.Files {
			if f.Filename == filename && f.SHA256 != "" {
				return f.SHA256, nil
			}
		}
		return "", fmt.Errorf("upstream: %s 的 %s 沒有 %s", g.URL, version, filename)
	}
	return "", fmt.Errorf("upstream: %s 沒有 %s 版", g.URL, version)
}

// jetbrains 讀 JetBrains 的 releases API。回應以產品代碼為鍵，latest=true 時只有一筆。
type jetbrains struct{ URL, Code string }

func (j jetbrains) Latest(ctx context.Context, c *Client) (string, error) {
	b, err := c.body(ctx, j.URL)
	if err != nil {
		return "", err
	}
	var m map[string][]struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(b), &m); err != nil {
		return "", fmt.Errorf("upstream: 解析 %s：%w", j.URL, err)
	}
	rs := m[j.Code]
	if len(rs) == 0 || rs[0].Version == "" {
		return "", fmt.Errorf("upstream: %s 沒有 %s 的版本", j.URL, j.Code)
	}
	return rs[0].Version, nil
}

// Checksum：API 只給 checksumLink，配方直接寫 file: "{{.URL}}.sha256" 比較單純。
func (jetbrains) Checksum(context.Context, *Client, string, string) (string, error) {
	return "", ErrNoChecksum
}

// tdf 讀 TDF 下載站的目錄清單，取版本號最大的子目錄。
type tdf struct{ URL string }

// tdfDir 只認純數字加點的子目錄連結，例如 26.8.0/。上層目錄與其他名稱略過。
var tdfDir = regexp.MustCompile(`href="(\d+(?:\.\d+)*)/"`)

func (t tdf) Latest(ctx context.Context, c *Client) (string, error) {
	b, err := c.body(ctx, t.URL)
	if err != nil {
		return "", err
	}
	best := ""
	for _, m := range tdfDir.FindAllStringSubmatch(b, -1) {
		if best == "" || versionLess(best, m[1]) {
			best = m[1]
		}
	}
	if best == "" {
		return "", fmt.Errorf("upstream: %s 找不到版本目錄", t.URL)
	}
	return best, nil
}

// Checksum：下載站的每個檔旁邊都有 .sha256，配方用 checksum.file。
func (tdf) Checksum(context.Context, *Client, string, string) (string, error) {
	return "", ErrNoChecksum
}

// versionLess 逐段比數字。字串比較會把 3.6.7 排在 26.8.0 後面。
// 段數不同時，前面相同的段數較少者較小（26.8 < 26.8.0）。
func versionLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, _ := strconv.Atoi(as[i]) // tdfDir 已保證是數字
		y, _ := strconv.Atoi(bs[i])
		if x != y {
			return x < y
		}
	}
	return len(as) < len(bs)
}
