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
  # 刻意覆寫 /etc/resolv.conf（沿用舊 base 行為）：固定 DNS，不依賴 DHCP 給的
  printf 'nameserver 168.95.1.1\nnameserver 8.8.8.8\n' >/etc/resolv.conf || return 1
  # community 與 edge 套件庫由 platforms/apk.yaml 與 alpine-<版本>.yaml 的 repositories 寫入

  pkg_install "${_BASE_PACKAGES[@]}" || return 1

  # 計畫 3：MKS 儀表板（inittab、/mks/bin、/mks/dialog）改由 mksctl dashboard 提供

  _base_guest_tools open-vm-tools open-vm-tools-guestinfo qemu-guest-agent || return 1
}

# _base_guest_tools <邏輯套件>... — VM 專屬工具：只在 init 在跑（真的 VM）時安裝並啟用
# 這幾個套件仍列在 recipe.yaml 的 packages，讓 preflight 查得到；但容器沒有 init，
# images 的 Dockerfile 會在容器裡裝 base，裝了只會變胖，所以沒有 init 就整段略過。
# 啟動失敗只警告：open-vm-tools 在非 VMware 會起不來，guest agent 沒有 virtio 通道也起不來。
_base_guest_tools() {
  local p
  if [[ "${MKSCTL_INIT_RUNNING:-0}" != "1" ]]; then
    echo "Info: 沒有 init，略過 VM 專屬工具" >&2
    return 0
  fi
  pkg_install "$@" || return 1
  for p in open-vm-tools qemu-guest-agent; do
    svc_enable "${p}" || echo "Info: ${p} 啟用或啟動失敗，略過" >&2
  done
}

_base_install_ubuntu() {
  local p

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
  for p in /etc/update-motd.d/*; do
    [[ -f "${p}" ]] || continue
    chmod -x "${p}" || return 1
  done

  # rsyslog 與 inetutils-syslogd 互相衝突：已有 inetutils-syslogd 就不裝 rsyslog
  # 套件清單裡的 rsyslog 在這裡單獨處理，其餘照單安裝
  if dpkg-query -W -f='${Status}' inetutils-syslogd 2>/dev/null | grep -q '^install ok installed$'; then
    local pkgs=()
    for p in "${_BASE_PACKAGES[@]}"; do
      [[ "${p}" == "rsyslog" ]] || pkgs+=("${p}")
    done
    pkg_install "${pkgs[@]}" || return 1
  else
    pkg_install "${_BASE_PACKAGES[@]}" || return 1
  fi

  # 計畫 3：MKS 儀表板（mks.service、/mks/bin、/mks/dialog、停用 getty@tty1）改由 mksctl dashboard 提供

  _base_guest_tools open-vm-tools qemu-guest-agent || return 1
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
  for cmd in bash curl jq git sudo envsubst tini sshpass dialog inotifywait shellcheck; do
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
}
