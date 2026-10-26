#!/bin/bash
# install.sh — 從 GitHub release 下載 mksctl，比對 checksums.txt 後裝到 /usr/local/bin/mksctl
# 驅動：curl -fsSL https://github.com/flysangel/mksctl/releases/latest/download/install.sh | sudo bash
set -euo pipefail

# 整支包在 main() 裡，最後一行才呼叫：管線中途斷線時，bash 讀到的是不完整的函式定義，
# 會以語法錯誤結束，不會只跑前半段。
# 指定版本：curl -fsSL https://github.com/flysangel/mksctl/releases/download/vX.Y.Z/install.sh | sudo bash -s -- --tag vX.Y.Z
# 使用者的選擇只走參數，不走環境變數：sudo 預設會清掉環境變數。
# MKSCTL_SYSROOT 只給 tests/test_install.sh 用：落地路徑前加上它，並略過 root 檢查。

ASSET="mksctl-linux-amd64"
DEFAULT_BASE="https://github.com/flysangel/mksctl/releases"
USAGE="usage: install.sh [--tag vX.Y.Z] [--base-url <URL>]"

# 給 trap 用：下載暫存目錄與落地前的暫存檔
W=""
TMP_DEST=""

cleanup() {
  if [[ -n "${TMP_DEST}" ]]; then
    rm -f "${TMP_DEST}"
  fi
  if [[ -n "${W}" ]]; then
    rm -rf "${W}"
  fi
}

main() {
  local tag="" base="${DEFAULT_BASE}"
  local sysroot dest_dir url line version cmd

  while (($# > 0)); do
    case "$1" in
    --tag)
      [[ -n "${2:-}" ]] || {
        echo "Error: ${USAGE}" >&2
        return 1
      }
      tag="$2"
      shift 2
      ;;
    --base-url)
      [[ -n "${2:-}" ]] || {
        echo "Error: ${USAGE}" >&2
        return 1
      }
      base="${2%/}"
      shift 2
      ;;
    *)
      echo "Error: ${USAGE}" >&2
      return 1
      ;;
    esac
  done
  if [[ -n "${tag}" && ! "${tag}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: --tag 要寫成 vX.Y.Z：${tag}" >&2
    return 1
  fi

  sysroot="${MKSCTL_SYSROOT:-}"
  if [[ -z "${sysroot}" ]] && ((EUID != 0)); then
    echo "Error: 需要 root 權限，改用 curl -fsSL ... | sudo bash" >&2
    return 1
  fi
  for cmd in curl sha256sum install; do
    command -v "${cmd}" >/dev/null || {
      echo "Error: 找不到 ${cmd}" >&2
      return 1
    }
  done

  if [[ -n "${tag}" ]]; then
    url="${base}/download/${tag}"
  else
    url="${base}/latest/download"
  fi
  dest_dir="${sysroot}/usr/local/bin"

  W="$(mktemp -d)"
  trap cleanup EXIT

  echo "Info: 下載 ${url}/${ASSET}"
  curl -fsSL --retry 5 --retry-delay 2 -o "${W}/${ASSET}" "${url}/${ASSET}" || {
    echo "Error: 下載 ${url}/${ASSET} 失敗" >&2
    return 1
  }
  curl -fsSL --retry 5 --retry-delay 2 -o "${W}/checksums.txt" "${url}/checksums.txt" || {
    echo "Error: 下載 ${url}/checksums.txt 失敗" >&2
    return 1
  }

  # 只取執行檔那一行再比對：--ignore-missing 在 checksums.txt 漏列這個檔時會當成通過。
  line="$(awk -v f="${ASSET}" '$2 == f || $2 == "*" f' "${W}/checksums.txt")"
  [[ -n "${line}" && "${line}" != *$'\n'* ]] || {
    echo "Error: checksums.txt 沒有剛好一行 ${ASSET}" >&2
    return 1
  }
  printf '%s\n' "${line}" >"${W}/expected.sha256"
  (cd "${W}" && sha256sum -c --status expected.sha256) || {
    echo "Error: ${ASSET} 與 checksums.txt 的雜湊不符，沒有安裝" >&2
    return 1
  }

  # 先裝到同目錄的暫存名再 mv：同一個檔案系統內的 rename 是原子的，執行中的舊版不會讀到半個檔。
  mkdir -p "${dest_dir}"
  TMP_DEST="${dest_dir}/.mksctl.new.$$"
  install -m 0755 "${W}/${ASSET}" "${TMP_DEST}"
  mv -f "${TMP_DEST}" "${dest_dir}/mksctl"
  TMP_DEST=""

  version="$("${dest_dir}/mksctl" version)"
  echo "Success: mksctl ${version} 已安裝到 /usr/local/bin/mksctl"
}

main "$@"
