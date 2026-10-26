#!/bin/bash
# test_pkg.sh — podman 層：在用完即棄的容器裡，對每個平台跑兩次 mksctl install
# 驅動：task test:recipe -- <配方>（配方的 venue 是 podman 時）
# 註記：逐項容錯，捨棄 -e；單一平台失敗由 run_recipe 累積
set -uo pipefail

# podman 容器共用 host kernel、沒有 init，所以這一層只收不碰 kernel 與服務的配方。
# repo 唯讀掛在 /repo（三層統一掛載點），dist/mksctl 在裡面。

[[ -n "${1:-}" ]] || {
  echo "Error: usage: test_pkg.sh <配方>" >&2
  exit 1
}
recipe_name="$1"

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
command -v podman >/dev/null || {
  echo "Error: 需要 podman（podman 層）" >&2
  exit 1
}
[[ -x dist/mksctl ]] || {
  echo "Error: 找不到 dist/mksctl，先跑 task build" >&2
  exit 1
}

# run_one <平台層> <配方> — 回傳容器內 install 的結束碼
run_one() {
  local layer="$1" name="$2"
  local boot

  boot="$(boot_cmd "${layer}")" || return 1
  # CWD 設可寫的 /root：配方可能相對 CWD 寫暫存檔
  podman run --rm -v "${PWD}:/repo:ro" -w /root "${PODMAN_IMAGE[${layer}]}" \
    sh -c "${boot} && $(install_twice_cmd "${name}")"
}

# main
# shellcheck source=/dev/null
source tests/lib/harness.sh
run_recipe podman "${recipe_name}"
