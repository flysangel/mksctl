#!/bin/bash
# test_trust_refresh.sh — 用本機 HTTP 伺服器與測試用配方目錄，驗 mksctl trust-record 與 bin/trust-refresh.sh
# 驅動：task test
# 註記：逐項容錯，捨棄 -e；每個案例的失敗由 bad 累積
set -uo pipefail

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

fail=0

# 兩個情境共用的起始檔：有別的配方的紀錄與檔頭註解，驗合併時保留
original="# 檔頭註解
other/keep:
    url: http://example.invalid/keep
    size: 1
    sha256: $(printf 'a%.0s' $(seq 64))
"

ok() { echo "[OK] $1"; }

bad() {
  echo "[FAIL] $1" >&2
  fail=1
}

# 依賴 main 之後才設定的 fake、bin、W，呼叫時都已就緒
run_refresh() {
  (cd "${fake}" && MKSCTL_BIN="${bin}" MKSCTL_TRUST_CATALOG="${W}/catalog" MKSCTL_TRUST_TEST_CMD="$1" bash bin/trust-refresh.sh fake 2>&1)
}

# main
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v python3 >/dev/null || {
  echo "[FAIL] 需要 python3 提供 HTTP 伺服器" >&2
  exit 1
}

W="$(mktemp -d)"
trap cleanup EXIT

# 在暫存的假 repo 裡跑，真正的 trust/hashes.yaml 不會被碰到
fake="${W}/repo"
mkdir -p "${fake}/bin" "${fake}/trust" "${W}/catalog/recipes/fake" "${W}/srv"
cp "${repo}/bin/trust-refresh.sh" "${fake}/bin/"
printf 'fake-artifact-bytes' >"${W}/srv/tool.bin"
expect_sha="$(sha256sum "${W}/srv/tool.bin" | cut -d' ' -f1)"

port="$(python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])')"
python3 -m http.server --bind 127.0.0.1 --directory "${W}/srv" "${port}" >/dev/null 2>&1 &
SERVER_PID=$!
base="http://127.0.0.1:${port}"
for _ in $(seq 50); do
  curl -fsS -o /dev/null "${base}/tool.bin" 2>/dev/null && break
  sleep 0.1
done

cat >"${W}/catalog/recipes/fake/recipe.yaml" <<YAML
venue: vm
artifacts:
  - id: tool
    url: ${base}/tool.bin
    checksum: {trust: true}
YAML

bin="${W}/mksctl"
(cd "${repo}" && CGO_ENABLED=0 go build -o "${bin}" ./cmd/mksctl) || {
  echo "[FAIL] go build 失敗" >&2
  exit 1
}

# 案例一：trust-record 印出正確的 url、size、sha256
out="$("${bin}" trust-record --catalog "${W}/catalog" fake 2>&1)"
want="fake/tool:
    url: ${base}/tool.bin
    size: 19
    sha256: ${expect_sha}"
if [[ "${out}" == "${want}" ]]; then
  ok "trust-record 印出正確的 url、size、sha256"
else
  bad "trust-record 輸出不符：${out}"
fi

# 案例二：實測成功時 trust/hashes.yaml 更新，其餘紀錄與檔頭保留
printf '%s' "${original}" >"${fake}/trust/hashes.yaml"
rc=0
out="$(run_refresh 'true')" || rc=$?
got="$(cat "${fake}/trust/hashes.yaml")"
if ((rc == 0)) && [[ "${out}" == *"Success:"* ]] &&
  [[ "${got}" == "# 檔頭註解"* && "${got}" == *"sha256: ${expect_sha}"* && "${got}" == *"other/keep:"* ]]; then
  ok "實測成功時 trust/hashes.yaml 更新"
else
  bad "成功案例不符（rc=${rc}）：${out}；檔案：${got}"
fi

# 案例三：實測失敗時 trust/hashes.yaml 位元組不變且 exit 1
printf '%s' "${original}" >"${fake}/trust/hashes.yaml"
rc=0
out="$(run_refresh 'exit 7')" || rc=$?
if ((rc != 0)) && cmp -s "${fake}/trust/hashes.yaml" <(printf '%s' "${original}"); then
  ok "實測失敗時 trust/hashes.yaml 還原且 exit 1"
else
  bad "失敗案例不符（rc=${rc}）：${out}"
fi

exit "${fail}"
