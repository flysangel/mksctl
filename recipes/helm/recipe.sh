#!/bin/bash
# recipe.sh — helm 配方：從官方下載點取 tarball，裝 helm 進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟

# _helm_extract_install <tarball> <暫存目錄> — 解壓並安裝，清理由呼叫端負責
_helm_extract_install() {
  local tarball="$1" tmp="$2"
  local found

  tar -xzf "${tarball}" -C "${tmp}" || return 1
  # tarball 內的目錄名是 linux-<arch>，用 glob 避開架構對照
  found=("${tmp}"/linux-*/helm)
  [[ "${#found[@]}" -eq 1 && -f "${found[0]}" ]] || {
    echo "Error: tarball 內找不到唯一的 linux-*/helm" >&2
    return 1
  }
  install_bin "${found[0]}" helm || return 1
}

helm_install() {
  local tarball tmp rc

  tarball="$(fetch helm)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  _helm_extract_install "${tarball}" "${tmp}" || rc=$?
  rm -rf "${tmp}"
  return "${rc}"
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
