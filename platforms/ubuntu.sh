#!/bin/bash
# ubuntu.sh — ubuntu 各版共用的時間同步 helper
# 驅動：mksctl 的 runner 在 ubuntu 上把本檔內嵌進組合腳本
# 註記：函式庫不設 set 旗標；失敗由各函式 return 1
#       24.04 預設時間同步服務是 systemd-timesyncd，26.04 是 chrony，兩者互斥

# _pkg_installed <deb 套件名> — 套件已安裝時回 0
_pkg_installed() {
  dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q '^install ok installed$'
}

# time_sync_enable — 啟用機器上已裝的時間同步服務；兩個都沒裝才補裝 chrony
# 這條規則來自舊 ubuntu ntp 腳本（「啟用機器上已裝的那個，兩者都沒裝才補裝 chrony」）。
# 24.04 與 26.04 共用：26.04 映像預設已裝 chrony、24.04 預設已裝 systemd-timesyncd，兩者互斥，
# 所以各版自然啟用自己的預設服務，不必分版本層覆寫。
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

# time_sync_active — 有時間同步服務在執行時回 0
time_sync_active() {
  systemctl is-active --quiet chrony || systemctl is-active --quiet systemd-timesyncd
}
