package aptsource

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/flysangel/mksctl/internal/platform"
)

const (
	// mirrorFile 是 ubuntu 24.04 起發行版自己的 deb822 來源檔。mksctl 寫過一次後就由 mksctl 管：
	// 整檔取代，不嘗試合併使用者的改動（原檔留在 .bk）。
	mirrorFile = "ubuntu.sources"
	// archiveKeyring 由 ubuntu-keyring 套件提供，雲端映像預設的 ubuntu.sources 也指向它。
	archiveKeyring = "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	confDir        = "etc/apt/apt.conf.d"
	// legacyNote 取代清空後的 sources.list，告訴使用者來源搬到哪裡、原內容在哪裡。
	legacyNote = "# mksctl 已把發行版來源移到 /etc/apt/sources.list.d/ubuntu.sources，原內容在 /etc/apt/sources.list.bk\n"
)

// RenderMirror 回傳鏡像的 deb822 內容。全部 suite 放在同一段，欄位順序固定，重跑才比對得出「內容相同」。
func RenderMirror(m platform.AptMirror, p platform.Platform) ([]byte, error) {
	uris, err := expand("apt_mirror.uris", m.URIs, p)
	if err != nil {
		return nil, err
	}
	suites := make([]string, 0, len(m.Suites))
	for _, s := range m.Suites {
		out, err := expand("apt_mirror.suites", s, p)
		if err != nil {
			return nil, err
		}
		suites = append(suites, out)
	}
	comps, err := expand("apt_mirror.components", m.Components, p)
	if err != nil {
		return nil, err
	}
	out := fmt.Sprintf("Types: deb\nURIs: %s\nSuites: %s\nComponents: %s\nSigned-By: %s\n",
		uris, strings.Join(suites, " "), comps, archiveKeyring)
	return []byte(out), nil
}

// WriteMirror 把發行版來源改指向鏡像。內容相同時什麼都不寫、回 changed=false，重跑才不會每次都動檔。
//
// 備份只做一次：已有 .bk 就不再備份，否則第二次改鏡像設定時，.bk 會被 mksctl 自己寫的內容蓋掉，
// 使用者就找不回原本的來源。
//
// /etc/apt/sources.list 還有一行式的 deb 行時（舊 provision 或舊版 ubuntu 的寫法），
// 備份後清空：同一個套件庫在兩處出現，apt 會報重複，Signed-By 不同時更會拒讀全部來源。
func WriteMirror(sysroot string, m platform.AptMirror, p platform.Platform) (changed bool, err error) {
	content, err := RenderMirror(m, p)
	if err != nil {
		return false, err
	}
	target := filepath.Join(sysroot, sourcesDir, mirrorFile)
	cur, err := os.ReadFile(target)
	switch {
	case err == nil && bytes.Equal(cur, content):
	case err == nil:
		if err := backupOnce(target, cur); err != nil {
			return false, err
		}
		fallthrough
	case os.IsNotExist(err):
		if err := writeAtomic(target, content); err != nil {
			return false, err
		}
		changed = true
	default:
		return false, err
	}

	legacy := filepath.Join(sysroot, legacyList)
	old, err := os.ReadFile(legacy)
	if os.IsNotExist(err) {
		return changed, nil
	}
	if err != nil {
		return false, err
	}
	if len(parseList(string(old))) == 0 {
		return changed, nil
	}
	if err := backupOnce(legacy, old); err != nil {
		return false, err
	}
	if err := writeAtomic(legacy, []byte(legacyNote)); err != nil {
		return false, err
	}
	return true, nil
}

// backupOnce 把 data 存成 path.bk；.bk 已存在就不動它。
func backupOnce(path string, data []byte) error {
	bk := path + ".bk"
	if _, err := os.Stat(bk); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeAtomic(bk, data)
}

// WriteConf 把平台層的 apt_conf 寫進 /etc/apt/apt.conf.d/，內容相同的檔不重寫。
// 檔名不能帶路徑：平台檔寫錯時寧可失敗，也不寫到 apt.conf.d 以外。
func WriteConf(sysroot string, conf map[string]string) (changed bool, err error) {
	names := make([]string, 0, len(conf))
	for n := range conf {
		if n == "" || n == "." || n == ".." || strings.ContainsAny(n, `/\`) {
			return false, fmt.Errorf("aptsource: apt_conf 的檔名 %q 不合法", n)
		}
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		path := filepath.Join(sysroot, confDir, n)
		if cur, err := os.ReadFile(path); err == nil && string(cur) == conf[n] {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return false, err
		}
		if err := writeAtomic(path, []byte(conf[n])); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
