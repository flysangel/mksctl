// Package identity 回答「現在這個行程是誰」：是否提權、是否為 SYSTEM。
// 安裝前檢查用它決定 machine 與 user 範圍的配方能不能裝。
package identity

// Identity 是行程的身分。
type Identity struct {
	// Elevated 表示可寫機器範圍：Unix 是 root，Windows 是提權的管理員或 SYSTEM。
	Elevated bool
	// System 表示以 SYSTEM（S-1-5-18）執行；只在 Windows 才會是 true。
	System bool
}
