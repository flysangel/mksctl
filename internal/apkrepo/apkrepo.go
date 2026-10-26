// Package apkrepo 維護 alpine 的 /etc/apk/repositories。
package apkrepo

import (
	"os"
	"path/filepath"
	"strings"
)

// Ensure 把缺的行原樣附加到 etc/apk/repositories，保留既有內容。
// 註解行（#開頭）不算已存在，否則被註解掉的套件庫永遠不會啟用。
func Ensure(sysroot string, repos []string) (bool, error) {
	path := filepath.Join(sysroot, "etc/apk/repositories")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			have[l] = true
		}
	}
	var add []string
	for _, r := range repos {
		if !have[strings.TrimSpace(r)] {
			add = append(add, r)
			have[strings.TrimSpace(r)] = true
		}
	}
	if len(add) == 0 {
		return false, nil
	}
	out := string(data)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += strings.Join(add, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(out), mode); err != nil {
		return false, err
	}
	return true, nil
}
