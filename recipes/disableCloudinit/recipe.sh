#!/bin/bash
# recipe.sh — disableCloudinit 配方：移除 cloud-init 與它的設定、狀態目錄
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       只有 ubuntu 有 cloud-init 要處理：requires: apt 讓 alpine 自動略過
#       requires: kernel：只在完整的 VM 或實機上有意義，容器映像本來就沒有 cloud-init 在跑

disableCloudinit_install() {
  local _

  # 等 cloud-init 首次開機流程與 apt 背景作業結束再搶 dpkg lock；
  # 否則剛開機幾分鐘內 apt-get purge 會撞 dpkg-lock-frontend，用 || true 吞掉會變成假成功
  if command -v cloud-init >/dev/null; then
    cloud-init status --wait >/dev/null 2>&1 || true
  fi
  for _ in $(seq 1 60); do
    fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1 || break
    sleep 2
  done

  DEBIAN_FRONTEND=noninteractive apt-get -o DPkg::Lock::Timeout=120 purge -y cloud-init || return 1
  rm -rf /etc/cloud /var/lib/cloud || return 1

  if dpkg -s cloud-init >/dev/null 2>&1; then
    echo "Error: cloud-init 仍然安裝著" >&2
    return 1
  fi
}

disableCloudinit_verify() {
  # install 在同一個 shell 跑過 cloud-init，bash 的命令快取還記著已刪除的路徑，command -v 會誤報；先清掉
  hash -r
  if command -v cloud-init >/dev/null; then
    echo "Error: cloud-init 仍在" >&2
    return 1
  fi
  if [[ -e /etc/cloud || -e /var/lib/cloud ]]; then
    echo "Error: /etc/cloud 或 /var/lib/cloud 仍在" >&2
    return 1
  fi
}
