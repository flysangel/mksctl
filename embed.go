// Package mksctl 把 recipe、平台定義與信任清單嵌進執行檔。
package mksctl

import "embed"

// FS 是嵌入的 recipes、platforms、pkgmap.yaml 與 trust。
// all: 前綴讓 .keep 這類點開頭的檔也算數，空目錄才編得過。
//
//go:embed all:recipes all:platforms pkgmap.yaml trust
var FS embed.FS
