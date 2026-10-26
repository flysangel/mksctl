package apkrepo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T, content string) (string, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, "etc/apk/repositories")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return root, p
}

func TestEnsureAddsMissingRepoOnce(t *testing.T) {
	root, p := setup(t, "https://dl-cdn.alpinelinux.org/alpine/v3.20/main\n")
	repos := []string{"https://dl-cdn.alpinelinux.org/alpine/v3.20/community"}

	changed, err := Ensure(root, repos)
	require.NoError(t, err)
	assert.True(t, changed)
	changed, err = Ensure(root, repos)
	require.NoError(t, err)
	assert.False(t, changed)

	b, _ := os.ReadFile(p)
	assert.Equal(t, "https://dl-cdn.alpinelinux.org/alpine/v3.20/main\nhttps://dl-cdn.alpinelinux.org/alpine/v3.20/community\n", string(b))
}

func TestEnsureAddsNewlineWhenMissing(t *testing.T) {
	root, p := setup(t, "a")
	_, err := Ensure(root, []string{"b"})
	require.NoError(t, err)
	b, _ := os.ReadFile(p)
	assert.Equal(t, "a\nb\n", string(b))
}

func TestEnsureCommentDoesNotCount(t *testing.T) {
	root, p := setup(t, "#https://x/community\n")
	changed, err := Ensure(root, []string{"https://x/community"})
	require.NoError(t, err)
	assert.True(t, changed)
	b, _ := os.ReadFile(p)
	assert.Equal(t, "#https://x/community\nhttps://x/community\n", string(b))
}

func TestEnsureNothingToDo(t *testing.T) {
	root, _ := setup(t, "a\n")
	changed, err := Ensure(root, nil)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestEnsureCreatesMissingFile(t *testing.T) {
	root := t.TempDir()
	changed, err := Ensure(root, []string{"a"})
	require.NoError(t, err)
	assert.True(t, changed)
	b, _ := os.ReadFile(filepath.Join(root, "etc/apk/repositories"))
	assert.Equal(t, "a\n", string(b))
}
