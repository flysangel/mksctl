#!/bin/bash
# recipe.sh — kustomize 配方：從 GitHub release 取 tarball，裝 kustomize 進 bin 目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       舊 alpine 版走 apk（edge community），這裡兩個發行版統一用上游 tarball，
#       因為 apk 的 kustomize 不在穩定分支

kustomize_install() {
  local tarball tmp rc

  tarball="$(fetch kustomize)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  {
    tar -xzf "${tarball}" -C "${tmp}" kustomize &&
      install_bin "${tmp}/kustomize" kustomize
  } || rc=$?
  rm -rf "${tmp}"
  return "${rc}"
}

kustomize_verify() {
  command -v kustomize >/dev/null || {
    echo "Error: 找不到 kustomize" >&2
    return 1
  }
  kustomize version >/dev/null || {
    echo "Error: kustomize version 失敗" >&2
    return 1
  }
}
