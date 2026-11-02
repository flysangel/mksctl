package upstream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fastRetry(t *testing.T) {
	t.Helper()
	old := retryDelay
	retryDelay = time.Millisecond
	t.Cleanup(func() { retryDelay = old })
}

// flaky 前 fails 次回 code，之後交給 ok。
func flaky(fails int32, code int, ok http.HandlerFunc) http.HandlerFunc {
	var n atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) <= fails {
			w.WriteHeader(code)
			return
		}
		ok(w, r)
	}
}

func TestLatestURLRetriesTransient(t *testing.T) {
	fastRetry(t)
	for _, code := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusTooManyRequests} {
		srv := newCountingServer(t, flaky(2, code, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("1.2.3"))
		}))
		c := &Client{HTTP: srv.Client()}
		v, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
		require.NoError(t, err, code)
		assert.Equal(t, "1.2.3", v)
		assert.Equal(t, 3, srv.hits("/v"), code)
	}
}

func TestLatestGitHubRetriesTransient(t *testing.T) {
	fastRetry(t)
	srv := newCountingServer(t, flaky(1, http.StatusServiceUnavailable, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/a/b/releases/tag/v1.0.0", http.StatusFound)
	}))
	c := &Client{HTTP: srv.Client(), GitHubBase: srv.URL}
	v, err := c.Latest(context.Background(), LatestSpec{GitHub: "a/b"})
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", v)
	assert.Equal(t, 2, srv.hits("/a/b/releases/latest"))
}

func TestProviderBodyRetriesTransient(t *testing.T) {
	fastRetry(t)
	srv := newCountingServer(t, flaky(1, http.StatusBadGateway, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	c := &Client{HTTP: srv.Client()}
	b, err := c.body(context.Background(), srv.URL+"/dl")
	require.NoError(t, err)
	assert.Equal(t, "ok", b)
	assert.Equal(t, 2, srv.hits("/dl"))
}

func TestLatestGivesUpAfterAttempts(t *testing.T) {
	fastRetry(t)
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := &Client{HTTP: srv.Client()}
	_, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503")
	assert.Equal(t, attempts, srv.hits("/v"))
}

// 404 與 403（GitHub 限流）不是暫時性錯誤，重試也一樣，不能多打。
func TestLatestNoRetryOnClientError(t *testing.T) {
	fastRetry(t)
	for _, code := range []int{http.StatusNotFound, http.StatusForbidden} {
		srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		})
		c := &Client{HTTP: srv.Client(), GitHubBase: srv.URL}
		_, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
		require.Error(t, err)
		assert.Equal(t, 1, srv.hits("/v"), code)
		_, err = c.Latest(context.Background(), LatestSpec{GitHub: "a/b"})
		require.Error(t, err)
		assert.Equal(t, 1, srv.hits("/a/b/releases/latest"), code)
	}
}

// 憑證錯誤代表連線被攔截，重試不會變好。
func TestLatestNoRetryOnCertError(t *testing.T) {
	fastRetry(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(srv.Close)
	var dials atomic.Int32
	hc := NewHTTPClient()
	tr := hc.Transport.(*http.Transport)
	d := &net.Dialer{}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials.Add(1)
		return d.DialContext(ctx, network, addr)
	}
	c := &Client{HTTP: hc}
	_, err := c.Latest(context.Background(), LatestSpec{URL: srv.URL + "/v"})
	require.Error(t, err)
	assert.Equal(t, int32(1), dials.Load())
}

// 被擋的降級轉址也代表連線被攔截，不重試。
func TestLatestNoRetryOnInsecureRedirect(t *testing.T) {
	fastRetry(t)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3"))
	}))
	t.Cleanup(plain.Close)
	var hits atomic.Int32
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, plain.URL+"/v", http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	c := &Client{HTTP: tlsClient(t, secure, HeaderTimeout)}
	_, err := c.Latest(context.Background(), LatestSpec{URL: secure.URL + "/v"})
	require.ErrorIs(t, err, ErrInsecureRedirect)
	assert.Equal(t, int32(1), hits.Load())
}

// timeoutErr 模擬 net.Error 的逾時。
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func urlErr(err error) error {
	return fmt.Errorf("upstream: 查 a/b：%w", &url.Error{Op: "Get", URL: "https://example.invalid/", Err: err})
}

func TestIsUnreachable(t *testing.T) {
	var syntaxErr *json.SyntaxError
	jsonErr := json.Unmarshal([]byte("{"), &struct{}{})
	require.ErrorAs(t, jsonErr, &syntaxErr)

	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"dial refused":    {urlErr(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}), true},
		"net unreach":     {urlErr(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}), true},
		"dns":             {urlErr(&net.DNSError{Err: "no such host", Name: "example.invalid", IsNotFound: true}), true},
		"timeout":         {urlErr(timeoutErr{}), true},
		"nil":             {nil, false},
		"canceled":        {urlErr(context.Canceled), false},
		"http 404":        {errors.New("upstream: https://x/ 回 HTTP 404"), false},
		"parse":           {fmt.Errorf("upstream: 解析 x：%w", jsonErr), false},
		"insecure redir":  {urlErr(fmt.Errorf("%w：https://a → http://b", ErrInsecureRedirect)), false},
		"x509 unknown CA": {urlErr(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), false},
		"x509 hostname":   {urlErr(x509.HostnameError{Host: "x"}), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsUnreachable(tc.err))
		})
	}
}

// 真的連線產生的錯誤：關掉的 port 是離線；自簽憑證（captive portal 攔截 https）與被擋的降級轉址不是。
func TestIsUnreachableRealErrors(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	_, err = NewHTTPClient().Get("http://" + addr + "/")
	require.Error(t, err)
	assert.True(t, IsUnreachable(err), err)

	portal := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(portal.Close)
	_, err = NewHTTPClient().Get(portal.URL)
	require.Error(t, err)
	assert.False(t, IsUnreachable(err), err)

	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	t.Cleanup(secure.Close)
	_, err = tlsClient(t, secure, HeaderTimeout).Get(secure.URL)
	require.ErrorIs(t, err, ErrInsecureRedirect)
	assert.False(t, IsUnreachable(err), err)
}
