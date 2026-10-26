#!/bin/bash
# recipe.sh — netplan 配方：DHCP 的 netplan 設定檔補 dhcp-identifier: mac，並附一份靜態 IP 範本註解
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       只有 ubuntu 有 netplan：requires: apt 讓 alpine 自動略過
#       requires: kernel：netplan apply 會重載 host 的網路，容器不跑
#       只有真的改了檔才 netplan apply，避免無謂重載網路

_NETPLAN_MARKER="# --- provision static-ip template (edit & uncomment to switch) ---"

# _netplan_is_dhcp <檔> — 檔內有未註解的 dhcp4 或 dhcp6 設為 true 或 yes 時回 0
_netplan_is_dhcp() {
  grep -Eq '^[[:space:]]*dhcp[46]:[[:space:]]*(true|yes)[[:space:]]*$' "$1"
}

netplan_install() {
  local yaml changed=0 yaml_changed
  local yamls=()

  # /etc/netplan 沒有 yaml 時，glob 展開成空，不是字面字串
  shopt -s nullglob
  yamls=(/etc/netplan/*.yaml)
  shopt -u nullglob
  if [[ "${#yamls[@]}" -eq 0 ]]; then
    echo "Info: /etc/netplan 底下沒有 *.yaml，不需要處理" >&2
    return 0
  fi

  for yaml in "${yamls[@]}"; do
    # 只處理仍在用 DHCP 的檔
    if ! _netplan_is_dhcp "${yaml}"; then
      echo "Info: ${yaml} 不是 DHCP，略過" >&2
      continue
    fi
    # 冪等備份：.bk 已存在就不覆蓋首次備份
    if [[ ! -f "${yaml}.bk" ]]; then
      cp "${yaml}" "${yaml}.bk" || return 1
    fi
    yaml_changed=0

    # 檔層級冪等：檔內已有任何 dhcp-identifier 就整份略過插入
    if grep -Eq '^[[:space:]]*dhcp-identifier:' "${yaml}"; then
      echo "Info: ${yaml} 已有 dhcp-identifier，略過插入" >&2
    else
      # 在每個 dhcp4: true|yes 行後插入 dhcp-identifier: mac，沿用原縮排
      sed -i -E 's/^([[:space:]]+)dhcp4:[[:space:]]*(true|yes)[[:space:]]*$/&\n\1dhcp-identifier: mac/' "${yaml}" || return 1
      echo "Info: ${yaml} 已插入 dhcp-identifier: mac" >&2
      yaml_changed=1
    fi

    # 附加靜態 IP 範本註解；以標記判斷，重跑不會重複附加
    if grep -qF "${_NETPLAN_MARKER}" "${yaml}"; then
      echo "Info: ${yaml} 已有靜態 IP 範本，略過附加" >&2
    else
      cat >>"${yaml}" <<TEMPLATE || return 1
${_NETPLAN_MARKER}
# To switch from DHCP to a static IP, follow these three steps:
#   1. In this file, change  dhcp4: true  (or dhcp6: true)  to  dhcp4: false / dhcp6: false,
#      and remove the  dhcp-identifier: mac  line (static IPs do not need it).
#   2. Uncomment the block below, replace all placeholder values, and adjust indentation
#      so the keys align with the rest of your existing network configuration.
#   3. Run:  sudo netplan apply
#
# network:
#   version: 2
#   renderer: networkd
#   ethernets:
#     <INTERFACE_NAME>:        # e.g. eth0, ens3, enp0s3 — check with: ip link
#       dhcp4: false
#       dhcp6: false
#       addresses:
#         - <IP_ADDRESS>/<PREFIX_LEN>  # e.g. 192.168.1.100/24
#       routes:
#         - to: default
#           via: <GATEWAY>           # e.g. 192.168.1.1
#       nameservers:
#         addresses:
#           - <DNS1>                 # e.g. 8.8.8.8
#           - <DNS2>                 # e.g. 8.8.4.4
TEMPLATE
      echo "Info: ${yaml} 已附加靜態 IP 範本" >&2
      yaml_changed=1
    fi

    if [[ "${yaml_changed}" -eq 1 ]]; then
      changed=1
    fi
  done

  if [[ "${changed}" -eq 1 ]]; then
    echo "Info: 套用 netplan" >&2
    netplan apply || return 1
  else
    echo "Info: 沒有改動，不套用 netplan" >&2
  fi
}

netplan_verify() {
  local yaml

  command -v netplan >/dev/null || {
    echo "Error: 找不到 netplan" >&2
    return 1
  }
  # 仍用 DHCPv4 的檔必須帶 dhcp-identifier
  for yaml in /etc/netplan/*.yaml; do
    [[ -f "${yaml}" ]] || continue
    if grep -Eq '^[[:space:]]*dhcp4:[[:space:]]*(true|yes)[[:space:]]*$' "${yaml}" &&
      ! grep -Eq '^[[:space:]]*dhcp-identifier:' "${yaml}"; then
      echo "Error: ${yaml} 用 DHCPv4 卻沒有 dhcp-identifier" >&2
      return 1
    fi
  done
  # 這支改網路設定；套用後機器必須還連得出去
  ip route | grep -q default || {
    echo "Error: 沒有預設路由" >&2
    return 1
  }
}
