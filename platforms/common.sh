#!/bin/bash
# common.sh — 所有平台共用的 helper 函式庫
# 驅動：mksctl 的 runner 把各層內嵌進組合腳本時，最先載入本檔
# 註記：函式庫不設 set 旗標，set -e 會污染呼叫端的 shell；失敗由各函式 return 1
#       _svc_enable 與 _svc_start 由 apk.sh、apt.sh 定義，本檔只負責呼叫

# install_bin <來源> [<目標名稱>] — 以 root:root 0755 裝進 bin 目錄
# 目標名稱省略時沿用來源的檔名。
# MKSCTL_BIN_DIR 預設 /usr/local/bin；測試時改指向暫存目錄，才不必 root。
install_bin() {
  local src="${1:-}" name dest_dir
  [[ -n "${src}" ]] || {
    echo "Error: usage: install_bin <來源> [<目標名稱>]" >&2
    return 1
  }
  [[ -f "${src}" ]] || {
    echo "Error: 找不到 ${src}" >&2
    return 1
  }
  name="${2:-$(basename "${src}")}"
  dest_dir="${MKSCTL_BIN_DIR:-/usr/local/bin}"
  install -o root -g root -m 0755 "${src}" "${dest_dir}/${name}"
}

# svc_enable <服務> — 開機啟用；init 系統正在跑時再順手啟動
# 容器沒有 init，啟動會失敗，所以只在 MKSCTL_INIT_RUNNING=1 時才啟動。
svc_enable() {
  local svc="${1:-}"
  [[ -n "${svc}" ]] || {
    echo "Error: usage: svc_enable <服務>" >&2
    return 1
  }
  _svc_enable "${svc}" || return 1
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    _svc_start "${svc}" || return 1
  fi
}

# fetch <artifact ID> — 下載並驗證，印出已驗證檔案的路徑
fetch() {
  local id="${1:-}"
  [[ -n "${id}" ]] || {
    echo "Error: usage: fetch <artifact ID>" >&2
    return 1
  }
  "${MKSCTL_BIN}" fetch "${id}"
}
