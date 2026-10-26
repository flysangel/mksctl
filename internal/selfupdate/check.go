// Package selfupdate 處理 mksctl 自己的版本：查新版與提示更新。
package selfupdate

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/flysangel/mksctl/internal/upstream"
)

// Repo 是 mksctl 在 GitHub 上的位置。
const Repo = "flysangel/mksctl"

// notifyTimeout 限制查新版的時間。提示只是附帶的，不能讓離線的機器每個命令都卡住。
const notifyTimeout = 3 * time.Second

// Notify 查最新版，比 current 新時在 stderr 印一行提示。
// 任何錯誤都安靜跳過：查不到新版不該讓原本成功的命令看起來像失敗。
func Notify(ctx context.Context, current string, c *upstream.Client, stderr io.Writer) {
	ctx, cancel := context.WithTimeout(ctx, notifyTimeout)
	defer cancel()
	latest, err := c.Latest(ctx, recipe.LatestSpec{GitHub: Repo})
	if err != nil || !newer(latest, current) {
		return
	}
	fmt.Fprintf(stderr, "Info: 有新版 %s，執行 curl -fsSL https://github.com/%s/releases/latest/download/install.sh | sudo bash\n", latest, Repo)
}

// newer 只比 vX.Y.Z 三段數字。任一邊看不懂時回 false：
// 寧可不提示，也不要把預發布版或奇怪的標籤當成新版，叫人「更新」成舊版。
func newer(a, b string) bool {
	x, ok1 := parseVersion(a)
	y, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
