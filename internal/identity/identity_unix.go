//go:build !windows

package identity

import "os"

// Current 回傳目前行程的身分。Unix 只看 euid，沒有 SYSTEM 的概念。
func Current() Identity {
	return Identity{Elevated: os.Geteuid() == 0}
}
