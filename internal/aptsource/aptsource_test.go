package aptsource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ubuntu = platform.Platform{ID: "ubuntu", Codename: "noble"}

func dockerSource() recipe.AptSource {
	return recipe.AptSource{
		File:       "docker.sources",
		URIs:       "https://download.docker.com/linux/{{.OSID}}",
		Suites:     "{{.Codename}}",
		Components: "stable",
		SignedBy:   "/etc/apt/keyrings/docker.asc",
		KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestRenderDockerSource(t *testing.T) {
	out, err := Render(dockerSource(), ubuntu)
	require.NoError(t, err)
	assert.Equal(t, "Types: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: noble\nComponents: stable\nSigned-By: /etc/apt/keyrings/docker.asc\n", string(out))
}

func TestRenderUnknownVariable(t *testing.T) {
	s := dockerSource()
	s.Suites = "{{.Nope}}"
	_, err := Render(s, ubuntu)
	assert.Error(t, err)
}

func TestFindConflictsOldList(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/docker.list",
		"deb [signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu noble stable\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Contains(t, cs[0].Fix, "rm /etc/apt/sources.list.d/docker.list")
	assert.Contains(t, cs[0].Fix, "rm /etc/apt/keyrings/docker.gpg")
}

func TestFindConflictsListWithArchAndTrailingSlash(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/d.list",
		"# 註解\ndeb [arch=amd64 signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu/ noble stable\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm /etc/apt/sources.list.d/d.list", cs[0].Fix)
}

func TestFindConflictsOtherSourcesFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/other.sources",
		"Types: deb\nURIs: https://a.example https://download.docker.com/linux/ubuntu\nSuites: noble\n\nTypes: deb\nURIs: https://b.example\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm /etc/apt/sources.list.d/other.sources", cs[0].Fix)
}

func TestFindConflictsSameFileSameSignedByIsIdempotent(t *testing.T) {
	root := t.TempDir()
	out, _ := Render(dockerSource(), ubuntu)
	writeFile(t, root, "etc/apt/sources.list.d/docker.sources", string(out))
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestFindConflictsSameFileDifferentSignedBy(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/docker.sources",
		"Types: deb\nURIs: https://download.docker.com/linux/ubuntu\nSuites: noble\nComponents: stable\nSigned-By: /etc/apt/keyrings/old.gpg\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm /etc/apt/sources.list.d/docker.sources", cs[0].Fix)
}

func TestFindConflictsNoDirOrUnrelated(t *testing.T) {
	root := t.TempDir()
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	assert.Empty(t, cs)
	writeFile(t, root, "etc/apt/sources.list.d/x.list", "deb https://x.example/ noble main\n")
	cs, err = FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	assert.Empty(t, cs)
}

func TestWriteDownloadsKeyAndWritesSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("KEYDATA"))
	}))
	defer srv.Close()
	s := dockerSource()
	s.KeyURL = srv.URL + "/gpg"
	root := t.TempDir()
	require.NoError(t, Write(context.Background(), root, s, ubuntu, srv.Client()))

	key, err := os.ReadFile(filepath.Join(root, "etc/apt/keyrings/docker.asc"))
	require.NoError(t, err)
	assert.Equal(t, "KEYDATA", string(key))
	st, _ := os.Stat(filepath.Join(root, "etc/apt/keyrings/docker.asc"))
	assert.Equal(t, os.FileMode(0o644), st.Mode().Perm())

	src, err := os.ReadFile(filepath.Join(root, "etc/apt/sources.list.d/docker.sources"))
	require.NoError(t, err)
	assert.Contains(t, string(src), "Suites: noble\n")
	// 寫完不留暫存檔
	ents, _ := os.ReadDir(filepath.Join(root, "etc/apt/sources.list.d"))
	assert.Len(t, ents, 1)
}

func TestWriteKeyDownloadFailureWritesNothing(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	s := dockerSource()
	s.KeyURL = srv.URL + "/gpg"
	root := t.TempDir()
	assert.Error(t, Write(context.Background(), root, s, ubuntu, srv.Client()))
	_, err := os.Stat(filepath.Join(root, "etc/apt/sources.list.d/docker.sources"))
	assert.True(t, os.IsNotExist(err))
}

func TestFixSharedKeyringNotRemoved(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/docker.list",
		"deb [signed-by=/etc/apt/keyrings/shared.gpg] https://download.docker.com/linux/ubuntu noble stable\n")
	writeFile(t, root, "etc/apt/sources.list.d/other.list",
		"deb [signed-by=/etc/apt/keyrings/shared.gpg] https://other.example noble main\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm /etc/apt/sources.list.d/docker.list", cs[0].Fix)
	assert.Contains(t, cs[0].Reason, "仍被其他來源使用")
}

func TestFixKeyringOutsideRemovableDirs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/docker.list",
		"deb [signed-by=/usr/share/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu noble stable\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.NotContains(t, cs[0].Fix, "keyrings")
	assert.Contains(t, cs[0].Reason, "不在可清除的位置")
}

func TestFixQuotesPathsWithSpace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/my docker.list",
		"deb [signed-by=/etc/apt/keyrings/mykey.gpg] https://download.docker.com/linux/ubuntu noble stable\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm '/etc/apt/sources.list.d/my docker.list' && rm /etc/apt/keyrings/mykey.gpg", cs[0].Fix)
}

func TestMultiStanzaReasonListsOtherURIs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/other.sources",
		"Types: deb\nURIs: https://download.docker.com/linux/ubuntu\n\nTypes: deb\nURIs: https://b.example\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Contains(t, cs[0].Reason, "這個檔還有其他來源：https://b.example")
}

func TestOneListTwoKeyringsAllCovered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/d.list",
		"deb [signed-by=/etc/apt/keyrings/a.gpg] https://download.docker.com/linux/ubuntu noble stable\n"+
			"deb [signed-by=/etc/apt/keyrings/b.gpg] https://download.docker.com/linux/ubuntu/ jammy stable\n")
	cs, err := FindConflicts(root, dockerSource(), ubuntu)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, "rm /etc/apt/sources.list.d/d.list && rm /etc/apt/keyrings/a.gpg && rm /etc/apt/keyrings/b.gpg", cs[0].Fix)
}

func TestWriteExpandsKeyURLTemplate(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		_, _ = w.Write([]byte("KEYDATA"))
	}))
	defer srv.Close()
	s := dockerSource()
	s.KeyURL = srv.URL + "/linux/{{.OSID}}/{{.Codename}}/gpg"
	require.NoError(t, Write(context.Background(), t.TempDir(), s, ubuntu, srv.Client()))
	assert.Equal(t, "/linux/ubuntu/noble/gpg", got)
}
