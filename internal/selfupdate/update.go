package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/flysangel/mksctl/internal/fetch"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/upstream"
)

const (
	// Asset 是 release 上的執行檔名稱，與 bin/install.sh 一致。
	Asset = "mksctl-linux-amd64"
	// Identity 是 release workflow 簽 attestation 時 Fulcio 憑證的 SAN。
	// 錨定到 tag 觸發的 release.yml，分支或別的 workflow 簽出來的都不收。
	Identity = `^https://github\.com/flysangel/mksctl/\.github/workflows/release\.yml@refs/tags/v`
	// Issuer 是 GitHub Actions 的 OIDC 簽發者。
	Issuer = "https://token.actions.githubusercontent.com"

	// smallLimit 限制 checksums.txt 與 API 回應的大小，網址填錯時不讀進整個大檔。
	smallLimit = 16 << 20
)

// Verifier 驗一份 sigstore bundle 是否簽了這個 sha256。測試換成假的，不必準備真的簽章材料。
type Verifier interface {
	Verify(ctx context.Context, bundleJSON, digest []byte) error
}

// SigstoreVerifier 用 sigstore 官方信任根做 keyless 驗證，身分固定為 mksctl 的 release workflow。
type SigstoreVerifier struct {
	// TUFCacheDir 是 TUF 的快取目錄，CLI 傳 /var/cache/mksctl/tuf。
	TUFCacheDir string
}

// Verify 實作 Verifier。
func (s SigstoreVerifier) Verify(ctx context.Context, bundleJSON, digest []byte) error {
	tm, err := fetch.FetchTrustedRoot(ctx, s.TUFCacheDir)
	if err != nil {
		return fmt.Errorf("取 sigstore 信任根：%w", err)
	}
	return fetch.VerifyKeyless(tm, bundleJSON, digest, Identity, Issuer)
}

// Updater 下載新版 mksctl，驗過雜湊與 attestation 才換掉 dest。
type Updater struct {
	Client   *upstream.Client
	Verifier Verifier
}

// Result 是一次更新的結果。UpToDate 為真時沒有下載，也沒有動 dest。
type Result struct {
	From, To string
	UpToDate bool
}

// Update 把 dest 換成 tag 那一版；tag 空字串時用最新版。current 是正在跑的版本，
// 只用來判斷是否已是最新版與組訊息。任何一項驗證失敗，dest 都保持原樣。
func (u *Updater) Update(ctx context.Context, tag, current, dest string) (Result, error) {
	res := Result{From: current, To: tag}
	if tag != "" {
		if !validTag(tag) {
			return res, fmt.Errorf("--tag 要寫成 vX.Y.Z：%q", tag)
		}
	} else {
		latest, err := u.Client.Latest(ctx, recipe.LatestSpec{GitHub: Repo})
		if err != nil {
			return res, err
		}
		// 最新版的 tag 也會拼進網址，跟 --tag 用同一道檢查。
		if !validTag(latest) {
			return res, fmt.Errorf("最新版的 tag 不是 vX.Y.Z：%q", latest)
		}
		res.To = latest
		if latest == current {
			res.UpToDate = true
			return res, nil
		}
		tag = latest
	}

	// 一律從查到的那一版下載，不走 latest/download：查版本與下載之間若剛好發了新版，
	// latest/download 會拿到另一版，回報的版本就跟裝上去的不一樣。
	base := u.Client.Base() + "/" + Repo + "/releases/download/" + tag + "/"

	// 暫存檔放在 dest 同一個目錄：最後的 rename 才在同一個檔案系統內，換檔是原子的。
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".mksctl.new.*")
	if err != nil {
		return res, fmt.Errorf("建立暫存檔：%w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	if err := u.download(ctx, base+Asset, io.MultiWriter(tmp, h), -1); err != nil {
		return res, err
	}
	digest := h.Sum(nil)

	var sums bytes.Buffer
	if err := u.download(ctx, base+"checksums.txt", &sums, smallLimit); err != nil {
		return res, err
	}
	want, err := checksumFor(sums.Bytes(), Asset)
	if err != nil {
		return res, err
	}
	if got := hex.EncodeToString(digest); got != want {
		return res, fmt.Errorf("%s 雜湊不符：checksums.txt 是 %s，下載檔是 %s", Asset, want, got)
	}

	if err := u.verifyAttestation(ctx, digest); err != nil {
		return res, err
	}

	if err := tmp.Chmod(0o755); err != nil {
		return res, fmt.Errorf("設定執行權限：%w", err)
	}
	if err := tmp.Close(); err != nil {
		return res, fmt.Errorf("寫入暫存檔：%w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return res, fmt.Errorf("換掉 %s：%w", dest, err)
	}
	committed = true
	return res, nil
}

// validTag 檢查 tag 是 vX.Y.Z。與 release.yml 的 tag 檢查同一個格式。
func validTag(tag string) bool {
	_, ok := parseVersion(tag)
	return ok && strings.HasPrefix(tag, "v")
}

// download 把 url 的內容寫進 w。limit 小於 0 表示不限制大小。
func (u *Updater) download(ctx context.Context, url string, w io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return u.do(req, url, w, limit)
}

func (u *Updater) do(req *http.Request, url string, w io.Writer, limit int64) error {
	resp, err := u.Client.HTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("下載 %s：%w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下載 %s：HTTP %d", url, resp.StatusCode)
	}
	var r io.Reader = resp.Body
	if limit >= 0 {
		r = io.LimitReader(resp.Body, limit)
	}
	if _, err := io.Copy(w, r); err != nil {
		return fmt.Errorf("下載 %s：%w", url, err)
	}
	return nil
}

// checksumFor 從 sha256sum 格式的內容找 name 那一行。要求剛好一行：
// 漏列這個檔時不能當成通過，重複列出時也分不出該信哪一行。
func checksumFor(data []byte, name string) (string, error) {
	var found []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			found = append(found, strings.ToLower(f[0]))
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("讀 checksums.txt：%w", err)
	}
	if len(found) != 1 {
		return "", fmt.Errorf("checksums.txt 沒有 %s 或不只一行（找到 %d 行）", name, len(found))
	}
	return found[0], nil
}

// verifyAttestation 向 GitHub API 取這個 digest 的 attestation，任一份通過驗證就算過。
func (u *Updater) verifyAttestation(ctx context.Context, digest []byte) error {
	url := u.Client.APIBase() + "/repos/" + Repo + "/attestations/sha256:" + hex.EncodeToString(digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if u.Client.Token != "" {
		req.Header.Set("Authorization", "Bearer "+u.Client.Token)
	}
	var body bytes.Buffer
	if err := u.do(req, url, &body, smallLimit); err != nil {
		return fmt.Errorf("取 attestation：%w", err)
	}
	var resp struct {
		Attestations []struct {
			Bundle json.RawMessage `json:"bundle"`
		} `json:"attestations"`
	}
	if err := json.Unmarshal(body.Bytes(), &resp); err != nil {
		return fmt.Errorf("解析 attestation 回應：%w", err)
	}
	if len(resp.Attestations) == 0 {
		return errors.New("GitHub 沒有這個執行檔的 attestation，不是 mksctl 的 release workflow 建出來的")
	}
	var errs []error
	for _, a := range resp.Attestations {
		err := u.Verifier.Verify(ctx, a.Bundle, digest)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	return fmt.Errorf("attestation 驗證失敗：%w", errors.Join(errs...))
}
