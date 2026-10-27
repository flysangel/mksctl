#!/bin/bash
# recipe.sh — sudopw 配方：鎖定 root 密碼，管理員群組免密碼 sudo
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       sudopw 不依賴 bigred；bigred 把帳號加進 wheel 或 sudo 群組，順序由 aconfig 的 steps 保證
#       免密規則寫成 /etc/sudoers.d 底下的獨立檔，不依賴 /etc/sudoers 的既有內容：
#       alpine cloud image 的 /etc/sudoers 沒有 %wheel 那一行，改檔會空轉

# _sudopw_group — 印出本發行版的管理員群組
_sudopw_group() {
  case "${OS_ID}" in
  alpine) echo wheel ;;
  ubuntu) echo sudo ;;
  *)
    echo "Error: sudopw 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac
}

sudopw_install() {
  local group members
  group="$(_sudopw_group)" || return 1

  pkg_install sudo || return 1

  # 鎖 root 之前，管理員群組至少要有一個成員；否則 root 被鎖、又沒有人能 sudo，只能進救援模式
  # alpine 的 wheel 群組由 base 映像提供；沒有的話，免密規則會對不到任何人
  members="$(getent group "${group}" | cut -d: -f4)" || members=""
  [[ -n "${members}" ]] || {
    echo "Error: 群組 ${group} 沒有成員。現在鎖 root，會沒有人能登入管理機器。請先安裝 bigred 或 aconfig，建立管理員帳號再裝 sudopw" >&2
    return 1
  }

  # 清除並鎖定 root 密碼，禁止以 root 密碼登入
  passwd -dl root || return 1

  mkdir -p /etc/sudoers.d || return 1
  chmod 0755 /etc/sudoers.d || return 1
  printf '%%%s ALL=(ALL:ALL) NOPASSWD:ALL\n' "${group}" >/etc/sudoers.d/nopasswd || return 1
  # sudo 對過鬆的權限會拒絕載入
  chmod 0440 /etc/sudoers.d/nopasswd || return 1

  # 語法錯的 sudoers 會讓所有人失去 sudo，寫完立刻檢查
  visudo -cf /etc/sudoers.d/nopasswd >/dev/null || {
    echo "Error: /etc/sudoers.d/nopasswd 語法檢查失敗" >&2
    return 1
  }
}

sudopw_verify() {
  local group
  group="$(_sudopw_group)" || return 1
  grep -q "^%${group} ALL=(ALL:ALL) NOPASSWD:ALL" /etc/sudoers.d/nopasswd || {
    echo "Error: /etc/sudoers.d/nopasswd 沒有 %${group} 的免密規則" >&2
    return 1
  }
}
