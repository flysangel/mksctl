#!/bin/bash
# recipe.sh — rclocal 配方：開機時執行自訂腳本（alpine 是 OpenRC 的 local 服務，ubuntu 是 /etc/rc.local）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       ubuntu 不裝 rc-local.service：舊 provision 留過一份 unit 檔，但沒有任何腳本下載它。
#       在 24.04（systemd 255）與 26.04（systemd 259）的 vm 裡實測，兩版都內建
#       /usr/lib/systemd/system/rc-local.service 與 systemd-rc-local-generator。
#       generator 在 /etc/rc.local 存在且可執行時，自動把該服務掛進 multi-user.target，
#       所以只要補齊 /etc/rc.local，不需要自己的 unit。

_rclocal_install_alpine() {
  # local 服務會執行 /etc/local.d/*.start；啟用後開機時才會跑
  svc_enable local || return 1
}

_rclocal_install_ubuntu() {
  # /etc/rc.local 缺檔時建立最小骨架；已有的內容不動
  if [[ ! -f /etc/rc.local ]]; then
    printf '#!/bin/bash\n\nexit 0\n' >/etc/rc.local || return 1
  fi
  # generator 只認可執行的 /etc/rc.local
  chmod 0755 /etc/rc.local || return 1

  # 沒有 init（容器、chroot）時沒有 systemd 可通知，檔備好就夠了
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    # 讓 generator 重跑，把 rc-local 掛進開機目標；內建 unit 沒有 [Install]，不能用 systemctl enable
    systemctl daemon-reload || return 1
    systemctl start rc-local || return 1
  fi
}

rclocal_install() {
  case "${OS_ID}" in
  alpine) _rclocal_install_alpine || return 1 ;;
  ubuntu) _rclocal_install_ubuntu || return 1 ;;
  *)
    echo "Error: rclocal 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac
}

rclocal_verify() {
  if [[ "${OS_ID}" == "alpine" ]]; then
    rc-update show default | grep -qw local || {
      echo "Error: local 服務未加入 default runlevel" >&2
      return 1
    }
    return 0
  fi

  [[ -x /etc/rc.local ]] || {
    echo "Error: /etc/rc.local 缺檔或沒有執行位元" >&2
    return 1
  }
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    systemctl is-active --quiet rc-local || {
      echo "Error: rc-local 服務沒有在執行" >&2
      return 1
    }
  fi
}
