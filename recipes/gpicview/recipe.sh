#!/bin/bash
# recipe.sh — gpicview 配方：裝輕量圖片檢視器
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；apt 的安裝是冪等的

gpicview_install() {
  pkg_install gpicview || return 1
}

gpicview_verify() {
  command -v gpicview >/dev/null || {
    echo "Error: 找不到 gpicview" >&2
    return 1
  }
}
