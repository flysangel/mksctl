#!/bin/bash
# recipe.sh — kubectl 配方：取 kubectl 單一執行檔，裝進 bin 目錄，並寫 bash 補全
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       補全寫 /etc/bash_completion.d/（bash 專用），不可放 /etc/profile.d/：
#       後者連 dash、busybox ash 都會 source，而 source <(...) 是 bash 語法，
#       dash 解析失敗會中止整個 /etc/profile，曾害 xrdp 桌面啟動失敗

kubectl_install() {
  local bin

  pkg_install bash-completion || return 1
  bin="$(fetch kubectl)" || return 1
  install_bin "${bin}" kubectl || return 1

  mkdir -p /etc/bash_completion.d || return 1
  kubectl completion bash >/etc/bash_completion.d/kubectl || return 1
  # 清掉更舊版本留下的 profile.d 補全檔
  rm -f /etc/profile.d/kubectl-env.sh /etc/profile.d/kubectl-env.sh.bk || return 1

  # dash-safety：/etc/profile 必須能被 dash（/bin/sh）source
  sh -c '. /etc/profile' >/dev/null 2>&1 || {
    echo "Error: /etc/profile 無法被 sh source（bash 專用的檔是否誤入 /etc/profile.d/？）" >&2
    return 1
  }
}

kubectl_verify() {
  command -v kubectl >/dev/null || {
    echo "Error: 找不到 kubectl" >&2
    return 1
  }
  kubectl version --client >/dev/null || {
    echo "Error: kubectl version --client 失敗" >&2
    return 1
  }
  grep -q kubectl /etc/bash_completion.d/kubectl 2>/dev/null || {
    echo "Error: /etc/bash_completion.d/kubectl 不存在或沒有 kubectl 補全" >&2
    return 1
  }
}
