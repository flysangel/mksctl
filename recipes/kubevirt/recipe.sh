#!/bin/bash
# recipe.sh — kubevirt 配方：取 virtctl 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       配方名叫 kubevirt（沿用舊 provision），實際裝的是 KubeVirt 的命令列工具 virtctl

kubevirt_install() {
  local bin

  bin="$(fetch virtctl)" || return 1
  install_bin "${bin}" virtctl
}

kubevirt_verify() {
  command -v virtctl >/dev/null || {
    echo "Error: 找不到 virtctl" >&2
    return 1
  }
  virtctl version --client >/dev/null || {
    echo "Error: virtctl version --client 失敗" >&2
    return 1
  }
}
