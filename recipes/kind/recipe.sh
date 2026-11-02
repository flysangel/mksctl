#!/bin/bash
# recipe.sh — kind 配方：取 kind 單一執行檔，裝進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       舊 provision 的 kind 會先裝 podman，再建 podman network kind（ubuntu 只在 systemd 下建）。
#       needs podman：kind 在這套環境用 podman 當 provider，沒有 podman 就跑不起來，所以由 podman 配方先裝。
#       podman network kind 只在有 init 時建：容器（映像建置、測試）裡建 network 沒有意義，也常因沒有 netavark 服務環境而失敗

kind_install() {
  local bin

  bin="$(fetch kind)" || return 1
  install_bin "${bin}" kind || return 1

  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    podman network exists kind || podman network create kind || return 1
  fi
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
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    podman network exists kind || {
      echo "Error: 沒有 podman network kind" >&2
      return 1
    }
  fi
}
