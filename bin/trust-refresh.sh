#!/bin/bash
# trust-refresh.sh — 重新記錄配方的 trust 雜湊，實測通過才保留
# 驅動：task trust:refresh -- <配方>
# 註記：以下三個環境變數只給 tests/test_trust_refresh.sh 用，平常不要設：
#   MKSCTL_BIN            用現成的 mksctl，不跑 task build
#   MKSCTL_TRUST_CATALOG  trust-record 從這個目錄載入配方，不用嵌入的
#   MKSCTL_TRUST_TEST_CMD 取代 task test:recipe，配方名是它的 $1
set -euo pipefail

# 給 trap 用：備份路徑與是否已成功
BACKUP=""
DONE=0

# 沒成功就把 trust/hashes.yaml 還原成原本的位元組。
# 用 EXIT trap 涵蓋失敗、Ctrl-C 與 kill：雜湊記了卻沒驗過，比沒記更危險，
# 因為下次 install 會把這份未驗證的雜湊當成可信。
cleanup() {
  if [[ -n "${BACKUP}" && -f "${BACKUP}" ]]; then
    if ((DONE == 0)); then
      cp -p "${BACKUP}" trust/hashes.yaml
      echo "Info: 已還原 trust/hashes.yaml" >&2
    fi
    rm -f "${BACKUP}"
  fi
}

# main
if (($# != 1)); then
  echo "Error: 用法：task trust:refresh -- <配方>" >&2
  exit 2
fi
recipe="$1"

# 回到 repo 根：mksctl trust-record --write 用相對路徑找 trust/hashes.yaml
cd "$(dirname "${BASH_SOURCE[0]}")/.."

if [[ ! -f trust/hashes.yaml ]]; then
  echo "Error: 找不到 trust/hashes.yaml" >&2
  exit 1
fi

bin="${MKSCTL_BIN:-}"
if [[ -z "${bin}" ]]; then
  task build >/dev/null
  bin="${PWD}/dist/mksctl"
fi

BACKUP="$(mktemp)"
trap cleanup EXIT
cp -p trust/hashes.yaml "${BACKUP}"

args=(trust-record --write)
if [[ -n "${MKSCTL_TRUST_CATALOG:-}" ]]; then
  args+=(--catalog "${MKSCTL_TRUST_CATALOG}")
fi
"${bin}" "${args[@]}" "${recipe}"
echo "Info: 已更新 trust/hashes.yaml，開始實測 ${recipe}"

if [[ -n "${MKSCTL_TRUST_TEST_CMD:-}" ]]; then
  bash -c "${MKSCTL_TRUST_TEST_CMD}" _ "${recipe}"
else
  task test:recipe -- "${recipe}"
fi

DONE=1
echo "Success: ${recipe} 的 trust 雜湊已更新並通過實測"
echo "Info: 下一步：確認 git diff trust/hashes.yaml，commit 後發新 tag"
