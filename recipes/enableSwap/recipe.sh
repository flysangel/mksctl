#!/bin/bash
# recipe.sh — enableSwap 配方：建 /swapfile（記憶體 GB 數加 2 GB）、啟用，並寫進 fstab 讓重開機後仍生效
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       requires: kernel：swapon 動的是 host kernel，容器不跑
#       記憶體大小讀 /proc/meminfo，不用 free -g：兩個發行版的 free 輸出格式不同（alpine 是 busybox）
#       判斷是否已啟用讀 /proc/swaps，不用 swapon --show：alpine 的 busybox swapon 沒有這個選項
#       舊版的 fstab 檢查用 grep '/swapfile none swap'，會被 disableSwap 註解掉的 #/swapfile 行誤判成已存在，
#       先 disableSwap 再 enableSwap 時永遠不會寫回 fstab。這裡錨定行首，只認未註解的行

enableSwap_install() {
  local mem_kb size_gb

  mem_kb="$(awk '/^MemTotal:/ { print $2 }' /proc/meminfo)" || return 1
  [[ -n "${mem_kb}" ]] || {
    echo "Error: 讀不到 /proc/meminfo 的 MemTotal" >&2
    return 1
  }
  size_gb=$((mem_kb / 1024 / 1024 + 2))

  pkg_install util-linux || return 1

  if [[ ! -f /swapfile ]]; then
    fallocate -l "${size_gb}G" /swapfile || return 1
  fi
  chmod 600 /swapfile || return 1

  # 沒啟用就一律 mkswap 再 swapon：fallocate 之後、mkswap 之前被中斷時，檔案已存在卻沒有 swap 簽章，
  # 只靠「檔案不存在才 mkswap」，重跑時 swapon 會失敗。對還沒啟用的檔重做 mkswap 是安全的
  if awk 'NR > 1 { print $1 }' /proc/swaps | grep -qx '/swapfile'; then
    echo "Info: /swapfile 已啟用，略過 swapon" >&2
  else
    mkswap /swapfile || return 1
    swapon /swapfile || return 1
  fi

  # 容器映像常沒有 /etc/fstab；有才先冪等備份
  if [[ -f /etc/fstab && ! -f /etc/fstab.bk ]]; then
    cp /etc/fstab /etc/fstab.bk || return 1
  fi
  if ! grep -q '^/swapfile none swap' /etc/fstab 2>/dev/null; then
    echo '/swapfile none swap sw 0 0' >>/etc/fstab || return 1
  fi
  echo "Info: swap 檔 ${size_gb} GB 已啟用，並寫進 /etc/fstab" >&2
}

enableSwap_verify() {
  if ! awk 'NR > 1' /proc/swaps | grep -q .; then
    echo "Error: swap 未啟用" >&2
    return 1
  fi
  [[ -f /swapfile ]] || {
    echo "Error: /swapfile 不存在" >&2
    return 1
  }
  grep -q '^/swapfile none swap' /etc/fstab || {
    echo "Error: /etc/fstab 沒有 /swapfile 的 swap 項目" >&2
    return 1
  }
}
