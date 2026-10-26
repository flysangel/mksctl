//go:build windows

package identity

import "golang.org/x/sys/windows"

// Current 回傳目前行程的身分。os.Geteuid 在 Windows 回 -1，不能用，改讀行程 token。
// 讀不到 token 時當作未提權、非 SYSTEM：寧可擋下 machine 配方，也不放行。
func Current() Identity {
	token := windows.GetCurrentProcessToken()
	system := false
	if u, err := token.GetTokenUser(); err == nil && u.User.Sid != nil {
		system = u.User.Sid.IsWellKnown(windows.WinLocalSystemSid)
	}
	return Identity{Elevated: system || token.IsElevated(), System: system}
}
