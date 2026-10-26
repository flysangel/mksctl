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
