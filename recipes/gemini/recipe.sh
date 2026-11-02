#!/bin/bash
# recipe.sh — gemini 配方：用 npm 全域安裝 Google 維護的 Gemini CLI
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；npm install -g 重跑會更新到最新版
#       刻意例外「下載不經 fetch」：npm 屬套件管理器下載，完整性由 npm registry 的 integrity 驗證，
#       跟 pkg_install 走 apt、apk 同類，所以不用 fetch

gemini_install() {
  npm install -g @google/gemini-cli || return 1
}

gemini_verify() {
  command -v gemini >/dev/null || {
    echo "Error: 找不到 gemini" >&2
    return 1
  }
  gemini --version >/dev/null || {
    echo "Error: gemini --version 失敗" >&2
    return 1
  }
}
