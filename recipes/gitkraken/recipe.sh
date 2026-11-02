#!/bin/bash
# recipe.sh — gitkraken 配方：下載官方 .deb 並安裝
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       網址沒有版本號，沒有 latest，每次安裝都會執行；.deb 的完整性靠 trust/hashes.yaml
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過
#       舊 provision 用 dpkg -i || true 再 apt-get install -f 補相依；
#       現在直接 apt-get install 本機 .deb，相依一併解開，失敗就中止

gitkraken_install() {
  local deb

  deb="$(fetch gitkraken)" || return 1
  # libsecret 是金鑰圈（keyring）函式庫，舊 provision 一併安裝
  pkg_install libsecret-1-0 libsecret-1-dev || return 1
  DEBIAN_FRONTEND=noninteractive apt-get install -y -o DPkg::Lock::Timeout=300 "${deb}" || return 1
}

gitkraken_verify() {
  local gk_path

  gk_path="$(command -v gitkraken)" || {
    echo "Error: 找不到 gitkraken" >&2
    return 1
  }
  [[ "$(readlink -f "${gk_path}")" != /snap/* ]] || {
    echo "Error: gitkraken 指向 snap（${gk_path}）" >&2
    return 1
  }
  [[ "$(dpkg-query -W -f='${Status}' gitkraken 2>/dev/null)" == "install ok installed" ]] || {
    echo "Error: gitkraken 的 dpkg 狀態不是 install ok installed" >&2
    return 1
  }
}
