#!/bin/bash
# apt.sh — ubuntu 的套件與服務 helper 函式庫
# 驅動：mksctl 的 runner 在 ubuntu 上把本檔內嵌進組合腳本
# 註記：函式庫不設 set 旗標，set -e 會污染呼叫端的 shell；失敗由各函式 return 1

# pkg_install <邏輯名稱>... — 轉成 apt 套件名後安裝
pkg_install() {
  local names pkgs
  [[ "$#" -gt 0 ]] || {
    echo "Error: usage: pkg_install <邏輯名稱>..." >&2
    return 1
  }
  names="$("${MKSCTL_BIN}" pkgname "$@")" || return 1
  # 套件名以空白分隔，要拆成多個參數
  read -r -a pkgs <<<"${names}"
  DEBIAN_FRONTEND=noninteractive apt-get install -y -o DPkg::Lock::Timeout=300 --no-install-recommends "${pkgs[@]}"
}

# _svc_enable <服務> — systemd 開機啟用
_svc_enable() {
  systemctl enable "$1"
}

# _svc_start <服務> — systemd 立即啟動
_svc_start() {
  systemctl start "$1"
}
