#!/bin/bash
# test_xrdp_account.sh — 驗 xrdp 配方重跑不會把既有帳號的密碼重設成預設值
# 驅動：task test:xrdp-account
# 註記：只測帳號邏輯（_xrdp_account），不裝 xrdp 與桌面，所以用 podman 容器就夠；
#       完整的 xrdp 安裝由 task test:recipe -- xrdp 在 vm 層驗。
#       三個案例：新建帳號用預設密碼；重跑沒帶 XRDP_PASSWD 時 /etc/shadow 的雜湊不變；
#       重跑帶了 XRDP_PASSWD 時雜湊才改變。
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
command -v podman >/dev/null || {
  echo "Error: 需要 podman" >&2
  exit 1
}
# 映像取自 tests/platforms.conf 的 ubuntu-24.04 那一行
image="$(awk -F'|' '$1 ~ /^ *ubuntu-24.04 *$/ { gsub(/ /, "", $2); print $2 }' tests/platforms.conf)"
[[ -n "${image}" ]] || {
  echo "Error: tests/platforms.conf 沒有 ubuntu-24.04" >&2
  exit 1
}

# 容器內跑的腳本。單引號是刻意的：變數要留到容器裡才展開
# shellcheck disable=SC2016
inner='
set -u
. /repo/recipes/xrdp/recipe.sh
groupadd -f adm
groupadd -f sudo
hash_of() { awk -F: -v u="$1" "\$1 == u { print \$2 }" /etc/shadow; }

state="$(_xrdp_account bob "")" || { echo "[FAIL] 新建帳號失敗"; exit 1; }
[ "$state" = new ] || { echo "[FAIL] 新建帳號應回報 new，實際 $state"; exit 1; }
h0="$(hash_of bob)"
printf "bob:%s\n" mine | chpasswd
h1="$(hash_of bob)"
state="$(_xrdp_account bob "")" || { echo "[FAIL] 重跑失敗"; exit 1; }
[ "$state" = existing ] || { echo "[FAIL] 重跑應回報 existing，實際 $state"; exit 1; }
h2="$(hash_of bob)"
[ "$h1" = "$h2" ] || { echo "[FAIL] 沒帶 XRDP_PASSWD 重跑，雜湊被改了"; exit 1; }
echo "[OK]   沒帶 XRDP_PASSWD 重跑，既有帳號的雜湊不變"
_xrdp_account bob newpass >/dev/null || { echo "[FAIL] 帶密碼重跑失敗"; exit 1; }
h3="$(hash_of bob)"
[ "$h3" != "$h2" ] || { echo "[FAIL] 帶了 XRDP_PASSWD，雜湊應該要變"; exit 1; }
echo "[OK]   帶 XRDP_PASSWD 重跑，雜湊才改變"
[ "$h0" != "" ] || { echo "[FAIL] 新建帳號沒有密碼雜湊"; exit 1; }
echo "[OK]   新建帳號有預設密碼的雜湊"
'

podman run --rm -v "${PWD}:/repo:ro" "${image}" bash -c "${inner}"
