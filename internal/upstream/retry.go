package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// attempts 與 fetch 相同：一次加兩次重試。
const attempts = 3

// retryDelay 是重試間隔的基數，第 n 次重試等 n 倍。
var retryDelay = 200 * time.Millisecond

// transientErr 標記值得重試的錯誤：連線錯誤、5xx 與 429。
type transientErr struct{ error }

func (e transientErr) Unwrap() error { return e.error }

// retry 照 fetch.do 的做法重試暫時性錯誤。cloud-init 開機時網路常晚一步起來，
// 查一次就放棄的話，還沒裝的配方會直接失敗。
func retry(ctx context.Context, fn func() (string, error)) (string, error) {
	var last error
	for i := range attempts {
		if i > 0 {
			select {
			case <-time.After(time.Duration(i) * retryDelay):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		v, err := fn()
		if err == nil {
			return v, nil
		}
		var te transientErr
		if !errors.As(err, &te) {
			return "", err
		}
		last = te.error
	}
	return "", last
}

// requestErr 包裝 hc.Do 的錯誤。使用者取消、憑證錯誤與被擋的降級轉址不重試，其餘當成暫時性錯誤。
func requestErr(ctx context.Context, what string, err error) error {
	err = fmt.Errorf("upstream: 查 %s：%w", what, err)
	if ctx.Err() != nil || untrusted(err) {
		return err
	}
	return transientErr{err}
}

// statusErr 是非預期狀態碼的錯誤。5xx 與 429 可以重試。
func statusErr(u string, code int) error {
	err := fmt.Errorf("upstream: %s 回 HTTP %d", u, code)
	if code >= 500 || code == http.StatusTooManyRequests {
		return transientErr{err}
	}
	return err
}

// untrusted 判斷 err 是不是「連上了，但對方不可信」：憑證驗不過或 https 被轉到非 https。
// captive portal（強制登入頁）攔截 https 時就是這種錯誤。
func untrusted(err error) bool {
	if errors.Is(err, ErrInsecureRedirect) {
		return true
	}
	var cve *tls.CertificateVerificationError
	var uae x509.UnknownAuthorityError
	var hne x509.HostnameError
	var cie x509.CertificateInvalidError
	return errors.As(err, &cve) || errors.As(err, &uae) || errors.As(err, &hne) || errors.As(err, &cie)
}

// IsUnreachable 判斷 err 是不是「連不上上游」：撥號失敗、DNS 查不到或逾時。
// 下面這些不算，呼叫端不該把它們當成單純離線：
//   - HTTP 狀態碼與解析錯誤：上游有回應，是上游或配方壞了。
//   - 憑證錯誤與被擋的降級轉址：連線被攔截。
//   - 使用者取消。
func IsUnreachable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || untrusted(err) {
		return false
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// readErr 包裝讀內文的錯誤。超過上限不重試；讀到一半斷線算暫時性錯誤。
func readErr(ctx context.Context, u string, err error) error {
	err = fmt.Errorf("upstream: 讀 %s：%w", u, err)
	if ctx.Err() != nil || errors.Is(err, ErrTooLarge) {
		return err
	}
	return transientErr{err}
}
