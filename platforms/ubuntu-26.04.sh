#!/bin/bash
# ubuntu-26.04.sh — 26.04 的時間同步 helper：預設服務是 chrony
# 註記：覆寫 ubuntu.sh 的 time_sync_enable；time_sync_active 沿用 ubuntu.sh
#       沿用舊 ntp 行為：已裝哪個就啟用哪個，不換掉使用者已有的；兩個都沒裝才補裝 chrony

time_sync_enable() {
  local svc
  if _pkg_installed chrony; then
    svc=chrony
  elif _pkg_installed systemd-timesyncd; then
    svc=systemd-timesyncd
  else
    pkg_install chrony || return 1
    svc=chrony
  fi
  echo "Info: 啟用時間同步服務 ${svc}" >&2
  svc_enable "${svc}" || return 1
}
