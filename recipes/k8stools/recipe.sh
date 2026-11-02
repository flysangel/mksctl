#!/bin/bash
# recipe.sh — k8stools 聚合器：實際工作都在 recipe.yaml 的 steps 配方裡
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       krew 裝到 ~/.krew（per-user），靠 /etc/profile.d/krew-env.sh 接 PATH，所以驗證要先 source

k8stools_install() {
  return 0
}

k8stools_verify() {
  local tool
  for tool in kind kubectl kustomize kube-linter helm hadolint; do
    command -v "${tool}" >/dev/null || {
      echo "Error: k8stools 應已安裝 ${tool}" >&2
      return 1
    }
  done
  export HOME="${HOME:-/root}"
  # shellcheck source=/dev/null
  . /etc/profile.d/krew-env.sh || return 1
  command -v kubectl-krew >/dev/null || {
    echo "Error: k8stools 應已安裝 kubectl-krew" >&2
    return 1
  }
}
