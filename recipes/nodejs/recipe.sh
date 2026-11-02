#!/bin/bash
# recipe.sh — nodejs 配方：裝 Node.js 與 npm，並關掉 npm 的廣告與 audit
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt、apk 的安裝是冪等的
#       刻意例外「不釘版本」：ubuntu 走 NodeSource，而 NodeSource 沒有「最新 LTS」通道
#       （node_lts.x、node_current.x 都是 404），只有 node_<major>.x，所以釘目前的 LTS major 24。
#       新 LTS 出現時，改 recipe.yaml 的 uris 並發新 tag。理由也寫在 docs/ARCHITECTURE.md 〈設計取捨〉。
#       為什麼不用 ubuntu 預設的 nodejs：24.04 的 18.x 太舊，gemini-cli 在 Node 18 上會拋
#       ReferenceError: Response is not defined
#       ubuntu 的 apt 來源（deb822）與金鑰由 mksctl 依 recipe.yaml 的 apt_sources 處理；
#       suite 固定是 nodistro，這是 NodeSource 官方值，不是發行版 codename
#       alpine 的 community／main 自帶夠新的 nodejs 與 npm，不釘版本
#       NodeSource 的 nodejs 已含 npm，所以 pkgmap.yaml 把 apt 的 npm 對到 []

nodejs_install() {
  # ubuntu 套件庫的 npm 與 NodeSource 的 nodejs 互相衝突，先移除舊版
  if command -v dpkg >/dev/null && dpkg -s npm >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get remove -y nodejs npm || return 1
  fi
  pkg_install nodejs npm || return 1
  # 關閉 npm 廣告訊息以減少 CI 干擾
  npm config set fund false --global || return 1
  npm config set audit false --global || return 1
}

nodejs_verify() {
  local tool
  for tool in node npm; do
    command -v "${tool}" >/dev/null || {
      echo "Error: 找不到 ${tool}" >&2
      return 1
    }
  done
  node --version >/dev/null || {
    echo "Error: node --version 失敗" >&2
    return 1
  }
  npm --version >/dev/null || {
    echo "Error: npm --version 失敗" >&2
    return 1
  }
}
