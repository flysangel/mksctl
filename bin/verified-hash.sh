#!/bin/bash
# verified-hash.sh — 印出某配方的內容雜湊，給 tests/verified.conf 判斷紀錄是否過期
# 驅動：tests/lib/harness.sh 在 lxc 與 vm 層通過後呼叫
set -euo pipefail

base_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[[ -n "${1:-}" ]] || {
  echo "Error: usage: verified-hash.sh <配方>" >&2
  exit 1
}
recipe_name="$1"

cd "${base_dir}/.." || {
  echo "Error: 進不了 repo 根" >&2
  exit 1
}
[[ -d "recipes/${recipe_name}" ]] || {
  echo "Error: 找不到配方 ${recipe_name}" >&2
  exit 1
}

# 雜湊的輸入是 git index 的 blob 雜湊，所以改了檔要 git add 才會讓雜湊變
{
  git ls-files -s -- "recipes/${recipe_name}" platforms pkgmap.yaml trust go.mod go.sum
  git ls-files -s -- '*.go'
} | sha256sum | cut -d' ' -f1
