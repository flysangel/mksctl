#!/bin/bash
# recipe.sh — podman 配方：裝 podman、skopeo、buildah、catatonit；寫容器執行環境的持久設定，有 init 時再即時套用
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt、apk 的安裝是冪等的
#       不用 kubic 來源：舊 ubuntu 腳本只在 ubuntu 20.04 以前才加 opensuse kubic 套件庫，
#       支援的 24.04 與 26.04 官方套件庫已有 podman（24.04 是 4.9.3、26.04 是 5.7.0，alpine 3.24 是 5.8），所以整段拿掉
#       舊版在有 init 時還會順手跑 disableSwap 與 ipv46。podman 不需要關 swap，那是 k8s 的需求，
#       所以這裡不再連動：要同樣的結果，另外安裝 disableSwap 與 ipv46（k8s 配方需要它們）
#       venue 是 vm：有 init 時會即時 modprobe 並改 sysctl，這些動的是 host kernel，容器層（privileged 的 lxc 也一樣）不能跑

# _podman_config — 寫持久設定：核心模組清單、inotify 與 conntrack 上限；alpine 另外要 rshared 掛載與 cgroups 服務
# 這些只是檔案與服務啟用，映像建置也要寫，開機後才生效；svc_enable 沒有 init 時只啟用不啟動
_podman_config() {
  mkdir -p /etc/modules-load.d /etc/sysctl.d || return 1
  printf 'br_netfilter\noverlay\nfuse\nip_tables\nip_conntrack\n' >/etc/modules-load.d/containers.conf || return 1
  # 多容器同時跑時預設值不夠
  cat >/etc/sysctl.d/podman.conf <<'CONF' || return 1
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 512
net.netfilter.nf_conntrack_max = 524288
CONF

  if [[ "${OS_ID}" == alpine ]]; then
    # 開機時把根掛載設成 rshared，容器的掛載傳遞才正常
    mkdir -p /etc/local.d || return 1
    printf 'mount --make-rshared /\n' >/etc/local.d/containers.start || return 1
    chmod a+x /etc/local.d/containers.start || return 1
    svc_enable cgroups || return 1
  fi
}

# _podman_apply_live — 即時載入模組並套用 sysctl；動的是 host kernel，只有 init 在跑時做
_podman_apply_live() {
  local mod

  # 精簡 kernel 可能缺其中幾個（例如 ip_tables 內建、ip_conntrack 已改名 nf_conntrack），逐一容錯
  while IFS= read -r mod; do
    modprobe "${mod}" 2>/dev/null || echo "Info: modprobe ${mod} 失敗（kernel 可能已內建或沒有），略過" >&2
  done </etc/modules-load.d/containers.conf
  # nf_conntrack_max 要 conntrack 模組載入後才存在，套用失敗不擋安裝
  sysctl -p /etc/sysctl.d/podman.conf || echo "Info: sysctl 即時套用失敗，重開機後生效" >&2
}

podman_install() {
  pkg_install podman skopeo buildah catatonit || return 1
  _podman_config || return 1

  # 沒有 init（容器、chroot、映像建置）時不動執行中的 kernel
  if [[ "${MKSCTL_INIT_RUNNING:-0}" != "1" ]]; then
    echo "Info: 沒有 init，持久設定已寫入，略過即時載入模組與 sysctl" >&2
    return 0
  fi
  _podman_apply_live || return 1
}

podman_verify() {
  command -v podman >/dev/null || {
    echo "Error: 找不到 podman" >&2
    return 1
  }
  podman --version >/dev/null || {
    echo "Error: podman --version 失敗" >&2
    return 1
  }
  # podman 沒有 daemon，沒有服務可驗；有 init（完整系統）時驗它真的跑得起來。
  # 巢狀容器（例如 kind 的測試層）的 overlay 儲存不能再疊 overlay，podman info 會失敗，不是配方的錯
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    podman info >/dev/null 2>&1 || {
      echo "Error: podman info 失敗" >&2
      return 1
    }
  fi
  # 持久設定不論有無 init 都要在
  [[ -f /etc/modules-load.d/containers.conf && -f /etc/sysctl.d/podman.conf ]] || {
    echo "Error: 缺少 /etc/modules-load.d/containers.conf 或 /etc/sysctl.d/podman.conf" >&2
    return 1
  }
  if [[ "${OS_ID}" == alpine && ! -x /etc/local.d/containers.start ]]; then
    echo "Error: 缺少 /etc/local.d/containers.start" >&2
    return 1
  fi
}
