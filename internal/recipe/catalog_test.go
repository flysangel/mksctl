package recipe

import (
	"bytes"
	"os/exec"
	"regexp"
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
	root := strings.TrimSpace(string(top))

	list := exec.Command("git", "ls-files", "-z")
	list.Dir = root
	out, err := list.Output()
	require.NoError(t, err)

	re := regexp.MustCompile(`[0-9a-f]{64}`)
	for _, f := range strings.Split(string(out), "\x00") {
		if f == "" || strings.HasPrefix(f, "trust/") ||
			strings.HasPrefix(f, "testdata/") || strings.Contains(f, "/testdata/") {
			continue
		}
		b, err := exec.Command("cat", root+"/"+f).Output()
		if err != nil || bytes.IndexByte(b, 0) >= 0 { // 刪除中的檔或二進位檔略過
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if re.MatchString(line) {
				t.Errorf("%s:%d 出現 64 位十六進位字串", f, i+1)
			}
		}
	}
}
