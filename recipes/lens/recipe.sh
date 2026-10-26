#!/bin/bash
# recipe.sh — lens 配方：用 Lens 官方 apt 來源裝 Lens Desktop
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt 的安裝是冪等的
#       apt 來源（deb822）與金鑰由 mksctl 依 recipe.yaml 的 apt_sources 處理
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過

lens_install() {
  pkg_install lens || return 1
}

lens_verify() {
  # Lens 是 GUI 應用，執行檔名不保證叫 lens，所以以套件狀態為準
  dpkg -s lens >/dev/null 2>&1 || {
    echo "Error: lens 套件未安裝" >&2
    return 1
  }
  # 驗來源沒有把 apt 弄壞：同一 URI 兩個 Signed-By 會讓 apt 拒讀全部來源，
  # 安裝當下不會顯現，裝完再跑一次 apt 才抓得到
  apt-get update -qq >/dev/null 2>&1 || {
    echo "Error: 裝完 lens 後 apt-get update 失敗（來源清單有衝突）" >&2
    return 1
  }
}
