//go:build !windows

package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// lockPollInterval 是搶不到鎖時重試的間隔。用非阻塞 flock 加輪詢，
// 而不是在 goroutine 裡阻塞等：逾時與取消時不必收拾卡在系統呼叫裡的 goroutine。
const lockPollInterval = 100 * time.Millisecond

// Lock 鎖住 <path>.lock，擋掉同時跑的兩個 mksctl：兩邊都讀狀態檔再各自寫回，
// 後寫的會蓋掉先寫的。等超過 wait 回錯誤。flock 綁在開啟的檔案上，
// 行程結束（含被殺）時核心自動放鎖，不會留下死鎖。
// 鎖檔本身留在原地不刪：刪掉會讓等鎖的一方鎖到已被 unlink 的 inode。
func Lock(ctx context.Context, path string, wait time.Duration) (unlock func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() {
				_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != unix.EWOULDBLOCK && err != unix.EINTR {
			_ = f.Close()
			return nil, fmt.Errorf("鎖 %s：%w", lockPath, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = f.Close()
			return nil, fmt.Errorf("另一個 mksctl 正在執行（鎖檔 %s，等了 %s 仍被占用）", lockPath, wait)
		case <-time.After(lockPollInterval):
		}
	}
}
