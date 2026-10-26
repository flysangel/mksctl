#!/bin/bash
# test_lxc.sh — LXC 層：privileged 系統容器（有真 init），對每個平台跑兩次 mksctl install
# 驅動：task test:recipe -- <配方>（配方的 venue 是 lxc 時）
# 註記：逐項容錯，捨棄 -e；單一平台失敗由 run_recipe 累積
set -uo pipefail

# 容器是 privileged：容器內 root 等於 host root，受測配方寫錯路徑會動到開發者機器。
# 所以會 swapoff -a 或 modprobe 的配方（動 host 的 swap 與 kernel）一律宣告 vm，不宣告 lxc。
# 升級路徑是改 unprivileged，需要 subuid／subgid 映射與 cgroup 委派。
# 用 lxc-utils 而非 incus 或 LXD：LXD 在 noble 只剩 snap，incus 帶 daemon，這層用依賴最輕的即可。

[[ -n "${1:-}" ]] || {
  echo "Error: usage: test_lxc.sh <配方>" >&2
  exit 1
}
recipe_name="$1"

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
command -v lxc-create >/dev/null || {
  echo "Error: 需要 lxc-utils（LXC 層）：sudo apt-get install -y lxc-utils" >&2
  exit 1
}
[[ -x dist/mksctl ]] || {
  echo "Error: 找不到 dist/mksctl，先跑 task build" >&2
  exit 1
}

# wait_ready <容器> — 最多 60 秒，輪詢「有預設路由且解析得了 dl-cdn.alpinelinux.org」
# 固定睡 5 秒不夠：慢的時候網路還沒好，安裝會在 apt 或 apk 那步假失敗。
wait_ready() {
  local n="$1"

  # 單引號是刻意的：迴圈在容器裡跑，變數要留到容器裡才展開
  # shellcheck disable=SC2016
  sudo lxc-attach -n "${n}" -- sh -c '
    if command -v systemctl >/dev/null; then
      systemctl is-system-running --wait >/dev/null 2>&1 || true
    fi
    i=0
    while [ "$i" -lt 60 ]; do
      if grep -qE "^[^[:space:]]+[[:space:]]+00000000[[:space:]]" /proc/net/route &&
        getent hosts dl-cdn.alpinelinux.org >/dev/null 2>&1; then
        exit 0
      fi
      i=$((i + 1))
      sleep 1
    done
    exit 1
  ' || {
    echo "Error: 60 秒內沒有預設路由，或解析不了 dl-cdn.alpinelinux.org" >&2
    return 1
  }
}

# run_one <平台層> <配方> — 回傳容器內 install 的結束碼
run_one() {
  local layer="$1" name="$2"
  local n boot rc
  local template

  n="mksctl-${name}-${layer//./-}"
  boot="$(boot_cmd "${layer}")" || return 1
  # 樣板值是多個獨立參數，要讓它拆開
  read -r -a template <<<"${LXC_TEMPLATE[${layer}]}"

  sudo lxc-destroy -f -n "${n}" >/dev/null 2>&1 || true
  sudo lxc-create -q -n "${n}" -t download -- "${template[@]}" || return 1

  # repo 唯讀 bind mount 到容器內的 /repo（三層統一掛載點）
  echo "lxc.mount.entry = ${PWD} repo none bind,ro,create=dir 0 0" |
    sudo tee -a "/var/lib/lxc/${n}/config" >/dev/null

  sudo lxc-start -n "${n}" || return 1
  rc=0
  wait_ready "${n}" || rc=1
  if ((rc == 0)); then
    sudo lxc-attach -n "${n}" -- sh -c "${boot} && $(install_twice_cmd "${name}")" || rc=$?
  fi

  sudo lxc-stop -k -n "${n}" >/dev/null 2>&1 || true
  sudo lxc-destroy -f -n "${n}" >/dev/null 2>&1 || true
  return "${rc}"
}

# main
# shellcheck source=/dev/null
source tests/lib/harness.sh
run_recipe lxc "${recipe_name}"
