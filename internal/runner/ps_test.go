package runner

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var winPlatform = platform.Platform{ID: "windows", Version: "26100", Layers: []string{"windows-server-2025", "windows-server", "windows"}}

var psFS = fstest.MapFS{
	"platforms/windows.ps1":        {Data: []byte("function Invoke-Native {}\n")},
	"platforms/windows-server.ps1": {Data: []byte("# server layer\n")},
	"recipes/demo/recipe.ps1":      {Data: []byte("function demo_install {}\nfunction demo_verify {}\nfunction demo_restore {}\n")},
	"recipes/nofunc/recipe.ps1":    {Data: []byte("function nofunc_install {}\nfunction nofunc_verify {}\n")},
}

func TestComposePSHasBOMAndPreamble(t *testing.T) {
	b, err := composePS(psFS, winPlatform, "demo", nil, ActInstall)
	require.NoError(t, err)
	assert.Equal(t, []byte{0xEF, 0xBB, 0xBF}, b[:3])
	s := string(b[3:])
	assert.Contains(t, s, "$ErrorActionPreference = 'Stop'")
	assert.Contains(t, s, "Set-StrictMode -Version Latest")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(s), "demo_install\ndemo_verify"))
	// 由通用到具體：windows 在 windows-server 之前，recipe 在最後。
	assert.Less(t, strings.Index(s, "function Invoke-Native"), strings.Index(s, "# server layer"))
	assert.Less(t, strings.Index(s, "# server layer"), strings.Index(s, "function demo_install"))
}

func TestComposePSRestoreCallsOnlyRestore(t *testing.T) {
	b, err := composePS(psFS, winPlatform, "demo", nil, ActRestore)
	require.NoError(t, err)
	s := strings.TrimSpace(string(b))
	assert.True(t, strings.HasSuffix(s, "\ndemo_restore"))
	assert.NotContains(t, s[strings.Index(s, "function demo_restore"):], "demo_verify\n")
	assert.False(t, strings.HasSuffix(s, "demo_install\ndemo_verify"))
}

func TestComposePSQuotesVars(t *testing.T) {
	b, err := composePS(psFS, winPlatform, "demo", map[string]string{"OS_VERSION": "a'b"}, ActInstall)
	require.NoError(t, err)
	assert.Contains(t, string(b), "$OS_VERSION = 'a''b'")
}

func TestComposePSRestoreMissingFunction(t *testing.T) {
	_, err := composePS(psFS, winPlatform, "nofunc", nil, ActRestore)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不支援 restore")
}

func TestComposePSBadVarName(t *testing.T) {
	_, err := composePS(psFS, winPlatform, "demo", map[string]string{"A B": "x"}, ActInstall)
	require.Error(t, err)
}

func TestPSRunnerRejectsBadEnvName(t *testing.T) {
	r := &PSRunner{FS: psFS, Stdout: io.Discard, Stderr: io.Discard}
	err := r.Run(context.Background(), winPlatform, recipe.Recipe{Name: "demo"}, map[string]string{"A=B": "x"}, ActInstall)
	require.Error(t, err)
}
