//go:build !windows

package state

// renameRetryable 在 Unix 一律回 false：rename 是原子操作，不會因為別人開著目標檔而失敗。
func renameRetryable(error) bool { return false }
