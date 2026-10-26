// Package aptsource 產生 deb822 格式的 apt 來源，並在寫入前偵測舊來源檔的衝突。
//
// 為什麼要偵測衝突：同一個套件庫 URI 若以兩個不同的 Signed-By 出現，
// apt 會拒讀「全部」來源清單，整台機器的 apt 從此不能用。
// 舊 provision 腳本留下的 .list 檔正是這類來源，所以寫入前必須先查。
package aptsource

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/flysangel/mksctl/internal/platform"
	"github.com/flysangel/mksctl/internal/recipe"
)

const sourcesDir = "etc/apt/sources.list.d"

// Conflict 描述一個會讓 apt 壞掉的既有來源。
type Conflict struct {
	File   string // 目標機器上的絕對路徑
	URI    string
	Reason string
	Fix    string // 可直接貼上執行的清理指令
}

// expand 代入 {{.OSID}} 與 {{.Codename}}。missingkey=error 讓拼錯的變數直接失敗，
// 不會靜默產出壞掉的來源檔。
func expand(field, text string, p platform.Platform) (string, error) {
	t, err := template.New(field).Option("missingkey=error").Parse(text)
	if err != nil {
		return "", fmt.Errorf("aptsource: 解析 %s 失敗：%w", field, err)
	}
	var b bytes.Buffer
	data := map[string]string{"OSID": p.ID, "Codename": p.Codename}
	if err := t.Execute(&b, data); err != nil {
		return "", fmt.Errorf("aptsource: 代入 %s 失敗：%w", field, err)
	}
	return b.String(), nil
}

type rendered struct {
	uris, suites, components, signedBy string
}

func renderFields(s recipe.AptSource, p platform.Platform) (rendered, error) {
	var r rendered
	var err error
	for _, f := range []struct {
		name string
		in   string
		out  *string
	}{
		{"URIs", s.URIs, &r.uris},
		{"Suites", s.Suites, &r.suites},
		{"Components", s.Components, &r.components},
		{"SignedBy", s.SignedBy, &r.signedBy},
	} {
		if *f.out, err = expand(f.name, f.in, p); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Render 回傳 deb822 內容，欄位順序固定。
func Render(s recipe.AptSource, p platform.Platform) ([]byte, error) {
	r, err := renderFields(s, p)
	if err != nil {
		return nil, err
	}
	out := fmt.Sprintf("Types: deb\nURIs: %s\nSuites: %s\nComponents: %s\nSigned-By: %s\n",
		r.uris, r.suites, r.components, r.signedBy)
	return []byte(out), nil
}

// entry 是來源檔裡的一筆來源。
type entry struct {
	uris     []string
	signedBy string
}

func normURI(u string) string { return strings.TrimRight(u, "/") }

// parseList 解析單行格式：deb [opts] URI suite comps...。
func parseList(data string) []entry {
	var out []entry
	for _, line := range strings.Split(data, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "deb" && f[0] != "deb-src") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), f[0]))
		var e entry
		if strings.HasPrefix(rest, "[") {
			end := strings.Index(rest, "]")
			if end < 0 {
				continue
			}
			for _, opt := range strings.Fields(rest[1:end]) {
				if k, v, ok := strings.Cut(opt, "="); ok && strings.EqualFold(k, "signed-by") {
					e.signedBy = v
				}
			}
			rest = strings.TrimSpace(rest[end+1:])
		}
		rf := strings.Fields(rest)
		if len(rf) == 0 {
			continue
		}
		e.uris = []string{rf[0]}
		out = append(out, e)
	}
	return out
}

// parseSources 解析 deb822 格式：空行分隔段落，URIs 可有多個，欄位可續行。
func parseSources(data string) []entry {
	var out []entry
	var cur entry
	var key string
	has := false
	flush := func() {
		if has {
			out = append(out, cur)
		}
		cur, key, has = entry{}, "", false
	}
	apply := func(k, v string) {
		switch strings.ToLower(k) {
		case "uris":
			cur.uris = append(cur.uris, strings.Fields(v)...)
			has = true
		case "signed-by":
			if cur.signedBy == "" {
				cur.signedBy = strings.TrimSpace(v)
			}
			has = true
		}
	}
	for _, line := range strings.Split(data, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			flush()
		case strings.HasPrefix(line, "#"):
		case line[0] == ' ' || line[0] == '\t':
			if key != "" {
				apply(key, line)
			}
		default:
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(k)
			apply(key, v)
		}
	}
	flush()
	return out
}

// FindConflicts 掃 sysroot 底下的 etc/apt/sources.list.d/*.list 與 *.sources。
// 衝突有兩種：同一個 URI 出現在目標檔以外的檔案，或目標檔已存在但 Signed-By 不同。
// 同檔同 Signed-By 是重跑，不算衝突。
func FindConflicts(sysroot string, s recipe.AptSource, p platform.Platform) ([]Conflict, error) {
	r, err := renderFields(s, p)
	if err != nil {
		return nil, err
	}
	ours := map[string]bool{}
	for _, u := range strings.Fields(r.uris) {
		ours[normURI(u)] = true
	}
	ents, err := os.ReadDir(filepath.Join(sysroot, sourcesDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type parsed struct {
		name     string
		isList   bool
		isTarget bool
		entries  []entry
	}
	var files []parsed
	for _, de := range ents {
		name := de.Name()
		isList := strings.HasSuffix(name, ".list")
		if de.IsDir() || (!isList && !strings.HasSuffix(name, ".sources")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sysroot, sourcesDir, name))
		if err != nil {
			return nil, err
		}
		pf := parsed{name: name, isList: isList, isTarget: name == s.File}
		if isList {
			pf.entries = parseList(string(data))
		} else {
			pf.entries = parseSources(string(data))
		}
		files = append(files, pf)
	}

	// 第一輪：找出每個檔的衝突 URI 與其他（不衝突的）URI。
	type hit struct {
		pf        parsed
		conflict  []string
		others    []string
		keyrings  []string // 衝突行的 signed-by，已去重
		abs       string
		isRemoved bool
	}
	var hits []*hit
	removed := map[string]bool{}
	for _, pf := range files {
		h := &hit{pf: pf, abs: "/" + sourcesDir + "/" + pf.name}
		seenKR := map[string]bool{}
		for _, e := range pf.entries {
			isConf := false
			for _, u := range e.uris {
				if ours[normURI(u)] && !(pf.isTarget && e.signedBy == r.signedBy) {
					isConf = true
					h.conflict = append(h.conflict, u)
				}
			}
			if !isConf {
				h.others = append(h.others, e.uris...)
				continue
			}
			if e.signedBy != "" && e.signedBy != r.signedBy && !seenKR[e.signedBy] {
				seenKR[e.signedBy] = true
				h.keyrings = append(h.keyrings, e.signedBy)
			}
		}
		if len(h.conflict) > 0 {
			hits = append(hits, h)
			removed[pf.name] = true
		}
	}

	// 其餘（會留下的）檔案還在引用的 keyring，不能刪。
	stillUsed := map[string]bool{}
	for _, pf := range files {
		if removed[pf.name] {
			continue
		}
		for _, e := range pf.entries {
			if e.signedBy != "" {
				stillUsed[path.Clean(e.signedBy)] = true
			}
		}
	}

	var cs []Conflict
	for _, h := range hits {
		c := Conflict{File: h.abs, URI: strings.Join(h.conflict, " ")}
		fix := "rm " + shellQuote(h.abs)
		if h.pf.isTarget {
			c.Reason = fmt.Sprintf("同一個來源檔的 Signed-By 與要寫的 %q 不同", r.signedBy)
		} else {
			c.Reason = "同一個 URI 已出現在其他來源檔，apt 會因 Signed-By 不一致而拒讀全部來源"
		}
		if len(h.others) > 0 {
			c.Reason += "；這個檔還有其他來源：" + strings.Join(h.others, " ") + "（整檔刪除會一併移除）"
		}
		if h.pf.isList {
			for _, kr := range h.keyrings {
				switch {
				case stillUsed[path.Clean(kr)]:
					c.Reason += fmt.Sprintf("；keyring %s 仍被其他來源使用，不建議刪", kr)
				case !removableKeyring(kr):
					c.Reason += fmt.Sprintf("；keyring %s 不在可清除的位置（只清 /etc/apt/keyrings/ 與 /etc/apt/trusted.gpg.d/），不建議刪", kr)
				default:
					fix += " && rm " + shellQuote(kr)
				}
			}
		}
		c.Fix = fix
		cs = append(cs, c)
	}
	return cs, nil
}

var safeShell = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote 讓路徑可以安全貼進 shell：安全字元原樣輸出，其餘用單引號包起來。
func shellQuote(s string) string {
	if safeShell.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// removableKeyring 只有明確由 provision 放置的目錄才建議刪；
// /usr/share/keyrings 之類屬於套件，刪了會壞套件。
func removableKeyring(k string) bool {
	k = path.Clean(k)
	return strings.HasPrefix(k, "/etc/apt/keyrings/") || strings.HasPrefix(k, "/etc/apt/trusted.gpg.d/")
}

// writeAtomic 先寫暫存檔再 rename，避免中斷時留下半個檔。
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mksctl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // rename 成功後這步是 no-op
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Write 下載金鑰（原樣存放，不論 ASCII-armored 或二進位）並寫入 .sources。
// 金鑰先成功才寫來源檔，下載失敗時不留下指向不存在金鑰的來源。
// 本函式不呼叫 FindConflicts，呼叫端要先查。
func Write(ctx context.Context, sysroot string, s recipe.AptSource, p platform.Platform, client *http.Client) error {
	content, err := Render(s, p)
	if err != nil {
		return err
	}
	r, err := renderFields(s, p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.KeyURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("aptsource: 下載金鑰失敗：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("aptsource: 下載金鑰失敗：%s 回 %s", s.KeyURL, resp.Status)
	}
	key, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("aptsource: 讀取金鑰失敗：%w", err)
	}
	if err := writeAtomic(filepath.Join(sysroot, r.signedBy), key); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(sysroot, sourcesDir, s.File), content)
}
