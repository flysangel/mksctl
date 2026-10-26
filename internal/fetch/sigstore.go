package fetch

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"

	"github.com/flysangel/mksctl/internal/recipe"
)

// trustedMaterial 回傳 sigstore 的信任根。測試注入固定的 trusted_root.json，
// 正式執行走 TUF，讓信任根跟著 sigstore 官方輪替，不寫死在程式裡。
//
// 只快取成功的結果。TUF 靠網路，暫時失敗或使用者取消（context.Canceled）若被記住，
// 同一個 Fetcher 之後每次驗證都會失敗。失敗時不設 f.root，下一個呼叫者重試；
// 鎖保證同時只有一個在抓，等鎖的呼叫者在前一個失敗後自己再試一次。
func (f *Fetcher) trustedMaterial(ctx context.Context) (root.TrustedMaterial, error) {
	f.rootMu.Lock()
	defer f.rootMu.Unlock()
	if f.root != nil {
		return f.root, nil
	}
	if f.TrustedRoot != nil {
		f.root = f.TrustedRoot
		return f.root, nil
	}
	fetch := f.fetchRoot
	if fetch == nil {
		fetch = FetchTrustedRoot
	}
	tm, err := fetch(ctx, f.TUFCacheDir)
	if err != nil {
		return nil, err
	}
	f.root = tm
	return tm, nil
}

// FetchTrustedRoot 經 TUF 取 sigstore 官方的信任根。cacheDir 空字串時用 sigstore-go 的預設快取。
func FetchTrustedRoot(ctx context.Context, cacheDir string) (root.TrustedMaterial, error) {
	opts := tuf.DefaultOptions().WithContext(ctx)
	if cacheDir != "" {
		opts = opts.WithCachePath(cacheDir)
	}
	return root.FetchTrustedRootWithOptions(opts)
}

// keyMaterial 把釘在 trust/ 的公鑰包成信任材料。金鑰簽章的 bundle 只帶公鑰提示，
// 不論提示是什麼都回同一把鑰匙：配方只認這一把，提示不同就讓簽章驗證失敗。
func (f *Fetcher) keyMaterial(path string) (root.TrustedMaterial, error) {
	if f.TrustFS == nil {
		return nil, errors.New("沒有設定 TrustFS，讀不到公鑰")
	}
	pem, err := fs.ReadFile(f.TrustFS, path)
	if err != nil {
		return nil, fmt.Errorf("讀公鑰 %s：%w", path, err)
	}
	pk, err := cryptoutils.UnmarshalPEMToPublicKey(pem)
	if err != nil {
		return nil, fmt.Errorf("解析公鑰 %s：%w", path, err)
	}
	sv, err := signature.LoadVerifier(pk, crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("載入公鑰 %s：%w", path, err)
	}
	// 零值時間代表沒有有效期限制；這把鑰匙的效期由 trust/ 的維護者負責。
	key := root.NewExpiringKey(sv, time.Time{}, time.Time{})
	return root.NewTrustedPublicKeyMaterial(func(string) (root.TimeConstrainedVerifier, error) {
		return key, nil
	}), nil
}

// verifySigstore 用 bundle 驗證檔案的 sha256。Key 有值時用該公鑰驗簽；
// 否則要求 Fulcio 憑證符合配方寫的身分。只比 digest 不讀整個檔，所以測試不必下載大檔。
func (f *Fetcher) verifySigstore(ctx context.Context, bundleJSON, digest []byte, s recipe.SigstoreSpec) error {
	tm, err := f.trustedMaterial(ctx)
	if err != nil {
		return fmt.Errorf("%w：取 sigstore 信任根：%v", ErrUnverified, err)
	}
	if s.Key == "" {
		return VerifyKeyless(tm, bundleJSON, digest, s.Identity, s.Issuer)
	}
	km, err := f.keyMaterial(s.Key)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrUnverified, err)
	}
	// 透明紀錄與時間戳仍由 sigstore 信任根驗，公鑰只負責簽章。
	tm = root.TrustedMaterialCollection{tm, km}
	return verifyBundle(tm, bundleJSON, digest, verify.WithKey(), nil)
}

// VerifyKeyless 用 Fulcio 憑證的身分驗 bundle：identity 是 SAN 的正規表示式，issuer 是 OIDC 簽發者。
// 只比 digest，呼叫端自己算好下載檔的 sha256。mksctl update 驗 GitHub artifact attestation 用這支。
func VerifyKeyless(tm root.TrustedMaterial, bundleJSON, digest []byte, identity, issuer string) error {
	id, err := verify.NewShortCertificateIdentity(issuer, "", "", identity)
	if err != nil {
		return fmt.Errorf("%w：sigstore 身分設定：%v", ErrUnverified, err)
	}
	// keyless 另要求 Fulcio 憑證帶至少一筆 SCT（憑證透明度紀錄的簽章時間戳）：
	// 沒有 SCT，就無法證明這張憑證有公開登記過，被偷簽的憑證也查不出來。
	// 金鑰那條路沒有憑證，加這個選項 sigstore-go 會直接拒絕，所以只放在這裡。
	return verifyBundle(tm, bundleJSON, digest, verify.WithCertificateIdentity(id),
		[]verify.VerifierOption{verify.WithSignedCertificateTimestamps(1)})
}

// verifyBundle 是金鑰與 keyless 兩條路共用的驗證：要求透明紀錄與時間戳各至少一筆。
// extra 是只有某一條路才要的驗證器選項。
func verifyBundle(tm root.TrustedMaterial, bundleJSON, digest []byte, idOpt verify.PolicyOption, extra []verify.VerifierOption) error {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("%w：解析 sigstore bundle：%v", ErrUnverified, err)
	}
	opts := append([]verify.VerifierOption{
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	}, extra...)
	v, err := verify.NewVerifier(tm, opts...)
	if err != nil {
		return fmt.Errorf("%w：建立 sigstore 驗證器：%v", ErrUnverified, err)
	}
	policy := verify.NewPolicy(verify.WithArtifactDigest("sha256", digest), idOpt)
	if _, err := v.Verify(&b, policy); err != nil {
		return fmt.Errorf("%w：sigstore 驗證失敗：%v", ErrUnverified, err)
	}
	return nil
}
