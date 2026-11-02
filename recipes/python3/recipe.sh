#!/bin/bash
# recipe.sh — python3 配方：裝 Python 3、pip 與編譯 C 擴充模組用的工具，設 python、pip 別名與 pip.conf
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt 的安裝是冪等的
#       requires: apt：舊 provision 只有 ubuntu 版，alpine 不支援
#       build-essential、libssl-dev、libffi-dev 供編譯 C 擴充模組用
#       pip.conf 在 files/etc/xdg/pip/pip.conf：index-url 釘官方 PyPI，timeout 拉長以免網路慢時逾時

# _python3_add_alias <別名> <指令> — 附加到 /etc/bash.bashrc，已有就略過
_python3_add_alias() {
  local line="alias $1='$2'"

  if ! grep -qxF "${line}" /etc/bash.bashrc; then
    echo "${line}" >>/etc/bash.bashrc || return 1
  fi
}

python3_install() {
  pkg_install python3 python3-pip build-essential libssl-dev libffi-dev || return 1
  _python3_add_alias python python3 || return 1
  _python3_add_alias pip pip3 || return 1
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/xdg/pip/pip.conf" \
    /etc/xdg/pip/pip.conf || return 1
}

python3_verify() {
  local tool
  for tool in python3 pip3; do
    command -v "${tool}" >/dev/null || {
      echo "Error: 找不到 ${tool}" >&2
      return 1
    }
  done
  python3 --version >/dev/null || {
    echo "Error: python3 --version 失敗" >&2
    return 1
  }
  pip3 --version >/dev/null || {
    echo "Error: pip3 --version 失敗" >&2
    return 1
  }
}
