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

# 場地的預期配方數：跑完拿來和實際跑過的數量比對
expected="$(awk -v v="${venue}" '$2 == v' <<<"${listing}" | wc -l)"

ran=0
failed=()
# 用 fd 3 讀清單：task test:recipe 會繼承 stdin，吃掉後面的配方行會讓配方少跑卻仍通過
while read -r -u 3 name v _; do
  if [[ "${v}" != "${venue}" ]]; then
    continue
  fi
  ran=$((ran + 1))
  echo "::group::${name}"
  task test:recipe -- "${name}" || failed+=("${name}")
  echo "::endgroup::"
done 3<<<"${listing}"

if ((ran == 0)); then
  echo "Error: ${venue} 層沒有任何配方可跑（場地名拼錯，或清單是空的）" >&2
  exit 1
fi
if ((ran != expected)); then
  echo "Error: ${venue} 層清單有 ${expected} 個配方，實際只跑了 ${ran} 個" >&2
  exit 1
fi
if ((${#failed[@]} > 0)); then
  echo "Error: ${venue} 層失敗的配方：${failed[*]}" >&2
  exit 1
fi
echo "[OK] ${venue} 層 ${ran} 個配方全部通過"
