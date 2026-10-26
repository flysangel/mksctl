//go:build windows

package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// lockPollInterval 與 Unix 版相同：非阻塞取鎖加輪詢，逾時與取消時不必收拾卡在系統呼叫裡的 goroutine。
const lockPollInterval = 100 * time.Millisecond

// Lock 鎖住 <path>.lock，語意與 lock_unix.go 相同：等超過 wait 回錯誤，ctx 取消回 ctx.Err()。
// LockFileEx 的鎖綁在 handle 上，行程結束（含被殺）時系統自動放鎖。
// 鎖檔留在原地不刪，理由同 Unix 版。
func Lock(ctx context.Context, path string, wait time.Duration) (unlock func(), err error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		// 鎖整個檔（長度取最大值）；鎖檔是空檔，鎖的範圍超出檔尾也合法。
		err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
		if err == nil {
			return func() {
				_ = windows.UnlockFileEx(h, 0, ^uint32(0), ^uint32(0), new(windows.Overlapped))
				_ = f.Close()
			}, nil
		}
		// 搶不到時回 ERROR_LOCK_VIOLATION；以非同步 handle 開檔時可能回 ERROR_IO_PENDING，一併當成被占用。
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) && !errors.Is(err, windows.ERROR_IO_PENDING) {
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
