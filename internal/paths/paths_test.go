package paths

import (
	"testing"

	"github.com/flysangel/mksctl/internal/identity"
	"github.com/stretchr/testify/assert"
)

func winEnv() func(string) string {
	env := map[string]string{"ProgramData": `C:\ProgramData`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`}
	return func(k string) string { return env[k] }
}

func TestPathsWindows(t *testing.T) {
	s := For("windows", "", identity.Identity{}, winEnv())
	assert.Equal(t, `C:\ProgramData\mksctl\state.json`, s.MachineState)
	assert.Equal(t, `C:\Users\u\AppData\Local\mksctl\state.json`, s.UserState)
	assert.Equal(t, `C:\Users\u\AppData\Local\mksctl\cache\fetch`, s.FetchRoot)
	assert.Equal(t, `C:\Users\u\AppData\Local\mksctl\cache\tuf`, s.TUFCache)
	assert.Equal(t, `C:\ProgramData\mksctl\github-token`, s.TokenFile)
}

func TestPathsWindowsElevatedCachesInProgramData(t *testing.T) {
	s := For("windows", "", identity.Identity{Elevated: true}, winEnv())
	assert.Equal(t, `C:\ProgramData\mksctl\cache\fetch`, s.FetchRoot)
	assert.Equal(t, `C:\ProgramData\mksctl\cache\tuf`, s.TUFCache)
	assert.Equal(t, `C:\Users\u\AppData\Local\mksctl\state.json`, s.UserState)
}

func TestPathsLinuxUnchanged(t *testing.T) {
	s := For("linux", "/tmp/r", identity.Identity{}, func(string) string { return "" })
	assert.Equal(t, "/tmp/r/var/lib/mksctl/state.json", s.MachineState)
	assert.Equal(t, s.MachineState, s.UserState)
	assert.Equal(t, "/tmp/r/var/cache/mksctl/fetch", s.FetchRoot)
	assert.Equal(t, "/tmp/r/var/cache/mksctl/tuf", s.TUFCache)
	assert.Equal(t, "/tmp/r/etc/mksctl/github-token", s.TokenFile)
}

// ProgramData 空時退回系統預設，不能組出 \mksctl\state.json 寫到目前磁碟的根。
func TestPathsWindowsEmptyProgramData(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`}
	s := For("windows", "", identity.Identity{Elevated: true}, func(k string) string { return env[k] })
	assert.Equal(t, `C:\ProgramData\mksctl\state.json`, s.MachineState)
	assert.Equal(t, `C:\ProgramData\mksctl\github-token`, s.TokenFile)
	assert.Equal(t, `C:\ProgramData\mksctl\cache\fetch`, s.FetchRoot)
}

// LOCALAPPDATA 空時 UserState 留空，由 install 對 user 配方報差異；
// 快取改放 ProgramData，不組出以 \ 開頭的路徑。
func TestPathsWindowsEmptyLocalAppData(t *testing.T) {
	env := map[string]string{"ProgramData": `D:\PD`}
	s := For("windows", "", identity.Identity{}, func(k string) string { return env[k] })
	assert.Empty(t, s.UserState)
	assert.Equal(t, `D:\PD\mksctl\state.json`, s.MachineState)
	assert.Equal(t, `D:\PD\mksctl\cache\fetch`, s.FetchRoot)
	assert.Equal(t, `D:\PD\mksctl\cache\tuf`, s.TUFCache)
}
