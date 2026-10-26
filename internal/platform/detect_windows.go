//go:build windows

package platform

import "errors"

// Detect 在 Windows 上先不實作，只讓整個模組能交叉編譯。
func Detect(sysroot string) (Platform, error) {
	return Platform{}, errors.New("Windows 平台尚未支援")
}
