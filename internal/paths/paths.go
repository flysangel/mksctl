// Package paths 依平台與身分決定 mksctl 的狀態檔、暫存與 token 路徑。
// 它只組字串，不碰檔案系統。
package paths

import (
	"path/filepath"
	"strings"

	"github.com/flysangel/mksctl/internal/identity"
)

// Set 是一組路徑。Linux 沒有 user 範圍，UserState 與 MachineState 相同。
type Set struct {
	MachineState string
	UserState    string
	FetchRoot    string
	TUFCache     string
	TokenFile    string
}

// For 組出路徑。goos 由呼叫端傳入（而不是讀 runtime.GOOS），測試才能在 Linux 驗 Windows 路徑。
// Windows 的分隔符號固定用反斜線，不依賴執行的主機。
func For(goos, sysroot string, id identity.Identity, getenv func(string) string) Set {
	if goos == "windows" {
		programData := winJoin(getenv("ProgramData"), "mksctl")
		local := winJoin(getenv("LOCALAPPDATA"), "mksctl")
		cache := local
		if id.Elevated {
			cache = programData
		}
		return Set{
			MachineState: winJoin(programData, "state.json"),
			UserState:    winJoin(local, "state.json"),
			FetchRoot:    winJoin(cache, "cache", "fetch"),
			TUFCache:     winJoin(cache, "cache", "tuf"),
			TokenFile:    winJoin(programData, "github-token"),
		}
	}
	state := filepath.Join(sysroot, "var/lib/mksctl/state.json")
	return Set{
		MachineState: state,
		UserState:    state,
		FetchRoot:    filepath.Join(sysroot, "var/cache/mksctl/fetch"),
		TUFCache:     filepath.Join(sysroot, "var/cache/mksctl/tuf"),
		TokenFile:    filepath.Join(sysroot, "etc/mksctl/github-token"),
	}
}

func winJoin(parts ...string) string {
	return strings.Join(parts, `\`)
}
