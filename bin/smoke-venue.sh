#!/bin/bash
# smoke-venue.sh — 對 mksctl list --all 中某個場地的每個配方跑 task test:recipe，印失敗清單
# 驅動：.github/workflows/release.yml 的 smoke job、.github/workflows/weekly.yml
# 註記：逐項容錯，捨棄 -e；單一配方失敗由 failed 累積，最後一起回報
set -uo pipefail

[[ -n "${1:-}" ]] || {
  echo "Error: usage: smoke-venue.sh <podman|lxc|vm>" >&2
  exit 1
}
venue="$1"

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
task build || {
  echo "Error: task build 失敗" >&2
  exit 1
}
listing="$(./dist/mksctl list --all)" || {
  echo "Error: mksctl list --all 失敗" >&2
  exit 1
}

ran=0
failed=()
while read -r name v _; do
  if [[ "${v}" != "${venue}" ]]; then
    continue
  fi
  ran=$((ran + 1))
  echo "::group::${name}"
  task test:recipe -- "${name}" || failed+=("${name}")
  echo "::endgroup::"
done <<<"${listing}"

if ((ran == 0)); then
  echo "Error: ${venue} 層沒有任何配方可跑（場地名拼錯，或清單是空的）" >&2
  exit 1
fi
if ((${#failed[@]} > 0)); then
  echo "Error: ${venue} 層失敗的配方：${failed[*]}" >&2
  exit 1
fi
echo "[OK] ${venue} 層 ${ran} 個配方全部通過"
