#!/bin/bash
# recipe.sh — helm 配方：從官方下載點取 tarball，裝 helm 進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟

# 給 trap 用的暫存目錄
HELM_TMP=""

helm_install() {
  local tarball

  tarball="$(fetch helm)" || return 1
  HELM_TMP="$(mktemp -d)" || return 1
  trap 'rm -rf "${HELM_TMP}"' EXIT
  tar -xzf "${tarball}" -C "${HELM_TMP}" || return 1
  # tarball 內的目錄名是 linux-<arch>，用 glob 避開架構對照
  install_bin "${HELM_TMP}"/linux-*/helm helm
}

helm_verify() {
  command -v helm >/dev/null || {
    echo "Error: 找不到 helm" >&2
    return 1
  }
  helm version --short >/dev/null || {
    echo "Error: helm version --short 失敗" >&2
    return 1
  }
}
