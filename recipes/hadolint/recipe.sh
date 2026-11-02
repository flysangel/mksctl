#!/bin/bash
# recipe.sh — hadolint 配方：取 hadolint 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       上游只出 x86_64 的檔名（hadolint-linux-x86_64），所以 artifact URL 寫死架構；本專案只出 amd64

hadolint_install() {
  local bin

  bin="$(fetch hadolint)" || return 1
  install_bin "${bin}" hadolint
}

hadolint_verify() {
  command -v hadolint >/dev/null || {
    echo "Error: 找不到 hadolint" >&2
    return 1
  }
  hadolint --version >/dev/null || {
    echo "Error: hadolint --version 失敗" >&2
    return 1
  }
}
