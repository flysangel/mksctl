//go:build windows

package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestFolderEnvIgnoresInheritedEnv(t *testing.T) {
	fake := `C:\Users\someone-else`
	t.Setenv("USERPROFILE", fake)
	t.Setenv("LOCALAPPDATA", fake+`\AppData\Local`)
	t.Setenv("ProgramData", fake+`\PD`)
	get := folderEnv(func(string) string { return fake + `\fallback` })

	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
		t.Fatal(err)
	}
	defer tok.Close()
	prof, err := tok.GetUserProfileDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := get("LOCALAPPDATA"), filepath.Join(prof, "AppData", "Local"); !strings.EqualFold(got, want) {
		t.Errorf("LOCALAPPDATA = %q，要 %q", got, want)
	}
	pd := get("ProgramData")
	if strings.Contains(pd, "someone-else") || pd == "" {
		t.Errorf("ProgramData = %q", pd)
	}
}
