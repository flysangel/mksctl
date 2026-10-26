#!/bin/bash
# recipe.sh — ntp 配方：時區設台北，向國家時間標準伺服器校時一次，啟用時間同步服務
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       ubuntu 26.04 起沒有 ntpdate 套件，改裝 ntpsec-ntpdate，對照在 pkgmap.yaml
#       用哪個時間同步服務由平台層的 time_sync_enable 決定，見 platforms/ubuntu.sh 與 apk.sh

ntp_install() {
  pkg_install tzdata ntpdate || return 1

  # 以符號連結把系統時區指向台北；-f 覆寫既有連結，重跑不會壞
  ln -snf /usr/share/zoneinfo/Asia/Taipei /etc/localtime || return 1

  # 沒有 init（容器、chroot、映像建置）時沒有時間服務可啟用，也不該動時鐘，整段略過
  if [[ "${MKSCTL_INIT_RUNNING:-0}" != "1" ]]; then
    echo "Info: 沒有 init，略過時間同步" >&2
    return 0
  fi

  if command -v timedatectl >/dev/null; then
    timedatectl set-timezone Asia/Taipei || return 1
  fi

  # 立即向國家時間標準伺服器校時一次，縮短首次同步等待；校時失敗（網路、伺服器）不擋安裝
  if command -v ntpdate >/dev/null; then
    ntpdate -u tock.stdtime.gov.tw || echo "Info: 一次性校時失敗，交給時間同步服務" >&2
  fi

  time_sync_enable || return 1
}

ntp_verify() {
  local target
  [[ -L /etc/localtime ]] || {
    echo "Error: /etc/localtime 不是符號連結" >&2
    return 1
  }
  target="$(readlink /etc/localtime)" || return 1
  [[ "${target}" == *Asia/Taipei ]] || {
    echo "Error: 時區不是 Asia/Taipei：${target}" >&2
    return 1
  }
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    time_sync_active || {
      echo "Error: 沒有執行中的時間同步服務" >&2
      return 1
    }
  fi
}
