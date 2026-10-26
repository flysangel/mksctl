// Package fetch 下載配方的檔案，並用上游或 trust/ 提供的雜湊驗證。
// 原始碼不放任何雜湊值：雜湊一律在執行時從上游取得，或讀 trust/hashes.yaml。
package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/sigstore/sigstore-go/pkg/root"

	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/upstream"
)

var (
	// ErrUnverified：沒有可用的雜湊來源，或 sigstore 驗證失敗。
	ErrUnverified = errors.New("fetch: 下載檔未通過驗證")
	// ErrTrustMismatch：下載檔與 trust/hashes.yaml 的記錄不符。
	ErrTrustMismatch = errors.New("fetch: 與 trust 記錄不符")
	// ErrChecksumMismatch：下載檔與上游公布的雜湊不符。
	ErrChecksumMismatch = errors.New("fetch: 雜湊不符")
)

const (
	attempts = 3
	// 小檔（checksum、bundle、API 回應）的上限，避免網址填錯時讀進整個大檔。
	smallLimit = 16 << 20
)

// retryDelay 是重試間隔的基數，第 n 次重試等 n 倍。
var retryDelay = 200 * time.Millisecond

// Fetcher 下載並驗證檔案。
type Fetcher struct {
	Upstream *upstream.Client
	Trust    TrustDB
	Dir      string
	// Arch 是樣板的 {{.Arch}}，預設 amd64。
	Arch string
	// TUFCacheDir 是 sigstore TUF 的快取目錄，CLI 傳 /var/cache/mksctl/tuf/。
	TUFCacheDir string
	// TrustedRoot 非 nil 時直接用，不走 TUF；給離線測試用。
	TrustedRoot root.TrustedMaterial
	// TrustFS 是嵌入的 FS（repo 根），讀 SigstoreSpec.Key 指的公鑰。
	TrustFS fs.FS
	// AllowUnverified 只放行「完全沒設雜湊來源」的檔，對應配方的 unverified 與 --allow-unverified。
	// 有設來源卻驗證失敗的檔照樣擋：作者寫了來源，失敗就代表檔案有問題。
	AllowUnverified bool

	rootOnce sync.Once
	root     root.TrustedMaterial
	rootErr  error
}

// Result 是下載完成且驗證通過的檔案。
type Result struct {
	ID, Path, Version, SHA256 string
}

type tmplData struct {
	Version, Arch, URL string
}

func (f *Fetcher) up() *upstream.Client {
	if f.Upstream != nil {
		return f.Upstream
	}
	return &upstream.Client{}
}

func render(name, text string, d tmplData) (string, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("fetch: 樣板 %s：%w", name, err)
	}
	var b strings.Builder
	if err := t.Execute(&b, d); err != nil {
		return "", fmt.Errorf("fetch: 樣板 %s：%w", name, err)
	}
	return b.String(), nil
}

// Get 下載 a 到 Dir，依序試：
//  1. Sigstore：有設就適用。
//  2. Provider：有設就適用。上游的 API 公布雜湊，和 File 同級，排在 File 前面。
//  3. File：有設就適用。
//  4. Trust：trust/hashes.yaml 有這個網址的紀錄才適用，沒有就往下。
//  5. GitHubDigest：網址是 GitHub release 下載點才適用，只當備援。
//
// 第一個「有設定且適用」的來源決定成敗，不再往下試：作者寫了來源卻驗不過，代表檔案有問題，
// 退到較弱的來源等於把強的驗證關掉。失敗時刪掉下載檔，不留未驗證的檔給後續步驟誤用。
func (f *Fetcher) Get(ctx context.Context, a recipe.Artifact, version string) (Result, error) {
	d := tmplData{Version: version, Arch: f.Arch}
	if d.Arch == "" {
		d.Arch = "amd64"
	}
	u, err := render("url", a.URL, d)
	if err != nil {
		return Result{}, err
	}
	d.URL = u
	pu, err := url.Parse(u)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: 網址 %s：%w", u, err)
	}
	name := path.Base(pu.Path)
	if name == "/" || name == "." {
		return Result{}, fmt.Errorf("fetch: 網址 %s 取不到檔名", u)
	}

	tmp, sum, size, err := f.download(ctx, u)
	if err != nil {
		return Result{}, err
	}
	if !f.AllowUnverified || hasSource(a.Checksum) {
		if err := f.verify(ctx, a.Checksum, d, name, sum, size); err != nil {
			_ = os.Remove(tmp)
			return Result{}, fmt.Errorf("%s（%s）：%w", a.ID, u, err)
		}
	}
	dst := filepath.Join(f.Dir, name)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return Result{}, fmt.Errorf("fetch: 移到 %s：%w", dst, err)
	}
	return Result{ID: a.ID, Path: dst, Version: version, SHA256: hex.EncodeToString(sum)}, nil
}

func (f *Fetcher) verify(ctx context.Context, c recipe.ChecksumSpec, d tmplData, name string, sum []byte, size int64) error {
	got := hex.EncodeToString(sum)
	if c.Sigstore != nil {
		bu, err := render("sigstore.bundle", c.Sigstore.Bundle, d)
		if err != nil {
			return err
		}
		b, err := f.getSmall(ctx, bu, nil)
		if err != nil {
			return fmt.Errorf("%w：取 bundle：%v", ErrUnverified, err)
		}
		return f.verifySigstore(ctx, b, sum, *c.Sigstore)
	}
	if c.Provider != "" {
		p, ok := upstream.Providers[c.Provider]
		if !ok {
			return fmt.Errorf("%w：沒有名為 %q 的 provider", ErrUnverified, c.Provider)
		}
		// 版本傳樣板的 Version，也就是 exclude 換過之後實際要裝的版本。
		want, err := p.Checksum(ctx, f.up(), d.Version, name)
		if err != nil {
			return fmt.Errorf("%w：provider %s：%v", ErrUnverified, c.Provider, err)
		}
		return compare(want, got, ErrChecksumMismatch)
	}
	if c.File != "" {
		fu, err := render("checksum.file", c.File, d)
		if err != nil {
			return err
		}
		b, err := f.getSmall(ctx, fu, nil)
		if err != nil {
			return fmt.Errorf("%w：取 checksum 檔：%v", ErrUnverified, err)
		}
		want, err := findChecksum(b, name)
		if err != nil {
			return fmt.Errorf("%w：%s：%v", ErrUnverified, fu, err)
		}
		return compare(want, got, ErrChecksumMismatch)
	}
	if c.Trust {
		if e, ok := f.Trust.lookup(d.URL); ok {
			if e.Size != 0 && e.Size != size {
				return fmt.Errorf("%w：大小 %d，記錄 %d", ErrTrustMismatch, size, e.Size)
			}
			return compare(e.SHA256, got, ErrTrustMismatch)
		}
	}
	if c.GitHubDigest {
		if owner, repo, tag, ok := f.githubRelease(d.URL); ok {
			want, err := f.githubDigest(ctx, owner, repo, tag, name)
			if err != nil {
				return fmt.Errorf("%w：%v", ErrUnverified, err)
			}
			return compare(want, got, ErrChecksumMismatch)
		}
	}
	return fmt.Errorf("%w：沒有可用的雜湊來源", ErrUnverified)
}

// hasSource 回報是否設了任何雜湊來源。recipe 的同名判斷沒有匯出，這裡另寫一份。
func hasSource(c recipe.ChecksumSpec) bool {
	return c.Sigstore != nil || c.Provider != "" || c.File != "" || c.Trust || c.GitHubDigest
}

func compare(want, got string, sentinel error) error {
	if strings.ToLower(want) != got {
		return fmt.Errorf("%w：得到 %s，應為 %s", sentinel, got, strings.ToLower(want))
	}
	return nil
}

// sumLine 接受 `<hex>  <name>`、`<hex> *<name>`（sha256sum -b）與只有 `<hex>` 的行。
var sumLine = regexp.MustCompile(`^([0-9a-fA-F]{64})(?:\s+\*?(\S.*))?$`)

// findChecksum 在 checksum 檔找檔名等於 name 的那一行。檔名從行尾錨定比對，
// 避免 helm.tar.gz 誤配 helm.tar.gz.asc；允許前面帶目錄（./dist/helm.tar.gz）。
func findChecksum(b []byte, name string) (string, error) {
	var bare []string
	for _, line := range strings.Split(string(b), "\n") {
		m := sumLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		file := strings.TrimSpace(m[2])
		switch {
		case file == "":
			bare = append(bare, m[1])
		case file == name || strings.HasSuffix(file, "/"+name):
			return m[1], nil
		}
	}
	// 單檔的 .sha256 只放雜湊、不寫檔名；只有一行時才認，多行無從判斷是哪一個。
	if len(bare) == 1 {
		return bare[0], nil
	}
	return "", fmt.Errorf("找不到 %s 的雜湊", name)
}

// githubRelease 從 https://github.com/<owner>/<repo>/releases/download/<tag>/<file> 取出各段。
// 不是這種網址時回 false，GitHubDigest 就不適用。
func (f *Fetcher) githubRelease(u string) (owner, repo, tag string, ok bool) {
	rest, found := strings.CutPrefix(u, f.up().Base()+"/")
	if !found {
		return "", "", "", false
	}
	p := strings.Split(rest, "/")
	if len(p) != 6 || p[2] != "releases" || p[3] != "download" {
		return "", "", "", false
	}
	return p[0], p[1], p[4], true
}

// githubDigest 讀 release API 的 asset digest。tag 取自下載網址而非 Latest 回的版本，
// 因為配方可能寫成 v{{.Version}}，網址裡的才是真正的 tag。
func (f *Fetcher) githubDigest(ctx context.Context, owner, repo, tag, name string) (string, error) {
	c := f.up()
	api := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", c.APIBase(), owner, repo, url.PathEscape(tag))
	h := http.Header{"Accept": {"application/vnd.github+json"}}
	if c.Token != "" {
		h.Set("Authorization", "Bearer "+c.Token)
	}
	b, err := f.getSmall(ctx, api, h)
	if err != nil {
		return "", err
	}
	var rel struct {
		Assets []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &rel); err != nil {
		return "", fmt.Errorf("解析 %s：%w", api, err)
	}
	for _, as := range rel.Assets {
		if as.Name != name {
			continue
		}
		hexd, ok := strings.CutPrefix(as.Digest, "sha256:")
		if !ok || len(hexd) != 64 {
			return "", fmt.Errorf("%s 的 asset %s 沒有 sha256 digest", api, name)
		}
		return hexd, nil
	}
	return "", fmt.Errorf("%s 沒有 asset %s", api, name)
}

// transient 判斷值不值得重試：連線錯誤、5xx 與 429。
type transientErr struct{ error }

func (f *Fetcher) do(ctx context.Context, u string, h http.Header, sink func(io.Reader) error) error {
	var last error
	for i := range attempts {
		if i > 0 {
			select {
			case <-time.After(time.Duration(i) * retryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		err := f.once(ctx, u, h, sink)
		if err == nil {
			return nil
		}
		last = err
		var te transientErr
		if !errors.As(err, &te) {
			return err
		}
	}
	return last
}

func (f *Fetcher) once(ctx context.Context, u string, h http.Header, sink func(io.Reader) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	for k, v := range h {
		req.Header[k] = v
	}
	resp, err := f.up().HTTPClient().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return transientErr{fmt.Errorf("fetch: 下載 %s：%w", u, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return transientErr{fmt.Errorf("fetch: %s 回 HTTP %d", u, resp.StatusCode)}
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch: %s 回 HTTP %d", u, resp.StatusCode)
	}
	if err := sink(resp.Body); err != nil {
		// 讀到一半斷線也算暫時性錯誤。
		return transientErr{fmt.Errorf("fetch: 讀 %s：%w", u, err)}
	}
	return nil
}

func (f *Fetcher) getSmall(ctx context.Context, u string, h http.Header) ([]byte, error) {
	var buf bytes.Buffer
	err := f.do(ctx, u, h, func(r io.Reader) error {
		buf.Reset()
		_, err := io.Copy(&buf, io.LimitReader(r, smallLimit))
		return err
	})
	return buf.Bytes(), err
}

// download 邊寫暫存檔邊算 sha256，大檔不必讀第二遍。暫存檔放在 Dir，
// 驗證通過後 rename 到正式檔名是同一個檔案系統內的原子操作。
func (f *Fetcher) download(ctx context.Context, u string) (string, []byte, int64, error) {
	tmp, err := os.CreateTemp(f.Dir, ".mksctl-*.part")
	if err != nil {
		return "", nil, 0, fmt.Errorf("fetch: 建暫存檔：%w", err)
	}
	h := sha256.New()
	var size int64
	err = f.do(ctx, u, nil, func(r io.Reader) error {
		// 重試時從頭寫，避免前一次的殘段混進來。
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if err := tmp.Truncate(0); err != nil {
			return err
		}
		h.Reset()
		n, err := io.Copy(io.MultiWriter(tmp, h), r)
		size = n
		return err
	})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, 0, err
	}
	return tmp.Name(), h.Sum(nil), size, nil
}
