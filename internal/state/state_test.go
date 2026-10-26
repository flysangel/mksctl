package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecideSkipSameVersion(t *testing.T) {
	f := File{Recipes: map[string]Entry{"helm": {Version: "v3.19.0"}}}
	assert.Equal(t, Skip, Decide(f, recipe.Recipe{Name: "helm", Versioned: true}, "v3.19.0"))
	assert.Equal(t, Run, Decide(f, recipe.Recipe{Name: "helm", Versioned: true}, "v3.19.1"))
	assert.Equal(t, Run, Decide(f, recipe.Recipe{Name: "ntp"}, ""))
	assert.Equal(t, Run, Decide(File{}, recipe.Recipe{Name: "helm", Versioned: true}, "v3.19.0"))
}

func TestLoadMissingIsEmpty(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "none.json"))
	require.NoError(t, err)
	assert.Empty(t, f.Recipes)
}

func TestLoadCorruptIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o644))
	_, err := Load(p)
	require.Error(t, err)
}

func TestSaveIsAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	p := filepath.Join(dir, "state.json")
	at := time.Date(2026, 10, 26, 1, 2, 3, 0, time.UTC)
	f := File{Recipes: map[string]Entry{"helm": {Version: "v1", InstalledAt: at}}}
	require.NoError(t, f.Save(p))

	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, ents, 1)
	assert.Equal(t, "state.json", ents[0].Name())

	got, err := Load(p)
	require.NoError(t, err)
	assert.Equal(t, "v1", got.Recipes["helm"].Version)
	assert.True(t, at.Equal(got.Recipes["helm"].InstalledAt))

	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"installed_at": "2026-10-26T01:02:03Z"`)
}

func TestSaveWritesUTC(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	at := time.Date(2026, 10, 26, 9, 2, 3, 0, time.FixedZone("x", 8*3600))
	require.NoError(t, File{Recipes: map[string]Entry{"a": {Version: "v", InstalledAt: at}}}.Save(p))
	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"installed_at": "2026-10-26T01:02:03Z"`)
}
