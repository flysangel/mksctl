package aptsource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testMirror() platform.AptMirror {
	return platform.AptMirror{
		URIs:       "http://mirror.example/ubuntu/",
		Suites:     []string{"{{.Codename}}", "{{.Codename}}-updates"},
		Components: "main universe",
	}
}

const cloudSources = "Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble noble-updates\nComponents: main\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n"

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	require.NoError(t, err)
	return string(b)
}

func TestRenderMirror(t *testing.T) {
	out, err := RenderMirror(testMirror(), ubuntu)
	require.NoError(t, err)
	assert.Equal(t, "Types: deb\nURIs: http://mirror.example/ubuntu/\nSuites: noble noble-updates\nComponents: main universe\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n", string(out))
}

func TestWriteMirrorBacksUpOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "etc/apt/sources.list.d/ubuntu.sources", cloudSources)

	changed, err := WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, cloudSources, readFile(t, root, "etc/apt/sources.list.d/ubuntu.sources.bk"))
	assert.Contains(t, readFile(t, root, "etc/apt/sources.list.d/ubuntu.sources"), "URIs: http://mirror.example/ubuntu/\n")

	// 重跑：內容相同不寫，備份也不能被鏡像內容蓋掉。
	changed, err = WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, cloudSources, readFile(t, root, "etc/apt/sources.list.d/ubuntu.sources.bk"))

	// 鏡像設定改了：照樣改寫，但不再備份。
	m := testMirror()
	m.Components = "main"
	changed, err = WriteMirror(root, m, ubuntu)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, cloudSources, readFile(t, root, "etc/apt/sources.list.d/ubuntu.sources.bk"))
}

func TestWriteMirrorNoOriginalNoBackup(t *testing.T) {
	root := t.TempDir()
	changed, err := WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.NoFileExists(t, filepath.Join(root, "etc/apt/sources.list.d/ubuntu.sources.bk"))
}

func TestWriteMirrorClearsLegacySourcesList(t *testing.T) {
	root := t.TempDir()
	legacy := "# 註解\ndeb http://free.nchc.org.tw/ubuntu/ noble main\ndeb-src http://free.nchc.org.tw/ubuntu/ noble main\n"
	writeFile(t, root, "etc/apt/sources.list", legacy)

	changed, err := WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, legacy, readFile(t, root, "etc/apt/sources.list.bk"))
	assert.Empty(t, parseList(readFile(t, root, "etc/apt/sources.list")), "清空後不能留任何 deb 行")

	changed, err = WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, legacy, readFile(t, root, "etc/apt/sources.list.bk"))
}

// sources.list 只有註解（ubuntu 24.04 起的預設）時不動它，也不備份。
func TestWriteMirrorLeavesCommentOnlySourcesList(t *testing.T) {
	root := t.TempDir()
	orig := "# Ubuntu sources have moved to /etc/apt/sources.list.d/ubuntu.sources\n"
	writeFile(t, root, "etc/apt/sources.list", orig)
	_, err := WriteMirror(root, testMirror(), ubuntu)
	require.NoError(t, err)
	assert.Equal(t, orig, readFile(t, root, "etc/apt/sources.list"))
	assert.NoFileExists(t, filepath.Join(root, "etc/apt/sources.list.bk"))
}

func TestWriteConfOnlyWhenDifferent(t *testing.T) {
	root := t.TempDir()
	conf := map[string]string{"20auto-upgrades": "APT::Periodic::Unattended-Upgrade \"0\";\n"}
	changed, err := WriteConf(root, conf)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, conf["20auto-upgrades"], readFile(t, root, "etc/apt/apt.conf.d/20auto-upgrades"))
	changed, err = WriteConf(root, conf)
	require.NoError(t, err)
	assert.False(t, changed)
}

// 檔名帶路徑分隔字元會寫到 apt.conf.d 以外，直接拒絕。
func TestWriteConfRejectsPath(t *testing.T) {
	_, err := WriteConf(t.TempDir(), map[string]string{"../evil": "x"})
	assert.Error(t, err)
}
