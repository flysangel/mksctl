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
  # 名稱全都對到 null（本平台沒有這些套件）時沒有東西要裝；空清單交給套件管理器會出錯或白跑一次
  [[ "${#pkgs[@]}" -gt 0 ]] || return 0
  apk add --no-cache "${pkgs[@]}" || return 1
}

# _svc_enable <服務> — openrc 開機啟用
_svc_enable() {
  # 沒有 init 工具（例如容器）時略過，不算失敗
  command -v rc-update >/dev/null || {
    echo "Info: 沒有 rc-update，略過啟用 $1" >&2
    return 0
  }
  rc-update add "$1" default || return 1
}

# _svc_start <服務> — openrc 立即啟動
_svc_start() {
  command -v rc-service >/dev/null || {
    echo "Info: 沒有 rc-service，略過啟動 $1" >&2
    return 0
  }
  rc-service "$1" start || return 1
}

# time_sync_enable — alpine 固定用 busybox 的 ntpd（OpenRC 服務）
time_sync_enable() {
  echo "Info: 啟用時間同步服務 ntpd" >&2
  svc_enable ntpd || return 1
}

# time_sync_active — ntpd 在執行時回 0
time_sync_active() {
  rc-service ntpd status >/dev/null 2>&1
}
