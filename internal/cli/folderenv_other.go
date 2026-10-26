//go:build !windows

package cli

// folderEnv 在非 Windows 平台原樣回傳 getenv。Known Folder 是 Windows 的概念，
// 見 folderenv_windows.go。
func folderEnv(getenv func(string) string) func(string) string {
	return getenv
}
