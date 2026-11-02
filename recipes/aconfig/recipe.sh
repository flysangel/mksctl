#!/bin/bash
# recipe.sh — aconfig 聚合器：實際工作都在 recipe.yaml 的 steps 配方裡
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       這裡只驗整條鏈跑完後最外顯的結果；各步驟配方自己的 verify 另有其檔

aconfig_install() {
  return 0
}

aconfig_verify() {
  command -v sudo >/dev/null || {
    echo "Error: aconfig 應已安裝 sudo" >&2
    return 1
  }
  case "${OS_ID}" in
  alpine)
    grep -q '^%wheel ALL=(ALL:ALL) NOPASSWD:ALL' /etc/sudoers.d/nopasswd || {
      echo "Error: /etc/sudoers.d/nopasswd 沒有 %wheel 的免密規則" >&2
      return 1
    }
    id -u bigred >/dev/null || {
      echo "Error: aconfig 應已建立 bigred 帳號" >&2
      return 1
    }
    ;;
  ubuntu)
    grep -q '^%sudo ALL=(ALL:ALL) NOPASSWD:ALL' /etc/sudoers.d/nopasswd || {
      echo "Error: /etc/sudoers.d/nopasswd 沒有 %sudo 的免密規則" >&2
      return 1
    }
    locale -a | grep -qi zh_TW || {
      echo "Error: aconfig 應已產生 zh_TW locale" >&2
      return 1
    }
    ;;
  *)
    echo "Error: aconfig 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac
}
