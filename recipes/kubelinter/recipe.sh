#!/bin/bash
# recipe.sh — kubelinter 配方：取 kube-linter 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟

kubelinter_install() {
  local bin

  bin="$(fetch kube-linter)" || return 1
  install_bin "${bin}" kube-linter
}

kubelinter_verify() {
  command -v kube-linter >/dev/null || {
    echo "Error: 找不到 kube-linter" >&2
    return 1
  }
  kube-linter version >/dev/null || {
    echo "Error: kube-linter version 失敗" >&2
    return 1
  }
}
