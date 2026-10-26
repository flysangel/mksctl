package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func mkdirAll(t *testing.T, root, rel string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, rel), 0o755))
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestParseUbuntu2604(t *testing.T) {
	p, err := Parse(readFixture(t, "ubuntu-26.04.os-release"))
	require.NoError(t, err)
	assert.Equal(t, []string{"ubuntu-26.04", "ubuntu", "apt", "common"}, p.Layers)
	assert.Equal(t, "resolute", p.Codename)
}

func TestParseUbuntu2404(t *testing.T) {
	p, err := Parse(readFixture(t, "ubuntu-24.04.os-release"))
	require.NoError(t, err)
	assert.Equal(t, "24.04", p.Version)
	assert.Equal(t, "noble", p.Codename)
}

func TestParseAlpineUsesMajorMinor(t *testing.T) {
	p, _ := Parse([]byte("ID=alpine\nVERSION_ID=3.22.2\n"))
	assert.Equal(t, []string{"alpine-3.22", "apk", "common"}, p.Layers)
	assert.Equal(t, "3.22", p.Version)
	assert.Equal(t, "", p.Codename)
}

func TestParseRejectsDebian(t *testing.T) {
	_, err := Parse([]byte("ID=debian\nVERSION_ID=13\n"))
	assert.ErrorContains(t, err, "debian")
}

func TestApplyMissingExactLayer(t *testing.T) {
	defs := Defs{"ubuntu": {}, "apt": {Provides: []string{"apt", "systemd"}}, "common": {}}
	p := defs.Apply(Platform{Layers: []string{"ubuntu-28.04", "ubuntu", "apt", "common"}})
	assert.False(t, p.HasExactLayer)
	assert.True(t, p.Provides["systemd"])
}

func TestApplyExactLayer(t *testing.T) {
	defs := Defs{"ubuntu-26.04": {}, "common": {}}
	p := defs.Apply(Platform{Layers: []string{"ubuntu-26.04", "common"}})
	assert.True(t, p.HasExactLayer)
}

func TestApplyRuntimeCapabilities(t *testing.T) {
	defs := Defs{"common": {}}
	p := defs.Apply(Platform{Layers: []string{"common"}, InitRunning: true, InContainer: true})
	assert.True(t, p.Provides["init"])
	assert.False(t, p.Provides["kernel"])
}

func TestDetectRuntimeLXC(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, root, "run/systemd/system")
	writeFile(t, root, "proc/1/environ", "container=lxc\x00")
	initRunning, inContainer := DetectRuntime(root)
	assert.True(t, initRunning)
	assert.True(t, inContainer)
}

func TestDetectRuntimeBareOpenrc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "run/openrc/softlevel", "")
	initRunning, inContainer := DetectRuntime(root)
	assert.True(t, initRunning)
	assert.False(t, inContainer)
}

func TestLoadDefs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "platforms/apt.yaml", "provides: [apt, systemd]\nrepositories: [main]\n")
	d, err := LoadDefs(os.DirFS(root))
	require.NoError(t, err)
	assert.Equal(t, []string{"apt", "systemd"}, d["apt"].Provides)
	assert.Equal(t, []string{"main"}, d["apt"].Repositories)
}
