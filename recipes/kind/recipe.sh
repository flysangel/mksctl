#!/bin/bash
# recipe.sh — kind 配方：取 kind 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       舊 provision 的 kind 會先裝 podman，再建 podman network kind（ubuntu 只在 systemd 下建）。
#       本配方只裝執行檔；podman network kind 留到 Task B7 的 podman 配方就緒後補

kind_install() {
  local bin

  bin="$(fetch kind)" || return 1
  install_bin "${bin}" kind
}

kind_verify() {
  command -v kind >/dev/null || {
    echo "Error: 找不到 kind" >&2
    return 1
  }
  kind version >/dev/null || {
    echo "Error: kind version 失敗" >&2
    return 1
  }
}
