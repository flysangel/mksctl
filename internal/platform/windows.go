package platform

import "strings"

// windowsBuild 是 build 號到產品層的對照。同一個 build 可同時是 Client 與 Server（例如 26100）。
type windowsBuild struct {
	installationType string // Client 或 Server
	build            string
	layer            string
}

// windowsBuilds 只列支援的 build，不在表內的一律走「沒有精確層」，由 preflight 擋下。
// 順序有意義：WindowsLayerFor 取同一層的第一筆。
var windowsBuilds = []windowsBuild{
	{"Client", "19045", "windows-10"},
	{"Client", "26100", "windows-11"},
	{"Client", "26200", "windows-11"},
	{"Server", "26100", "windows-server-2025"},
}

// WindowsPlatform 由登錄的 InstallationType 與 CurrentBuild 組出平台。
// 對照表命中時 Layers[0] 是產品層；沒命中時 Layers[0] 是帶 build 的層，
// 沒有定義檔，Defs.Apply 會得到 HasExactLayer == false。
// Server Core 視為 Server；其他未知的 InstallationType 用 windows-unknown-<build>。
func WindowsPlatform(installationType, currentBuild string) Platform {
	family := ""
	switch installationType {
	case "Client":
		family = "client"
	case "Server", "Server Core":
		family = "server"
		installationType = "Server"
	}

	p := Platform{ID: "windows", Version: currentBuild, InitRunning: true}
	if family == "" {
		p.Layers = []string{"windows-unknown-" + currentBuild, "windows"}
		return p
	}
	layer := "windows-" + family + "-" + currentBuild
	for _, b := range windowsBuilds {
		if b.installationType == installationType && b.build == currentBuild {
			layer = b.layer
			break
		}
	}
	p.Layers = []string{layer, "windows-" + family, "windows"}
	return p
}

// WindowsLayerFor 由產品層名反查對照表的第一個 build，給 list --platform 用。
func WindowsLayerFor(layer string) (installationType, build string, ok bool) {
	if !strings.HasPrefix(layer, "windows-") {
		return "", "", false
	}
	for _, b := range windowsBuilds {
		if b.layer == layer {
			return b.installationType, b.build, true
		}
	}
	return "", "", false
}
