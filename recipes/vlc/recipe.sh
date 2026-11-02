#!/bin/bash
# recipe.sh — vlc 配方：裝 VLC 媒體播放器
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；apt 的安裝是冪等的

vlc_install() {
  pkg_install vlc || return 1
}

vlc_verify() {
  local vlc_path
  vlc_path="$(command -v vlc)" || {
    echo "Error: 找不到 vlc" >&2
    return 1
  }
  [[ "$(readlink -f "${vlc_path}")" != /snap/* ]] || {
    echo "Error: vlc 指向 snap（${vlc_path}）" >&2
    return 1
  }
}
