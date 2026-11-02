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
# /no-length/ 開頭的路徑不送 Content-Length，讀到連線關閉為止，給讀取中途的上限案例用。
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
        if self.path.startswith("/no-length/"):
            path = self.translate_path(self.path[len("/no-length"):])
            try:
                with open(path, "rb") as f:
                    body = f.read()
            except OSError:
                self.send_error(404)
                return
            self.send_response(200)
            self.end_headers()
            self.wfile.write(body)
            self.close_connection = True
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

root="${W}/root-http"
mkdir -p "${root}"
before="$(requests "${http_log}")"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${http_base}" 2>&1)" || rc=$?
after="$(requests "${http_log}")"
if ((rc == 1 && before == after)) && [[ -z "$(ls -A "${root}")" && "${out}" == *"只收 https://"* ]]; then
  ok "--base-url 是 http:// 時 exit 1 且不送請求"
else
  bad "--base-url 是 http:// 時 exit 1 且不送請求（rc=${rc}，請求數 ${before} → ${after}）：${out}"
fi

root="${W}/root-redir"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
before="$(requests "${http_log}")"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}/to-http" 2>&1)" || rc=$?
after="$(requests "${http_log}")"
# 先確認伺服器真的回了 302，否則這個案例驗不到轉址。
if command grep -qE '"GET /to-http/[^"]*" 302 ' "${log}" &&
  ((rc == 1 && before == after)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"下載"*"失敗"* ]]; then
  ok "https 轉址到 http 時 exit 1、不打 http 且目標檔不變"
else
  bad "https 轉址到 http 時 exit 1、不打 http 且目標檔不變（rc=${rc}，http 請求數 ${before} → ${after}）：${out}"
fi

# PATH 前面放一支假的 uname，模擬 arm64 的機器
fake="${W}/fake-arm64"
mkdir -p "${fake}"
cat >"${fake}/uname" <<'SH'
#!/bin/sh
case "$1" in
-m) echo aarch64 ;;
*) echo Linux ;;
esac
SH
chmod +x "${fake}/uname"
root="${W}/root-arm64"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
before="$(requests "${log}")"
rc=0
out="$(PATH="${fake}:${PATH}" MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" 2>&1)" || rc=$?
after="$(requests "${log}")"
if ((rc == 1 && before == after)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"Linux/aarch64"* ]]; then
  ok "不是 linux/amd64 時 exit 1、不送請求且目標檔不變"
else
  bad "不是 linux/amd64 時 exit 1、不送請求且目標檔不變（rc=${rc}，請求數 ${before} → ${after}）：${out}"
fi

# v0.0.3 的 checksums.txt 有正確那一行，但補到超過 64 KiB
make_release "${srv}/download/v0.0.3" v0.0.3
head -c $((64 << 10)) /dev/zero | tr '\0' '#' >>"${srv}/download/v0.0.3/checksums.txt"
root="${W}/root-big-sums"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" --tag v0.0.3 2>&1)" || rc=$?
if ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"超過上限"* ]]; then
  ok "checksums.txt 超過 64 KiB 時 exit 1 且目標檔不變"
else
  bad "checksums.txt 超過 64 KiB 時 exit 1 且目標檔不變（rc=${rc}）：${out}"
fi

# 同一份 v0.0.3，改走不送 Content-Length 的路徑，驗讀取中途的上限
root="${W}/root-big-sums-nolen"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}/no-length" --tag v0.0.3 2>&1)" || rc=$?
if command grep -q '"GET /no-length/download/v0.0.3/checksums.txt' "${log}" &&
  ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"超過上限"* ]]; then
  ok "沒有 Content-Length 時 checksums.txt 超過 64 KiB 也 exit 1 且目標檔不變"
else
  bad "沒有 Content-Length 時 checksums.txt 超過 64 KiB 也 exit 1 且目標檔不變（rc=${rc}）：${out}"
fi

# PATH 前面放一支拿掉 --max-filesize 的 curl，模擬沒有 Content-Length 時不擋的舊版 curl（< 8.4.0）
oldcurl="${W}/fake-oldcurl"
mkdir -p "${oldcurl}"
cat >"${oldcurl}/curl" <<SH
#!/bin/bash
args=()
while ((\$# > 0)); do
  if [[ "\$1" == --max-filesize ]]; then
    shift 2
    continue
  fi
  args+=("\$1")
  shift
done
exec $(command -v curl) "\${args[@]}"
SH
chmod +x "${oldcurl}/curl"
root="${W}/root-big-sums-oldcurl"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
rc=0
out="$(PATH="${oldcurl}:${PATH}" MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}/no-length" --tag v0.0.3 2>&1)" || rc=$?
if ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"超過上限"* ]]; then
  ok "curl 不擋時，下載後的大小檢查仍擋下超過上限的 checksums.txt"
else
  bad "curl 不擋時，下載後的大小檢查仍擋下超過上限的 checksums.txt（rc=${rc}）：${out}"
fi

# 執行檔上限從 install.sh 讀，不在這裡另寫一份。release.yml 的大小閘門用同一個寫法。
if [[ "$(command grep -E '^MAX_BIN_BYTES=' "${script}")" =~ ^MAX_BIN_BYTES=\$\(\(([0-9]+)\ \<\<\ ([0-9]+)\)\)$ ]]; then
  max_bin=$((BASH_REMATCH[1] << BASH_REMATCH[2]))
  # v0.0.4 的執行檔能跑、雜湊也對，只是比上限多 1 byte。用稀疏檔，不佔磁碟。
  dir="${srv}/download/v0.0.4"
  mkdir -p "${dir}"
  printf '#!/bin/sh\necho v0.0.4\nexit 0\n' >"${dir}/mksctl-linux-amd64"
  truncate -s "$((max_bin + 1))" "${dir}/mksctl-linux-amd64"
  (cd "${dir}" && sha256sum mksctl-linux-amd64 >checksums.txt)
  root="${W}/root-big-bin"
  mkdir -p "${root}/usr/local/bin"
  printf 'old\n' >"${root}/usr/local/bin/mksctl"
  rc=0
  out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" --tag v0.0.4 2>&1)" || rc=$?
  if ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"超過上限"* ]]; then
    ok "執行檔超過 MAX_BIN_BYTES 時 exit 1 且目標檔不變"
  else
    bad "執行檔超過 MAX_BIN_BYTES 時 exit 1 且目標檔不變（rc=${rc}）：${out}"
  fi
  rm -rf "${dir}"
else
  bad "bin/install.sh 沒有 MAX_BIN_BYTES=\$((<數字> << <數字>)) 這一行"
fi

# v0.0.5 的執行檔雜湊對，但跑不起來，模擬架構不符或 noexec 掛載
dir="${srv}/download/v0.0.5"
mkdir -p "${dir}"
printf '#!/bin/sh\nexit 1\n' >"${dir}/mksctl-linux-amd64"
(cd "${dir}" && sha256sum mksctl-linux-amd64 >checksums.txt)
root="${W}/root-broken"
mkdir -p "${root}/usr/local/bin"
printf 'old\n' >"${root}/usr/local/bin/mksctl"
rc=0
out="$(MKSCTL_SYSROOT="${root}" bash "${script}" --base-url "${base}" --tag v0.0.5 2>&1)" || rc=$?
if ((rc == 1)) && [[ "$(cat "${root}/usr/local/bin/mksctl")" == "old" && "${out}" == *"執行 version 失敗"* ]] && [[ "$(ls -A "${root}/usr/local/bin")" == "mksctl" ]]; then
  ok "新檔跑不起來時 exit 1、目標檔不變且不留暫存檔"
else
  bad "新檔跑不起來時 exit 1、目標檔不變且不留暫存檔（rc=${rc}）：${out}"
fi

exit "${fail}"
