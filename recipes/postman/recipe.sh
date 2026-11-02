#!/bin/bash
# recipe.sh — postman 配方：下載官方 tarball 解到 /opt/postman，並落地 .desktop
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       網址沒有版本號，沒有 latest，每次安裝都會執行；tarball 的完整性靠 trust/hashes.yaml
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過
#       .desktop 範本放 files/，由 MKSCTL_RECIPE_DIR 取得

_POSTMAN_DIR=/opt/postman
_POSTMAN_DESKTOP=/usr/share/applications/postman.desktop

# _postman_install_desktop — 落地 .desktop，並把範本的 EXECPATH、ICONPATH 換成實際路徑
_postman_install_desktop() {
  local execpath="${_POSTMAN_DIR}/Postman"
  local iconpath="${_POSTMAN_DIR}/app/resources/app/assets/icon.png"

  # 固定圖示路徑不存在時（版本改版可能移動），改用 find 搜出第一個 icon.png
  if [[ ! -f "${iconpath}" ]]; then
    iconpath="$(find "${_POSTMAN_DIR}" -name icon.png -type f | head -n 1)" || return 1
  fi
  [[ -f "${iconpath}" ]] || {
    echo "Error: ${_POSTMAN_DIR} 底下找不到 icon.png" >&2
    return 1
  }
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/postman.desktop" "${_POSTMAN_DESKTOP}" || return 1
  sed -i -e "s|EXECPATH|${execpath}|g" -e "s|ICONPATH|${iconpath}|g" "${_POSTMAN_DESKTOP}" || return 1
}

postman_install() {
  local tarball

  pkg_install tar || return 1
  tarball="$(fetch postman)" || return 1
  # tarball 內含一層頂層目錄，--strip-components=1 去掉它，內容直接落在 /opt/postman
  mkdir -p "${_POSTMAN_DIR}" || return 1
  tar -xzf "${tarball}" --strip-components=1 -C "${_POSTMAN_DIR}" || return 1
  _postman_install_desktop || return 1
}

postman_verify() {
  [[ -x "${_POSTMAN_DIR}/Postman" ]] || {
    echo "Error: Postman 執行檔缺或不可執行" >&2
    return 1
  }
  if ldd "${_POSTMAN_DIR}/Postman" 2>/dev/null | command grep -q 'not found'; then
    echo "Error: Postman 缺共享庫" >&2
    return 1
  fi
  [[ -f "${_POSTMAN_DESKTOP}" ]] || {
    echo "Error: 找不到 ${_POSTMAN_DESKTOP}" >&2
    return 1
  }
  # 佔位字必須已被換掉
  if command grep -q -e 'EXECPATH' -e 'ICONPATH' "${_POSTMAN_DESKTOP}"; then
    echo "Error: ${_POSTMAN_DESKTOP} 還有未替換的 EXECPATH 或 ICONPATH" >&2
    return 1
  fi
}
