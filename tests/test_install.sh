#!/bin/bash
# test_install.sh — 用本機 https 伺服器模擬 GitHub release，驗 bin/install.sh 的下載、比對與落地
# 驅動：task test
# 註記：逐項容錯，捨棄 -e；每個案例的失敗由 fail 累積
set -uo pipefail

# 假的 mksctl 是印版本的 shell 腳本：install.sh 用它的 version 輸出組 Success 訊息。
# MKSCTL_SYSROOT 讓 install.sh 落地到暫存根目錄，也略過 root 檢查。
# install.sh 只走 https，所以伺服器用自簽憑證，curl 經 CURL_CA_BUNDLE 信任它。
# install.sh 不加任何放行 http 的開關。

# 給 trap 用：暫存根目錄與伺服器的 pid
W=""
SERVER_PIDS=()

cleanup() {
  local pid

  for pid in ${SERVER_PIDS[@]+"${SERVER_PIDS[@]}"}; do
    kill "${pid}" 2>/dev/null || true
  done
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

# free_port — 印一個本機沒人用的 TCP 埠
free_port() {
  python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
}

# wait_up <網址> — 等伺服器能回應，最多 5 秒
wait_up() {
  local url="$1"

  for _ in $(seq 50); do
    curl -fsS -o /dev/null "${url}" 2>/dev/null && return 0
    sleep 0.1
  done
  echo "[FAIL] 伺服器沒有起來：${url}" >&2
  return 1
}

# requests <日誌> — 印伺服器收過的請求數
requests() {
  command grep -c '"GET ' "$1" || true
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

for cmd in python3 openssl; do
  command -v "${cmd}" >/dev/null || {
    echo "[FAIL] 需要 ${cmd} 提供 https 伺服器" >&2
    exit 1
  }
done

W="$(mktemp -d)"
trap cleanup EXIT

srv="${W}/srv"
make_release "${srv}/latest/download" v0.0.2
make_release "${srv}/download/v0.0.1" v0.0.1
make_release "${srv}/download/v0.0.9" v0.0.9
# v0.0.9 的 checksums.txt 換成別的內容的雜湊，模擬下載檔被竄改
printf 'tampered' | sha256sum | sed 's/-$/mksctl-linux-amd64/' >"${srv}/download/v0.0.9/checksums.txt"

openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=127.0.0.1 \
  -addext subjectAltName=IP:127.0.0.1 -keyout "${W}/key.pem" -out "${W}/cert.pem" 2>/dev/null || {
  echo "[FAIL] openssl 產生自簽憑證失敗" >&2
  exit 1
}
export CURL_CA_BUNDLE="${W}/cert.pem"

# https 伺服器。/to-http/ 開頭的路徑回 302，轉到同一份檔的 http 網址，給轉址案例用。
cat >"${W}/server.py" <<'PY'
import functools
import http.server
import ssl
import sys

directory, port, cert, key, http_port = sys.argv[1:6]


class Handler(http.server.SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith("/to-http/"):
            self.send_response(302)
            rest = self.path[len("/to-http"):]
            self.send_header("Location", f"http://127.0.0.1:{http_port}{rest}")
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        super().do_GET()


handler = functools.partial(Handler, directory=directory)
httpd = http.server.ThreadingHTTPServer(("127.0.0.1", int(port)), handler)
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(cert, key)
httpd.socket = ctx.wrap_socket(httpd.socket, server_side=True)
httpd.serve_forever()
PY

port="$(free_port)"
http_port="$(free_port)"
log="${W}/server.log"
http_log="${W}/http.log"
python3 "${W}/server.py" "${srv}" "${port}" "${W}/cert.pem" "${W}/key.pem" "${http_port}" 2>"${log}" >/dev/null &
SERVER_PIDS+=("$!")
# 同一份檔的純 http 伺服器：install.sh 若肯走 http，就會從這裡拿到檔並裝成功。
python3 -m http.server --bind 127.0.0.1 --directory "${srv}" "${http_port}" 2>"${http_log}" >/dev/null &
SERVER_PIDS+=("$!")
base="https://127.0.0.1:${port}"
http_base="http://127.0.0.1:${http_port}"
wait_up "${base}/latest/download/checksums.txt" || exit 1
wait_up "${http_base}/latest/download/checksums.txt" || exit 1

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
before="$(requests "${log}")"
head -c "$((size / 2))" "${script}" | MKSCTL_SYSROOT="${root}" bash -s -- --base-url "${base}" >/dev/null 2>&1
after="$(requests "${log}")"
if [[ -z "$(ls -A "${root}")" ]] && ((before == after)); then
  ok "腳本被截斷（只餵前半段）時什麼都不執行"
else
  bad "腳本被截斷（只餵前半段）時什麼都不執行（請求數 ${before} → ${after}）"
fi

exit "${fail}"
