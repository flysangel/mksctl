// Package platform 判斷目標機器的發行版、層順序與執行環境。
package platform

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Platform 描述目標機器。Layers 由具體到通用排列，前面的層優先。
type Platform struct {
	ID       string
	Version  string
	Codename string
	Layers   []string
	// HasExactLayer 表示 Layers[0]（含版本的層）有對應的定義檔。
	HasExactLayer bool
	InitRunning   bool
	InContainer   bool
	Provides      map[string]bool
}

// Parse 解析 /etc/os-release 的內容。
// 只認 ubuntu 與 alpine，其他 ID 直接回錯誤，避免在不支援的系統上亂裝。
func Parse(osRelease []byte) (Platform, error) {
	kv := parseKeyValues(osRelease)
	p := Platform{ID: kv["ID"]}

	switch p.ID {
	case "ubuntu":
		p.Version = kv["VERSION_ID"]
		p.Codename = kv["VERSION_CODENAME"]
		p.Layers = []string{"ubuntu-" + p.Version, "ubuntu", "apt", "common"}
	case "alpine":
		// alpine 的 VERSION_ID 是 主.次.修，平台層只看 主.次。
		p.Version = majorMinor(kv["VERSION_ID"])
		p.Layers = []string{"alpine-" + p.Version, "apk", "common"}
	default:
		return Platform{}, fmt.Errorf("不支援的發行版：%q", p.ID)
	}
	return p, nil
}

func majorMinor(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

// parseKeyValues 解析 KEY=value 行，值可加單引號或雙引號。
func parseKeyValues(b []byte) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[strings.TrimSpace(k)] = v
	}
	return out
}

// DetectRuntime 判斷 init 是否在跑、是否身處容器。
// sysroot 讓測試能指向假的根目錄；正式執行傳 "/"。
func DetectRuntime(sysroot string) (initRunning, inContainer bool) {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(sysroot, rel))
		return err == nil
	}

	initRunning = exists("run/systemd/system") || exists("run/openrc/softlevel")

	if exists(".dockerenv") || exists("run/.containerenv") {
		return initRunning, true
	}
	// 容器內 PID 1 的環境變數以 NUL 分隔。讀不到（權限或不存在）就當不在容器。
	if env, err := os.ReadFile(filepath.Join(sysroot, "proc/1/environ")); err == nil {
		for _, entry := range bytes.Split(env, []byte{0}) {
			if bytes.HasPrefix(entry, []byte("container=")) {
				return initRunning, true
			}
		}
	}
	return initRunning, false
}
