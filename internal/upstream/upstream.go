// Package upstream 向上游查詢工具的最新版本。
package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/flysangel/mksctl/internal/recipe"
)

// Client 查上游。測試用 httptest 覆寫兩個 base。
type Client struct {
	HTTP          *http.Client
	GitHubBase    string // 預設 https://github.com
	GitHubAPIBase string // 預設 https://api.github.com
	// Token 非空時，GitHub API 請求帶 Authorization: Bearer，避開匿名限流。
	Token string

	mu    sync.Mutex
	cache map[string]*entry
}

// entry 讓同時查同一個鍵的呼叫者共用一次請求，不必各打一次。
type entry struct {
	done    chan struct{}
	version string
	err     error
}

// HTTPClient 回傳要用的 http.Client。沒設時用預設值。
func (c *Client) HTTPClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Base 回傳 GitHub 網站的根網址。
func (c *Client) Base() string {
	if c.GitHubBase != "" {
		return strings.TrimRight(c.GitHubBase, "/")
	}
	return "https://github.com"
}

// APIBase 回傳 GitHub API 的根網址。
func (c *Client) APIBase() string {
	if c.GitHubAPIBase != "" {
		return strings.TrimRight(c.GitHubAPIBase, "/")
	}
	return "https://api.github.com"
}

// Latest 回傳最新版本字串。同一個 Client 對同一個 repo 或網址只查一次；
// 查詢失敗也記住，同一輪安裝不重複打已知失敗的上游。
func (c *Client) Latest(ctx context.Context, s recipe.LatestSpec) (string, error) {
	var key string
	switch {
	case s.GitHub != "":
		key = "github:" + s.GitHub
	case s.URL != "":
		key = "url:" + s.URL
	default:
		return "", errors.New("upstream: latest 沒有設 github 或 url")
	}

	c.mu.Lock()
	if c.cache == nil {
		c.cache = map[string]*entry{}
	}
	if e, ok := c.cache[key]; ok {
		c.mu.Unlock()
		select {
		case <-e.done:
			return e.version, e.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	e := &entry{done: make(chan struct{})}
	c.cache[key] = e
	c.mu.Unlock()

	if s.GitHub != "" {
		e.version, e.err = c.latestGitHub(ctx, s.GitHub)
	} else {
		e.version, e.err = c.latestURL(ctx, s.URL)
	}
	close(e.done)
	return e.version, e.err
}

// latestGitHub 讀 releases/latest 的轉址目標。走網頁而不走 API，
// 是因為網頁轉址不吃 API 的匿名限流。
func (c *Client) latestGitHub(ctx context.Context, repo string) (string, error) {
	u := c.Base() + "/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	// 複製一份 client，只在這裡關掉轉址；下載仍要跟隨 GitHub 的轉址。
	hc := *c.HTTPClient()
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("upstream: 查 %s：%w", repo, err)
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if resp.StatusCode < 300 || resp.StatusCode >= 400 || loc == "" {
		return "", fmt.Errorf("upstream: %s 沒有轉址（HTTP %d）", u, resp.StatusCode)
	}
	v := path.Base(strings.TrimRight(loc, "/"))
	// 沒有任何 release 時，GitHub 會轉回 releases 列表頁，最後一段不是版本。
	if v == "" || v == "." || v == "/" || v == "releases" || v == "latest" {
		return "", fmt.Errorf("upstream: %s 轉址到 %s，取不到版本", u, loc)
	}
	return v, nil
}

func (c *Client) latestURL(ctx context.Context, u string) (string, error) {
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
	// 版本檔只有一行，限制讀取量，避免網址填錯時把整個網頁讀進來。
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("upstream: %s 內文是空的", u)
	}
	return v, nil
}

// Effective 回傳實際要裝的版本：上游最新版在 exclude 裡時改用替代版本。
func Effective(version string, exclude map[string]string) string {
	if r, ok := exclude[version]; ok {
		return r
	}
	return version
}
