#!/bin/bash
# recipe.sh — disableSwap 配方：關閉所有 swap，並讓關閉狀態在重開機後仍生效
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       requires: kernel：swap 是 host kernel 的資源，容器內的 swapoff 會關掉 host 的 swap，所以容器不跑
#       兩個發行版收斂成同一個行為：舊 ubuntu 版多一行 rm -rf /swap*，舊 alpine 版沒有。
#       這裡選擇兩邊都刪實體 swap 檔，但只刪「實際當 swap 用的檔」（/proc/swaps 與 fstab 列出的一般檔案），
#       不用 /swap* 萬用字元：ubuntu 的預設 swap 檔是 /swap.img，萬用字元也會誤刪其他以 swap 開頭的根目錄檔案
#       讀 /proc/swaps 而不用 swapon --show：後者是 util-linux 的選項，alpine 的 busybox swapon 沒有

# _disableSwap_files — 印出目前當 swap 的一般檔案，與 fstab 裡尚未註解的 swap 項目指向的一般檔案
_disableSwap_files() {
  local path
  {
    awk 'NR > 1 && $2 == "file" { print $1 }' /proc/swaps
    if [[ -f /etc/fstab ]]; then
      awk '$1 !~ /^#/ && $3 == "swap" { print $1 }' /etc/fstab
    fi
  } | sort -u | while IFS= read -r path; do
    if [[ -f "${path}" && ! -L "${path}" ]]; then
      echo "${path}"
    fi
  done
}

disableSwap_install() {
  local files path

  # 先在 swapoff 與註解 fstab 之前取清單，之後兩個來源都空了
  files="$(_disableSwap_files)" || return 1

  # 即時關閉所有 swap；沒有啟用的 swap 時 swapoff 會回非零，故容錯
  swapoff -a || true

  # 把 fstab 內尚未註解的 swap 行前面加 #，讓關閉狀態在重開機後仍生效
  if [[ -f /etc/fstab ]]; then
    sed -i -e '/^[[:space:]]*[^#].*swap/s/^/#/' /etc/fstab || return 1
  fi

  while IFS= read -r path; do
    if [[ -n "${path}" ]]; then
      rm -f -- "${path}" || return 1
    fi
  done <<<"${files}"

  # 沒有 fstab（精簡環境）就沒有殘留項目可查
  if [[ -f /etc/fstab ]] && grep -qE '^[^#]*[[:space:]]swap[[:space:]]' /etc/fstab; then
    echo "Error: /etc/fstab 仍有啟用中的 swap 項目" >&2
    return 1
  fi
}

disableSwap_verify() {
  # swap 不是 namespace 化的資源，只有真 VM 驗得到「真的關掉了」
  if awk 'NR > 1' /proc/swaps | grep -q .; then
    echo "Error: swap 仍在啟用" >&2
    return 1
  fi
  if [[ -f /etc/fstab ]] && grep -qE '^[^#]*[[:space:]]swap[[:space:]]' /etc/fstab; then
    echo "Error: /etc/fstab 仍有啟用中的 swap 項目" >&2
    return 1
  fi
}
