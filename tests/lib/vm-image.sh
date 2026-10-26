#!/bin/bash
# vm-image.sh — qemu 層的基底映像快取與即棄 overlay
# 驅動：tests/test_vm.sh 用 source 載入
# 註記：函式庫不設 set 旗標，set -e 會污染呼叫端的 shell；失敗由各函式 return 1
#
# 基底映像下載一次存進 .cache/vm（已進 .gitignore），每次測試從它開一層 qcow2 overlay，
# 跑完丟掉，隔離性等同 podman 的 --rm。
# 用官方 cloud image 而非自建基底：自建的會變成第二個要維護的產物，
# 且它與真實 cloud image 的差異會成為新的保真度缺口。映像網址在 tests/platforms.conf。

VM_CACHE=".cache/vm"

# vm_base <平台層> <映像網址> — 確保基底映像在本地快取，路徑印到 stdout
vm_base() {
  local layer="$1" url="$2" base

  base="${VM_CACHE}/base-${layer}.qcow2"
  mkdir -p "${VM_CACHE}"
  if [[ ! -f "${base}" ]]; then
    echo "Info: 下載 ${layer} 基底映像（只會做一次）" >&2
    curl -fsSL "${url}" -o "${base}.part" || {
      rm -f "${base}.part"
      return 1
    }
    mv "${base}.part" "${base}"
  fi
  printf '%s\n' "${base}"
}

# vm_overlay <基底> <名稱> — 從基底開一層即棄 overlay，路徑印到 stdout
vm_overlay() {
  local base="$1" name="$2" ov

  ov="${VM_CACHE}/ov-${name}.qcow2"
  rm -f "${ov}"
  qemu-img create -q -f qcow2 -F qcow2 -b "$(realpath "${base}")" "${ov}" 8G || return 1
  printf '%s\n' "${ov}"
}
