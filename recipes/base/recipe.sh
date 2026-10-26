#!/bin/bash
# recipe.sh — base 配方：所有其他配方 needs 的基礎套件與系統設定
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       兩個發行版的套件名稱差異在 pkgmap.yaml；指令本身不同的步驟在 _base_install_<發行版>
#       不清空 /mks，也不用標記檔；MKS 儀表板改由 mksctl dashboard 提供

# _base_motd_profile — 裝登入畫面腳本（兩個發行版共用同一份）
_base_motd_profile() {
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/profile.d/motd-env.sh" \
    /etc/profile.d/motd-env.sh || return 1
}

_base_install_alpine() {
  # 登入訊息只留 motd-env.sh 那一份
  : >/etc/motd || return 1

  # DNS：不讓 udhcpc 蓋掉 resolv.conf，固定用下面兩台
  mkdir -p /etc/udhcpc || return 1
  printf 'RESOLV_CONF="NO"\n' >/etc/udhcpc/udhcpc.conf || return 1
  printf 'nameserver 168.95.1.1\nnameserver 8.8.8.8\n' >/etc/resolv.conf || return 1
  # community 與 edge 套件庫由 platforms/apk.yaml 與 alpine-<版本>.yaml 的 repositories 寫入

  pkg_install "${_BASE_PACKAGES[@]}" || return 1

  # 計畫 3：MKS 儀表板（inittab、/mks/bin、/mks/dialog）改由 mksctl dashboard 提供

  # open-vm-tools 在非 VMware 的 hypervisor 上會啟動失敗，容許失敗
  svc_enable open-vm-tools || echo "Warning: open-vm-tools 啟動失敗（非 VMware 環境屬正常）" >&2
  svc_enable qemu-guest-agent || return 1
}

_base_install_ubuntu() {
  local f

  # 更新套件期間以非互動方式重啟服務
  if [[ -f /etc/needrestart/needrestart.conf ]]; then
    if [[ ! -f /etc/needrestart/needrestart.conf.bk ]]; then
      cp /etc/needrestart/needrestart.conf /etc/needrestart/needrestart.conf.bk || return 1
    fi
    sed -i "s/#\$nrconf{restart} = 'i';/\$nrconf{restart} = 'l';/" /etc/needrestart/needrestart.conf || return 1
  fi

  # 關掉 motd-news 與動態 motd，登入訊息只留 motd-env.sh 那一份
  if [[ -f /etc/default/motd-news ]]; then
    if [[ ! -f /etc/default/motd-news.bk ]]; then
      cp /etc/default/motd-news /etc/default/motd-news.bk || return 1
    fi
    sed -i 's|ENABLED=1|ENABLED=0|g' /etc/default/motd-news || return 1
  fi
  for f in /etc/update-motd.d/*; do
    [[ -f "${f}" ]] || continue
    chmod -x "${f}" || return 1
  done

  # rsyslog 與 inetutils-syslogd 互相衝突：已有 inetutils-syslogd 就不裝 rsyslog
  # 套件清單裡的 rsyslog 在這裡單獨處理，其餘照單安裝
  if dpkg-query -W -f='${Status}' inetutils-syslogd 2>/dev/null | grep -q '^install ok installed$'; then
    local pkgs=()
    for f in "${_BASE_PACKAGES[@]}"; do
      [[ "${f}" == "rsyslog" ]] || pkgs+=("${f}")
    done
    pkg_install "${pkgs[@]}" || return 1
  else
    pkg_install "${_BASE_PACKAGES[@]}" || return 1
  fi

  # 計畫 3：MKS 儀表板（mks.service、/mks/bin、/mks/dialog、停用 getty@tty1）改由 mksctl dashboard 提供

  svc_enable qemu-guest-agent || return 1
}

base_install() {
  # 邏輯名稱，與 recipe.yaml 的 packages 一致；實際名稱由 pkgmap.yaml 轉換
  _BASE_PACKAGES=(
    sudo bash bash-completion openssh-client sshpass
    jq yq nano tree wget curl acl dialog git shellcheck
    p7zip e2fsprogs grep gettext
    inotify-tools netcat bind-tools
    util-linux coreutils findutils binutils iputils procps
    tini
    software-properties-common rsyslog net-tools iproute2 iftop traceroute
    open-vm-tools open-vm-tools-guestinfo qemu-guest-agent
  )

  case "${OS_ID}" in
  alpine) _base_install_alpine || return 1 ;;
  ubuntu) _base_install_ubuntu || return 1 ;;
  *)
    echo "Error: base 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac

  _base_motd_profile || return 1
}

base_verify() {
  local cmd
  for cmd in bash curl jq git sudo envsubst tini sshpass dialog inotifywait vmtoolsd shellcheck; do
    command -v "${cmd}" >/dev/null || {
      echo "Error: 找不到 ${cmd}" >&2
      return 1
    }
  done

  [[ -f /etc/profile.d/motd-env.sh ]] || {
    echo "Error: 找不到 /etc/profile.d/motd-env.sh" >&2
    return 1
  }

  if [[ "${OS_ID}" == "alpine" ]]; then
    grep -q '^nameserver 168.95.1.1$' /etc/resolv.conf || {
      echo "Error: /etc/resolv.conf 沒有 nameserver 168.95.1.1" >&2
      return 1
    }
  fi

  # init 在跑時，qemu guest agent 必須是啟動狀態
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    case "${OS_ID}" in
    alpine) rc-service qemu-guest-agent status >/dev/null ;;
    *) systemctl is-active --quiet qemu-guest-agent ;;
    esac || {
      echo "Error: qemu-guest-agent 沒有在跑" >&2
      return 1
    }
  fi
}
