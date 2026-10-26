#!/bin/bash
# recipe.sh — vcluster 配方：取 vcluster 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟

vcluster_install() {
  local bin

  bin="$(fetch vcluster)" || return 1
  install_bin "${bin}" vcluster
}

vcluster_verify() {
  command -v vcluster >/dev/null || {
    echo "Error: 找不到 vcluster" >&2
    return 1
  }
  vcluster --version >/dev/null || {
    echo "Error: vcluster --version 失敗" >&2
    return 1
  }
}
