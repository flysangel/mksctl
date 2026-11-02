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

# _disableSwap_comment_fstab — 把第 3 欄是 swap 的未註解行前面加 #
# 先寫到暫存檔，再用 cat 覆寫 fstab，保留原檔的權限與擁有者
_disableSwap_comment_fstab() {
  local tmp rc=0
  tmp="$(mktemp)" || return 1
  awk '$1 !~ /^#/ && $3 == "swap" { print "#" $0; next } { print }' /etc/fstab >"${tmp}" || rc=$?
  if [[ "${rc}" -eq 0 ]]; then
    cat "${tmp}" >/etc/fstab || rc=$?
  fi
  rm -f "${tmp}"
  return "${rc}"
}

disableSwap_install() {
  local files path

  # 先在 swapoff 與註解 fstab 之前取清單，之後兩個來源都空了
  files="$(_disableSwap_files)" || return 1

  # 即時關閉所有 swap；沒有啟用的 swap 時 swapoff 會回非零，故容錯
  swapoff -a || true
  # swapoff 失敗（例如記憶體不夠吞回 swap 的內容）時 swap 還在用；
  # 此時刪 swap 檔會害系統崩潰，所以以 /proc/swaps 為準，還有就停止，不動 fstab 也不刪檔
  if awk 'NR > 1' /proc/swaps | grep -q .; then
    echo "Error: swapoff 後 swap 仍在啟用，不刪 swap 檔也不改 /etc/fstab" >&2
    return 1
  fi

  # 把 fstab 內尚未註解、且第 3 欄是 swap 的行前面加 #，讓關閉狀態在重開機後仍生效
  # 只看第 3 欄：掛載點路徑含 swap 字樣的一般項目（例如 /mnt/swapdata）不能註解掉
  if [[ -f /etc/fstab ]]; then
    _disableSwap_comment_fstab || return 1
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
