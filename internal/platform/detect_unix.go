//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// Detect 讀 <sysroot>/etc/os-release 解析平台，再補上執行期狀態。
// 平台判斷集中在這裡，Windows 版（detect_windows.go）才有單一替換點。
func Detect(sysroot string) (Platform, error) {
	b, err := os.ReadFile(filepath.Join(sysroot, "etc/os-release"))
	if err != nil {
		return Platform{}, fmt.Errorf("讀不到 os-release：%w", err)
	}
	p, err := Parse(b)
	if err != nil {
		return Platform{}, err
	}
	p.InitRunning, p.InContainer = DetectRuntime(sysroot)
	return p, nil
}
