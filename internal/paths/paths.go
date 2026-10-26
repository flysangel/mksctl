// Package paths 依平台與身分決定 mksctl 的狀態檔、暫存與 token 路徑。
// 它只組字串，不碰檔案系統。
package paths

import (
	"path"
	"strings"

	"github.com/flysangel/mksctl/internal/identity"
)

// Set 是一組路徑。Linux 沒有 user 範圍，UserState 與 MachineState 相同。
// Windows 的 LOCALAPPDATA 是空的時，UserState 是空字串：install 對 user 配方報差異，不猜路徑。
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
		// 環境變數空的時候，直接接字串會得到 \mksctl\…，寫到目前磁碟的根。
		// ProgramData 的位置固定，可以退回預設值；使用者的 LOCALAPPDATA 猜不到，留空給 install 擋。
		pd := getenv("ProgramData")
		if pd == "" {
			pd = defaultProgramData
		}
		programData := winJoin(pd, "mksctl")
		cache, userState := programData, ""
		if la := getenv("LOCALAPPDATA"); la != "" {
			local := winJoin(la, "mksctl")
			userState = winJoin(local, "state.json")
			if !id.Elevated {
				cache = local
			}
		}
		return Set{
			MachineState: winJoin(programData, "state.json"),
			UserState:    userState,
			FetchRoot:    winJoin(cache, "cache", "fetch"),
			TUFCache:     winJoin(cache, "cache", "tuf"),
			TokenFile:    winJoin(programData, "github-token"),
		}
	}
	// Linux 目標的路徑固定用 /，不依賴執行的主機（對稱於上面 Windows 固定用反斜線）。
	state := path.Join(sysroot, "var/lib/mksctl/state.json")
	return Set{
		MachineState: state,
		UserState:    state,
		FetchRoot:    path.Join(sysroot, "var/cache/mksctl/fetch"),
		TUFCache:     path.Join(sysroot, "var/cache/mksctl/tuf"),
		TokenFile:    path.Join(sysroot, "etc/mksctl/github-token"),
	}
}

// defaultProgramData 是 ProgramData 環境變數缺席時的位置（Windows 的出廠值）。
const defaultProgramData = `C:\ProgramData`

func winJoin(parts ...string) string {
	return strings.Join(parts, `\`)
}
