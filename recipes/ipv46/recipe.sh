#!/bin/bash
# recipe.sh — ipv46 配方：開 IPv4 轉送、關 ICMP redirect、關 IPv6
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       requires: kernel：sysctl 動的是 host kernel 的網路參數，容器不跑
#       兩個發行版的設定完全相同，所以沒有發行版專屬段落

ipv46_install() {
  mkdir -p /etc/sysctl.d || return 1
  # 容器與叢集網路要 IPv4 轉送；關 ICMP redirect 是基本加固；這套環境不用 IPv6，
  # 三處一起關才不會被 default 補回來
  cat >/etc/sysctl.d/ipv46.conf <<'CONF' || return 1
net.ipv4.ip_forward = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv6.conf.all.disable_ipv6 = 1
net.ipv6.conf.default.disable_ipv6 = 1
net.ipv6.conf.lo.disable_ipv6 = 1
CONF
  # 立即套用，免等重新開機；唯讀的 /proc/sys 套用失敗不擋安裝，verify 會查實際值
  sysctl -p /etc/sysctl.d/ipv46.conf || echo "Info: sysctl 即時套用失敗，重開機後生效" >&2
}

ipv46_verify() {
  grep -q '^net.ipv4.ip_forward = 1' /etc/sysctl.d/ipv46.conf || {
    echo "Error: /etc/sysctl.d/ipv46.conf 沒有 ip_forward 設定" >&2
    return 1
  }
  # 設定檔不算數，執行中的 kernel 值對才算生效
  [[ "$(sysctl -n net.ipv4.ip_forward)" == 1 ]] || {
    echo "Error: ip_forward 沒有生效" >&2
    return 1
  }
  [[ "$(sysctl -n net.ipv6.conf.all.disable_ipv6)" == 1 ]] || {
    echo "Error: IPv6 沒有停用" >&2
    return 1
  }
}
