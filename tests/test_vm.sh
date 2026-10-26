#!/bin/bash
# test_vm.sh — qemu 層：完整 VM（自己的 kernel），對每個平台跑兩次 mksctl install
# 驅動：task test:recipe -- <配方>（配方的 venue 是 vm 時）
# 註記：逐項容錯，捨棄 -e；單一平台失敗由 run_recipe 累積
set -uo pipefail

# 不用 SSH：受測配方可能改網路，把 harness 自己的連線切斷。
#   結果改用可寫的 virtiofs /out 回傳：VM 把結束碼與 log 寫進去，關機後 host 直接讀。
# guest agent 通道接 null 後端，只為了讓 qemu-guest-agent 服務起得來，不拿它當控制頻道，
#   理由是受測配方可能重啟那個服務，分不清是配方失敗還是控制頻道掉了。
# 一次一台全新 VM、qcow2 overlay 即棄，同一台跑多個會讓改機器狀態的配方互相打架。
# cloud-init 只負責把測試腳本寫進去再用 setsid 脫離執行，
#   這樣配方即使移除 cloud-init，也不會殺掉正在跑的測試行程。

VM_TIMEOUT="${VM_TIMEOUT:-900}"
VIRTIOFSD="${VIRTIOFSD:-/usr/libexec/virtiofsd}"

[[ -n "${1:-}" ]] || {
  echo "Error: usage: test_vm.sh <配方>" >&2
  exit 1
}
recipe_name="$1"

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
[[ -x dist/mksctl ]] || {
  echo "Error: 找不到 dist/mksctl，先跑 task build" >&2
  exit 1
}
[[ -x "${VIRTIOFSD}" ]] || VIRTIOFSD="$(command -v virtiofsd || true)"
[[ -n "${VIRTIOFSD}" ]] || {
  echo "Error: 找不到 virtiofsd（Ubuntu 放在 /usr/libexec/virtiofsd）" >&2
  exit 1
}
for tool in qemu-system-x86_64 qemu-img cloud-localds; do
  command -v "${tool}" >/dev/null || {
    echo "Error: 需要 ${tool}（qemu 層）：sudo apt-get install -y qemu-system-x86 qemu-utils cloud-image-utils virtiofsd" >&2
    exit 1
  }
done
if [[ ! -r /dev/kvm || ! -w /dev/kvm ]]; then
  echo "Error: 讀寫不了 /dev/kvm。已加入 kvm 群組的話要重新登入才生效" >&2
  echo "Info: 不想重登可改跑 sg kvm -c 'task test:recipe -- ${recipe_name}'" >&2
  exit 1
fi

# vm_script <平台層> <配方> — 印測試機裡跑的 POSIX sh 腳本
# 用 POSIX sh 而非 bash：Alpine cloud image 預設沒有 bash，而裝 bash 就在這支腳本裡。
vm_script() {
  local layer="$1" name="$2"
  local tmpl boot

  boot="$(boot_cmd "${layer}")" || return 1
  # 單引號與 __ 佔位是刻意的：$n 與 $rc 要留到 VM 裡才展開
  # shellcheck disable=SC2016
  tmpl='#!/bin/sh
mkdir -p /repo /out
mount -t virtiofs -o ro repo /repo
mount -t virtiofs out /out
if ! { __BOOT__; } >/out/install.log 2>&1; then
  echo 1 >/out/install.rc
  sync
  poweroff -f
fi
rc=0
# virtiofsd 1.10.0 沒有唯讀選項，唯讀靠上面的 -o ro；這裡驗證寫入真的被擋
if touch /repo/.mksctl-rw-probe 2>/dev/null; then
  rm -f /repo/.mksctl-rw-probe
  echo "Error: /repo 不是唯讀" >>/out/install.log
  rc=1
fi
for n in 1 2; do
  [ "$rc" -eq 0 ] || break
  echo "Info: 第 $n 次 install __NAME__" >>/out/install.log
  /repo/dist/mksctl install __NAME__ >>/out/install.log 2>&1 || rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "Error: 第 $n 次 install 結束碼 $rc" >>/out/install.log
    break
  fi
done
echo "$rc" >/out/install.rc
sync
poweroff -f'
  # 替換字串要加引號：bash 5.2 的 patsub_replacement 會把裡面的 & 換成比對到的樣式，boot 裡有 &&
  tmpl="${tmpl//__BOOT__/"${boot}"}"
  printf '%s\n' "${tmpl//__NAME__/"${name}"}"
}

# run_one <平台層> <配方> — 回傳 VM 內 install 的結束碼
run_one() {
  local layer="$1" name="$2"
  local vm_name out seeddir sockdir base ov rc

  vm_name="mksctl-${name}-${layer//./-}"
  out="$(mktemp -d)"
  seeddir="$(mktemp -d)"
  sockdir="$(mktemp -d)"
  clean_path "${out}"
  clean_path "${seeddir}"
  clean_path "${sockdir}"

  base="$(vm_base "${layer}" "${VM_URL[${layer}]}")" || return 1
  ov="$(vm_overlay "${base}" "${vm_name}")" || return 1
  clean_path "${ov}"

  {
    printf '#cloud-config\nwrite_files:\n  - path: /mksctl-run.sh\n    permissions: "0755"\n    content: |\n'
    vm_script "${layer}" "${name}" | sed 's/^/      /'
    printf 'runcmd:\n  - setsid nohup /mksctl-run.sh </dev/null >/dev/null 2>&1 &\n'
  } >"${seeddir}/user-data"
  printf 'instance-id: %s\nlocal-hostname: %s\n' "${vm_name}" "${vm_name}" >"${seeddir}/meta-data"
  cloud-localds "${seeddir}/seed.iso" "${seeddir}/user-data" "${seeddir}/meta-data" || return 1

  "${VIRTIOFSD}" --socket-path="${sockdir}/repo" --shared-dir "${PWD}" --sandbox none >/dev/null 2>&1 &
  local vp1=$!
  clean_pid "${vp1}"
  "${VIRTIOFSD}" --socket-path="${sockdir}/out" --shared-dir "${out}" --sandbox none >/dev/null 2>&1 &
  local vp2=$!
  clean_pid "${vp2}"
  sleep 1

  # qemu -nographic 會讀 stdin，所以導向 /dev/null
  timeout "${VM_TIMEOUT}" qemu-system-x86_64 \
    -enable-kvm -m 2048 -smp 2 -nographic \
    -drive "file=${ov},if=virtio,format=qcow2" \
    -drive "file=${seeddir}/seed.iso,if=virtio,format=raw" \
    -netdev user,id=n0 -device virtio-net-pci,netdev=n0 \
    -device virtio-serial \
    -chardev null,id=qga -device virtserialport,chardev=qga,name=org.qemu.guest_agent.0 \
    -object memory-backend-memfd,id=mem,size=2048M,share=on -numa node,memdev=mem \
    -chardev "socket,id=cr,path=${sockdir}/repo" -device vhost-user-fs-pci,chardev=cr,tag=repo \
    -chardev "socket,id=co,path=${sockdir}/out" -device vhost-user-fs-pci,chardev=co,tag=out \
    </dev/null >/dev/null 2>&1

  if [[ ! -f "${out}/install.rc" ]]; then
    echo "Error: VM 沒有寫出結果（開機失敗、cloud-init 沒跑，或逾時 ${VM_TIMEOUT} 秒）" >&2
    if [[ -f "${out}/install.log" ]]; then
      tail -n 20 "${out}/install.log" >&2
    fi
    return 1
  fi

  rc="$(cat "${out}/install.rc")"
  cat "${out}/install.log"
  return "${rc}"
}

# main
# shellcheck source=/dev/null
source tests/lib/vm-image.sh
# shellcheck source=/dev/null
source tests/lib/harness.sh
run_recipe vm "${recipe_name}"
