package upstream

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tlsClient 回傳套了 hardened 設定、但信任 srv 自簽憑證的 client。
func tlsClient(t *testing.T, srv *httptest.Server, headerTimeout time.Duration) *http.Client {
	t.Helper()
	c := newHTTPClient(headerTimeout)
	tr := c.Transport.(*http.Transport)
	tr.TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return c
}

func TestNewHTTPClientRejectsHTTPSToHTTPRedirect(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("downgraded"))
	}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x", http.StatusFound)
	}))
	t.Cleanup(secure.Close)

	resp, err := tlsClient(t, secure, HeaderTimeout).Get(secure.URL + "/key")
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInsecureRedirect)
}

func TestNewHTTPClientAllowsHTTPSToHTTPS(t *testing.T) {
	var secure *httptest.Server
	secure = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, secure.URL+"/to", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(secure.Close)

	resp, err := tlsClient(t, secure, HeaderTimeout).Get(secure.URL + "/from")
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(b))
}

func TestNewHTTPClientResponseHeaderTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	start := time.Now()
	resp, err := tlsClient(t, srv, 50*time.Millisecond).Get(srv.URL)
	if resp != nil {
		resp.Body.Close()
	}
	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestNewHTTPClientDefaults(t *testing.T) {
	c := NewHTTPClient()
	require.NotNil(t, c.CheckRedirect)
	tr, ok := c.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, HeaderTimeout, tr.ResponseHeaderTimeout)
	assert.Equal(t, 30*time.Second, HeaderTimeout)
	assert.Zero(t, c.Timeout, "大檔在慢線上會超過整體逾時，所以不設")
	// 預設 client 不是 http.DefaultClient：DefaultClient 沒有轉址與逾時政策。
	assert.NotSame(t, http.DefaultClient, (&Client{}).HTTPClient())
	assert.NotNil(t, (&Client{}).HTTPClient().CheckRedirect)
}

func TestReadLimited(t *testing.T) {
	b, err := ReadLimited(strings.NewReader("abcd"), 4)
	require.NoError(t, err)
	assert.Equal(t, "abcd", string(b), "剛好等於上限要收")

	_, err = ReadLimited(strings.NewReader("abcde"), 4)
	require.ErrorIs(t, err, ErrTooLarge)
	assert.Contains(t, err.Error(), "4")
}

func TestLatestURLOverLimitFails(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("v", 4097)))
	})
	c := &Client{HTTP: srv.Client()}
	_, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
	require.ErrorIs(t, err, ErrTooLarge)
	assert.Equal(t, 1, srv.hits("/v"), "超過上限不重試")
}

func TestBodyOverLimitFails(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", bodyLimit+1)))
	})
	c := &Client{HTTP: srv.Client()}
	_, err := c.body(context.Background(), srv.URL+"/dl")
	require.ErrorIs(t, err, ErrTooLarge)
	assert.Equal(t, 1, srv.hits("/dl"), "超過上限不重試")
}

// stallServer 送出標頭與一點內文後就不再送資料。
func stallServer(t *testing.T) *countingServer {
	t.Helper()
	return newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("1."))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
}

func fastIdle(t *testing.T) {
	t.Helper()
	old := idleTimeout
	idleTimeout = 50 * time.Millisecond
	t.Cleanup(func() { idleTimeout = old })
}

// 查最新版的小檔讀到一半卡住時，也要由看門狗中止，不能一直等。
func TestLatestURLStallTimesOut(t *testing.T) {
	fastRetry(t)
	fastIdle(t)
	srv := stallServer(t)
	c := &Client{HTTP: srv.Client()}
	start := time.Now()
	_, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
	require.ErrorIs(t, err, ErrStalled)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestBodyStallTimesOut(t *testing.T) {
	fastRetry(t)
	fastIdle(t)
	srv := stallServer(t)
	c := &Client{HTTP: srv.Client()}
	start := time.Now()
	_, err := c.body(context.Background(), srv.URL+"/dl")
	require.ErrorIs(t, err, ErrStalled)
	assert.Less(t, time.Since(start), 5*time.Second)
}

func TestWatchdogStopsStalledRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("first"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	wd := NewWatchdog(context.Background(), 50*time.Millisecond)
	defer wd.Stop()
	req, err := http.NewRequestWithContext(wd.Context(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	start := time.Now()
	_, err = io.ReadAll(wd.Reader(resp.Body))
	err = wd.Err(err)
	require.ErrorIs(t, err, ErrStalled)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestWatchdogKeepsOtherErrors(t *testing.T) {
	wd := NewWatchdog(context.Background(), time.Minute)
	defer wd.Stop()
	other := errors.New("boom")
	assert.Same(t, other, wd.Err(other))
	assert.NoError(t, wd.Err(nil))
}
