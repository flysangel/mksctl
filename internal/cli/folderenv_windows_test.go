//go:build windows

package cli

import (
	"testing"

	"golang.org/x/sys/windows"
)

func TestFolderEnvIgnoresInheritedEnv(t *testing.T) {
	get := folderEnv(func(string) string { return `C:\Users\someone-else\fake` })
	for key, id := range map[string]*windows.KNOWNFOLDERID{
		"LOCALAPPDATA": windows.FOLDERID_LocalAppData,
		"ProgramData":  windows.FOLDERID_ProgramData,
	} {
		want, err := windows.KnownFolderPath(id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := get(key); got != want {
			t.Errorf("%s = %q，要 %q", key, got, want)
		}
	}
}
