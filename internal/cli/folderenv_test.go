package cli

import "testing"

func TestFolderEnvPassesOtherKeys(t *testing.T) {
	get := folderEnv(func(k string) string { return "v:" + k })
	if got := get("MKSCTL_RUN"); got != "v:MKSCTL_RUN" {
		t.Fatalf("got %q", got)
	}
}
