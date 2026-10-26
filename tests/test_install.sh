#!/bin/bash
# test_install.sh — 用本機 HTTP 伺服器模擬 GitHub release，驗 bin/install.sh 的下載、比對與落地
# 驅動：task test
# 註記：逐項容錯，捨棄 -e；每個案例的失敗由 fail 累積
set -uo pipefail

# 假的 mksctl 是印版本的 shell 腳本：install.sh 用它的 version 輸出組 Success 訊息。
# MKSCTL_SYSROOT 讓 install.sh 落地到暫存根目錄，也略過 root 檢查。

# 給 trap 用：暫存根目錄與 HTTP 伺服器的 pid
W=""
SERVER_PID=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" 2>/dev/null || true
  fi
  if [[ -n "${W}" ]]; then
    rm -rf "${W}"
  fi
}

# make_release <目錄> <版本> — 放一份假執行檔與對應的 checksums.txt
make_release() {
  local dir="$1" version="$2"

  mkdir -p "${dir}"
  printf '#!/bin/sh\necho %s\n' "${version}" >"${dir}/mksctl-linux-amd64"
  (cd "${dir}" && sha256sum mksctl-linux-amd64 >checksums.txt)
}

# installed_version <sysroot> — 印落地的 mksctl 回報的版本，沒有檔時印空字串
installed_version() {
  local bin="$1/usr/local/bin/mksctl"

  if [[ -x "${bin}" ]]; then
    "${bin}" version
  fi
}

ok() { echo "[OK] $1"; }

bad() {
  echo "[FAIL] $1" >&2
  fail=1
}

# main
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${repo}/bin/install.sh"
fail=0

command -v python3 >/dev/null || {
  echo "[FAIL] 需要 python3 提供 HTTP 伺服器" >&2
  exit 1
}

W="$(mktemp -d)"
trap cleanup EXIT

srv="${W}/srv"
make_release "${srv}/latest/download" v0.0.2
make_release "${srv}/download/v0.0.1" v0.0.1
make_release "${srv}/download/v0.0.9" v0.0.9
# v0.0.9 的 checksums.txt 換成別的內容的雜湊，模擬下載檔被竄改
printf 'tampered' | sha256sum | sed 's/-$/mksctl-linux-amd64/' >"${srv}/download/v0.0.9/checksums.txt"

port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
log="${W}/server.log"
python3 -m http.server --bind 127.0.0.1 --directory "${srv}" "${port}" 2>"${log}" >/dev/null &
SERVER_PID=$!
base="http://127.0.0.1:${port}"
for _ in $(seq 50); do
  curl -fsS -o /dev/null "${base}/latest/download/checksums.txt" 2>/dev/null && break
  sleep 0.1
done

root="${W}/root-latest"
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" 2>&1)"
if [[ "$(installed_version "${root}")" == "v0.0.2" && "${out}" == *"Success: mksctl v0.0.2 已安裝到 /usr/local/bin/mksctl"* ]]; then
  ok "無 --tag 時安裝 latest 那一份"
else
  bad "無 --tag 時安裝 latest 那一份：${out}"
fi

root="${W}/root-tag"
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" --tag v0.0.1 2>&1)"
if [[ "$(installed_version "${root}")" == "v0.0.1" ]]; then
  ok "--tag v0.0.1 時安裝 v0.0.1 那一份"
else
  bad "--tag v0.0.1 時安裝 v0.0.1 那一份：${out}"
fi

root="${W}/root-bad"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" --tag v0.0.9 2>&1)" || rc=$?
if ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" ]] && [[ "$(ls -A "${root}/usr/local/bin")" == "mksctl" ]]; then
  ok "checksums.txt 不符時 exit 1 且目標檔不變"
else
  bad "checksums.txt 不符時 exit 1 且目標檔不變（rc=${rc}）：${out}"
fi

root="${W}/root-cut"
mkdir -p "${root}"
size="$(wc -c <"${script}")"
before="$(wc -l <"${log}")"
head -c "$((size / 2))" "${script}" | MKSCTL_SYSROOT="${root}" bash -s -- --base-url "${base}" >/dev/null 2>&1
after="$(wc -l <"${log}")"
if [[ -z "$(ls -A "${root}")" ]] && ((before == after)); then
  ok "腳本被截斷（只餵前半段）時什麼都不執行"
else
  bad "腳本被截斷（只餵前半段）時什麼都不執行（請求數 ${before} → ${after}）"
fi

exit "${fail}"
