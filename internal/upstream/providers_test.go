package upstream

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureServer 在 path 回 testdata 的檔。fixture 是真實回應裁小後的樣本，
// 雜湊值只放 testdata，不寫進 .go（TestNoSha256OutsideTrust 會擋）。
func fixtureServer(t *testing.T, path, file string) *countingServer {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	require.NoError(t, err)
	return newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(b)
	})
}

func readFixture(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	require.NoError(t, err)
	return strings.TrimSpace(string(b))
}

func TestGodevLatestAndChecksum(t *testing.T) {
	srv := fixtureServer(t, "/dl/", "godev.json")
	p := godev{URL: srv.URL + "/dl/?mode=json"}
	c := &Client{HTTP: srv.Client()}
	v, err := p.Latest(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "go1.26.4", v)

	sum, err := p.Checksum(context.Background(), c, "go1.26.4", "go1.26.4.linux-amd64.tar.gz")
	require.NoError(t, err)
	assert.Equal(t, readFixture(t, "godev-go1.26.4.linux-amd64.sha256"), sum)
	// 版本與雜湊來自同一份 JSON，同一個 Client 只打一次。
	assert.Equal(t, 1, srv.hits("/dl/"))

	_, err = p.Checksum(context.Background(), c, "go1.26.4", "go1.26.4.plan9-arm.tar.gz")
	require.Error(t, err)
	_, err = p.Checksum(context.Background(), c, "go1.9", "go1.9.linux-amd64.tar.gz")
	require.Error(t, err)
}

func TestGodevSkipsUnstable(t *testing.T) {
	srv := fixtureServer(t, "/dl/", "godev-unstable.json")
	v, err := godev{URL: srv.URL + "/dl/?mode=json"}.Latest(context.Background(), &Client{HTTP: srv.Client()})
	require.NoError(t, err)
	assert.Equal(t, "go1.26.4", v)
}

func TestJetbrainsLatest(t *testing.T) {
	srv := fixtureServer(t, "/products/releases", "jetbrains.json")
	p := jetbrains{URL: srv.URL + "/products/releases?code=IIC&latest=true&type=release", Code: "IIC"}
	v, err := p.Latest(context.Background(), &Client{HTTP: srv.Client()})
	require.NoError(t, err)
	assert.Equal(t, "2025.2.6.2", v)
	// 上游不提供雜湊：配方改用 checksum.file。
	_, err = p.Checksum(context.Background(), &Client{HTTP: srv.Client()}, v, "ideaIC-2025.2.6.2.tar.gz")
	require.ErrorIs(t, err, ErrNoChecksum)
}

func TestTdfLatestPicksHighestVersion(t *testing.T) {
	// fixture 另有 3.6.7/：字串比較會把它排在 26.8.0 之後，數字比較才會選對。
	srv := fixtureServer(t, "/libreoffice/stable/", "tdf.html")
	p := tdf{URL: srv.URL + "/libreoffice/stable/"}
	v, err := p.Latest(context.Background(), &Client{HTTP: srv.Client()})
	require.NoError(t, err)
	assert.Equal(t, "26.8.0", v)
	_, err = p.Checksum(context.Background(), &Client{HTTP: srv.Client()}, v, "x.deb")
	require.ErrorIs(t, err, ErrNoChecksum)
}

func TestTdfNoVersionFails(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a href="older/">older/</a><a href="/libreoffice/">Parent</a>`))
	})
	_, err := tdf{URL: srv.URL + "/"}.Latest(context.Background(), &Client{HTTP: srv.Client()})
	require.Error(t, err)
}

func TestProvidersRegistered(t *testing.T) {
	for _, n := range []string{"godev", "jetbrains-iic", "tdf-libreoffice"} {
		assert.Contains(t, Providers, n)
	}
}

// fakeProvider 計算 Latest 被呼叫幾次，驗證 Client.Latest 對 provider 也只查一次。
type fakeProvider struct{ calls int }

func (f *fakeProvider) Latest(context.Context, *Client) (string, error) {
	f.calls++
	return "1.0.0", nil
}

func (f *fakeProvider) Checksum(context.Context, *Client, string, string) (string, error) {
	return "", ErrNoChecksum
}

func TestLatestProviderMemoized(t *testing.T) {
	fp := &fakeProvider{}
	Providers["test-fake"] = fp
	t.Cleanup(func() { delete(Providers, "test-fake") })
	c := &Client{}
	for range 2 {
		v, err := c.Latest(context.Background(), LatestSpec{Provider: "test-fake"})
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", v)
	}
	assert.Equal(t, 1, fp.calls)
	_, err := c.Latest(context.Background(), LatestSpec{Provider: "nope"})
	require.ErrorContains(t, err, "nope")
}
