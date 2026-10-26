//go:build windows

package state

import (
	"errors"

	"golang.org/x/sys/windows"
)

// renameRetryable 判定 Windows 上 rename 的暫時性錯誤：目標檔正被別的行程
// （防毒、索引服務、另一個讀狀態檔的工具）開著時，MoveFileEx 回這兩種錯誤。
func renameRetryable(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
