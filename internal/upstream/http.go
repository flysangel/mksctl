package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// HeaderTimeout 是送出請求後等回應標頭的上限。讀 body 不算在內，由 Watchdog 管。
	HeaderTimeout = 30 * time.Second
	// IdleTimeout 是下載中途多久沒有新資料就中止。fetch 與 selfupdate 共用。
	IdleTimeout = 60 * time.Second
	// maxRedirects 與 http.Client 的預設值相同。
	maxRedirects = 10
)

var (
	// ErrInsecureRedirect：https 被轉址到非 https。TLS 是 apt 金鑰唯一的防線，降級就等於讓中間人換檔。
	ErrInsecureRedirect = errors.New("upstream: 拒絕從 https 轉址到非 https")
	// ErrTooLarge：回應超過上限。上限是刻意設的，重試也不會變小，所以呼叫端不該重試。
	ErrTooLarge = errors.New("upstream: 回應超過上限")
	// ErrStalled：下載中途超過 IdleTimeout 沒有新資料。
	ErrStalled = errors.New("upstream: 下載中途太久沒有新資料")
)

// NewHTTPClient 回傳正式環境用的 client：等標頭 HeaderTimeout 秒，轉址不准從 https 降到非 https。
// 不設 Client.Timeout：200 MB 的檔在慢線上會超過任何固定的整體逾時；卡住改由 Watchdog 處理。
func NewHTTPClient() *http.Client {
	return newHTTPClient(HeaderTimeout)
}

func newHTTPClient(headerTimeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = headerTimeout
	return &http.Client{Transport: tr, CheckRedirect: checkRedirect}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("upstream: 轉址超過 %d 次", maxRedirects)
	}
	if prev := via[len(via)-1]; prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("%w：%s → %s", ErrInsecureRedirect, prev.URL, req.URL)
	}
	return nil
}

var (
	defaultOnce   sync.Once
	defaultClient *http.Client
)

// defaultHTTPClient 讓沒設 HTTP 的 Client 共用一個 client，連線池才能重用。
func defaultHTTPClient() *http.Client {
	defaultOnce.Do(func() { defaultClient = NewHTTPClient() })
	return defaultClient
}

// ReadLimited 讀完 r，超過 n bytes 時回 ErrTooLarge。不截斷：截斷後的內容只會換來誤導的錯誤，
// 例如「找不到雜湊」或 JSON 解析失敗。
func ReadLimited(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > n {
		return nil, fmt.Errorf("%w：超過 %d bytes", ErrTooLarge, n)
	}
	return b, nil
}

// CheckLength 在開始讀 body 前先看 Content-Length。標頭已經說超過上限時，不必讀就能失敗。
// 沒有 Content-Length（-1）時放行，交給讀取時的上限擋。
func CheckLength(resp *http.Response, n int64) error {
	if resp.ContentLength > n {
		return fmt.Errorf("%w：Content-Length %d 超過 %d bytes", ErrTooLarge, resp.ContentLength, n)
	}
	return nil
}

// Watchdog 是一次下載的閒置看門狗。用法：
//
//	wd := NewWatchdog(ctx, d); defer wd.Stop()
//	req 用 wd.Context()；讀 body 用 wd.Reader(resp.Body)；讀取錯誤交給 wd.Err。
//
// d 內沒有讀到新資料時取消 wd.Context()，卡住的 Read 跟著返回，wd.Err 把錯誤換成 ErrStalled。
type Watchdog struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	d      time.Duration
	mu     sync.Mutex
	timer  *time.Timer
}

// NewWatchdog 建一個看門狗。計時從呼叫 Reader 開始，等標頭的時間由 HeaderTimeout 管。
func NewWatchdog(ctx context.Context, d time.Duration) *Watchdog {
	c, cancel := context.WithCancelCause(ctx)
	return &Watchdog{ctx: c, cancel: cancel, d: d}
}

// Context 回傳要給請求用的 context。
func (w *Watchdog) Context() context.Context { return w.ctx }

// Reader 包住 r 並開始計時，每讀到資料就重新計時。
func (w *Watchdog) Reader(r io.Reader) io.Reader {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer == nil {
		w.timer = time.AfterFunc(w.d, func() { w.cancel(ErrStalled) })
	}
	return &idleReader{r: r, w: w}
}

// Stop 停掉計時並釋放 context。讀完 body 後呼叫。
func (w *Watchdog) Stop() {
	w.mu.Lock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.mu.Unlock()
	w.cancel(nil)
}

// Err 在看門狗觸發時把 err 換成 ErrStalled，其餘原樣回傳。
func (w *Watchdog) Err(err error) error {
	if err != nil && errors.Is(context.Cause(w.ctx), ErrStalled) {
		return fmt.Errorf("%w（%s）", ErrStalled, w.d)
	}
	return err
}

type idleReader struct {
	r io.Reader
	w *Watchdog
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.w.timer.Reset(i.w.d)
	}
	return n, err
}
