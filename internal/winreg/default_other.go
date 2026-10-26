//go:build !windows

package winreg

// Default 在非 Windows 平台沒有登錄區，回傳 nil。
func Default() Store { return nil }
