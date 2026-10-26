//go:build windows

package state

import (
	"context"
	"time"
)

// Lock 在 Windows 先回空鎖，讓程式編得過。真正的 LockFileEx 實作留給 Windows 計畫；
// 在那之前 Windows 上不擋並行執行。
func Lock(_ context.Context, _ string, _ time.Duration) (unlock func(), err error) {
	return func() {}, nil
}
