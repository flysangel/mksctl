#!/bin/bash
# test_check_verified.sh — 用假的 mksctl、假的雜湊指令與假的 verified.conf 驗 bin/check-verified.sh
# 驅動：task test
# 註記：逐項容錯，捨棄 -e；每個案例的失敗由 bad 累積
set -uo pipefail

fail=0
ok() { echo "[OK] $1"; }
bad() {
  echo "[FAIL] $1" >&2
  fail=1
}

W="$(mktemp -d)"
trap 'rm -rf "${W}"' EXIT

cd "$(git rev-parse --show-toplevel)" || exit 1

# 假的 mksctl：兩個配方，r1 是 lxc、r2 是 podman；兩個平台都支援兩者
cat >"${W}/mksctl" <<'FAKE'
#!/bin/bash
case "$*" in
"list --all") printf 'r1 lxc\nr2 podman\n' ;;
"list --platform "*) printf 'r1\nr2\n' ;;
*) exit 1 ;;
esac
FAKE
chmod +x "${W}/mksctl"

# 假的雜湊指令：印 H-<配方>
cat >"${W}/hash.sh" <<'FAKE'
#!/bin/bash
echo "H-$1"
FAKE

cat >"${W}/platforms.conf" <<'EOF2'
pa | img | -d a | url
pb | img | -d b | url
EOF2

today="$(date -u +%F)"
old="$(date -u -d '10 days ago' +%F)"

# run_check <conf 內容> — 跑 check-verified.sh，輸出放 ${W}/out，結束碼放 rc
run_check() {
  printf '%s\n' "$1" >"${W}/verified.conf"
  rc=0
  MKSCTL_BIN="${W}/mksctl" VERIFIED_CONF="${W}/verified.conf" PLATFORMS_CONF="${W}/platforms.conf" \
    VERIFIED_HASH_CMD="${W}/hash.sh" bash bin/check-verified.sh >"${W}/out" 2>&1 || rc=$?
}

run_check "r1 pa lxc H-r1 ${today}
r1 pb lxc H-r1 ${today}"
if ((rc == 0)); then
  ok "雜湊相同且 7 天內通過"
else
  bad "雜湊相同且 7 天內通過（rc=${rc}）"
  cat "${W}/out" >&2
fi

run_check "r1 pa lxc H-r1 ${today}
r1 pb lxc WRONG ${today}"
if ((rc == 1)) && grep -q 'r1 pb' "${W}/out" && ! grep -q 'r1 pa' "${W}/out"; then
  ok "雜湊不同時列出並 exit 1"
else
  bad "雜湊不同時列出並 exit 1（rc=${rc}）"
  cat "${W}/out" >&2
fi

run_check "r1 pa lxc H-r1 ${today}
r1 pb lxc H-r1 ${old}"
if ((rc == 1)) && grep -q 'r1 pb' "${W}/out" && ! grep -q 'r1 pa' "${W}/out"; then
  ok "超過 7 天時列出並 exit 1"
else
  bad "超過 7 天時列出並 exit 1（rc=${rc}）"
  cat "${W}/out" >&2
fi

run_check "r1 pa lxc H-r1 ${today}"
if ((rc == 1)) && grep -q 'r1 pb' "${W}/out"; then
  ok "缺紀錄時列出並 exit 1"
else
  bad "缺紀錄時列出並 exit 1（rc=${rc}）"
  cat "${W}/out" >&2
fi

exit "${fail}"
