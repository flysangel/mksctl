//go:build windows

package cli

import (
	"strings"

	"golang.org/x/sys/windows"
)

// folderEnv 包住 getenv：鍵是 LOCALAPPDATA 或 ProgramData 時，以目前行程 token 的
// Known Folder 為準，查不到才退回 getenv。
//
// 理由：Start-Process -Credential、runas、排程與服務都可能把父行程的環境區塊原樣
// 傳給子行程，LOCALAPPDATA 因此指向別的帳號的 profile，user 狀態檔就寫到別人那裡。
// Known Folder 依 token 解析，不受環境變數影響。
func folderEnv(getenv func(string) string) func(string) string {
	return func(key string) string {
		var id *windows.KNOWNFOLDERID
		switch strings.ToUpper(key) {
		case "LOCALAPPDATA":
			id = windows.FOLDERID_LocalAppData
		case "PROGRAMDATA":
			id = windows.FOLDERID_ProgramData
		default:
			return getenv(key)
		}
		if p, err := tokenKnownFolder(id); err == nil && p != "" {
			return p
		}
		return getenv(key)
	}
}

// tokenKnownFolder 帶目前行程的 token 查 Known Folder。
// hToken 為 NULL 時，Shell 用行程的環境區塊展開 User Shell Folders 裡的 %USERPROFILE%，
// 而 USERPROFILE 同樣是繼承來的；帶 token 才改由 ExpandEnvironmentStringsForUser 展開。
func tokenKnownFolder(id *windows.KNOWNFOLDERID) (string, error) {
	var t windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE|windows.TOKEN_DUPLICATE, &t)
	if err != nil {
		return "", err
	}
	defer t.Close()
	return t.KnownFolderPath(id, 0)
}
