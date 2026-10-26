#!/bin/bash
# apk.sh — alpine 的套件與服務 helper 函式庫
# 驅動：mksctl 的 runner 在 alpine 上把本檔內嵌進組合腳本
# 註記：函式庫不設 set 旗標，set -e 會污染呼叫端的 shell；失敗由各函式 return 1

# pkg_install <邏輯名稱>... — 轉成 apk 套件名後安裝
pkg_install() {
  local names pkgs
  [[ "$#" -gt 0 ]] || {
    echo "Error: usage: pkg_install <邏輯名稱>..." >&2
    return 1
  }
  names="$("${MKSCTL_BIN}" pkgname "$@")" || return 1
  # 套件名以空白分隔，要拆成多個參數
  read -r -a pkgs <<<"${names}" || return 1
  apk add --no-cache "${pkgs[@]}" || return 1
}

# _svc_enable <服務> — openrc 開機啟用
_svc_enable() {
  rc-update add "$1" default || return 1
}

# _svc_start <服務> — openrc 立即啟動
_svc_start() {
  rc-service "$1" start || return 1
}
