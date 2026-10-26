//go:build windows

package platform

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

// Detect 讀 HKLM 的 InstallationType 與 CurrentBuild 判斷平台。
// sysroot 在 Windows 上不用，保留是為了與 detect_unix.go 同簽名。
func Detect(sysroot string) (Platform, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return Platform{}, fmt.Errorf("讀不到 Windows 版本登錄：%w", err)
	}
	defer k.Close()
	typ, _, err := k.GetStringValue("InstallationType")
	if err != nil {
		return Platform{}, fmt.Errorf("讀不到 InstallationType：%w", err)
	}
	build, _, err := k.GetStringValue("CurrentBuild")
	if err != nil {
		return Platform{}, fmt.Errorf("讀不到 CurrentBuild：%w", err)
	}
	return WindowsPlatform(typ, build), nil
}
