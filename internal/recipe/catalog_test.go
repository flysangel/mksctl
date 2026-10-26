package recipe

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/flysangel/mksctl"
	"github.com/flysangel/mksctl/internal/platform"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedCatalogValid(t *testing.T) {
	c, err := Load(mksctl.FS)
	require.NoError(t, err)
	defs, err := platform.LoadDefs(mksctl.FS)
	require.NoError(t, err)
	for _, e := range Validate(c, defs) {
		t.Error(e)
	}
}

// 雜湊只能放在 trust/，散落各處的 sha256 會與可信來源脫鉤。
func TestNoSha256OutsideTrust(t *testing.T) {
	top, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	require.NoError(t, err)
	hits, err := scanForSha256(strings.TrimSpace(string(top)))
	require.NoError(t, err)
	for _, h := range hits {
		t.Error(h)
	}
}

// 掃描函式本身要有測試：之前只認小寫、又吞掉讀檔錯誤，漏洞不會被發現。
func TestScanForSha256(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	}
	git("init", "-q")
	upper := strings.Repeat("AB", 32)
	lower := strings.Repeat("cd", 32)
	write("a.txt", "x\nsum: "+upper+"\n")
	write("b.txt", lower+"\n")
	write("trust/hashes.yaml", lower)
	write("pkg/testdata/f", lower)
	write("tests/verified.conf", lower)
	write("bin.dat", "\x00"+lower)
	write("ok.txt", "no hash here "+strings.Repeat("a", 63))
	git("add", "-A")

	hits, err := scanForSha256(dir)
	require.NoError(t, err)
	require.Len(t, hits, 2)
	require.Contains(t, hits[0], "a.txt:2")
	require.Contains(t, hits[1], "b.txt:1")

	// 已追蹤卻讀不到的檔（非「不存在」）要讓掃描失敗，不能靜默略過。
	write("c.txt", "x")
	git("add", "c.txt")
	require.NoError(t, os.Chmod(filepath.Join(dir, "c.txt"), 0))
	// root 讀得了 mode 0 的檔；Windows 的 Chmod 不能拿掉讀取權限，兩者都無法造出「讀不到」。
	if os.Geteuid() != 0 && runtime.GOOS != "windows" {
		_, err = scanForSha256(dir)
		require.Error(t, err)
	}
	// 已刪除的追蹤檔（不存在）照舊略過。
	require.NoError(t, os.Remove(filepath.Join(dir, "c.txt")))
	_, err = scanForSha256(dir)
	require.NoError(t, err)
}

// scanForSha256 回報 root（git repo）裡追蹤的檔案出現 64 位十六進位字串的位置。
// trust/ 是雜湊的正本；tests/verified.conf 記的是配方內容的指紋，不是下載檔的雜湊，
// 沒有可脫鉤的來源；testdata 是測試固定資料。不分大小寫：大寫雜湊一樣會與 trust/ 脫鉤。
func scanForSha256(root string) ([]string, error) {
	list := exec.Command("git", "ls-files", "-z")
	list.Dir = root
	out, err := list.Output()
	if err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`[0-9a-fA-F]{64}`)
	var hits []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || strings.HasPrefix(f, "trust/") ||
			f == "tests/verified.conf" || strings.HasPrefix(f, "testdata/") || strings.Contains(f, "/testdata/") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, f))
		if os.IsNotExist(err) { // 工作樹裡已刪除、index 還在的檔
			continue
		}
		if err != nil {
			return nil, err // 其他讀檔錯誤不能略過，否則掃描會靜默漏檔
		}
		if bytes.IndexByte(b, 0) >= 0 { // 二進位檔略過
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d 出現 64 位十六進位字串", f, i+1))
			}
		}
	}
	return hits, nil
}
