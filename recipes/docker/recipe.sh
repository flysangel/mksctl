#!/bin/bash
# recipe.sh — docker 配方：裝 Docker Engine 與 compose、buildx 外掛，啟用並啟動服務
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt、apk 的安裝是冪等的
#       不釘版本：ubuntu 取 Docker 官方 apt 來源的最新版，alpine 取 community 的最新版
#       ubuntu 的 apt 來源由 mksctl 依 recipe.yaml 的 apt_source 寫好，金鑰也由 mksctl 下載
#       requires: init：沒有 init（容器、chroot）時 mksctl 不會跑本配方，舊 alpine 腳本的 OpenRC 早退由此取代
#       兩個發行版的套件名稱差異在 pkgmap.yaml

docker_install() {
  pkg_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin || return 1
  svc_enable docker || return 1
  echo "Info: 免 sudo 使用 docker：把使用者加進 docker 群組（ubuntu 用 usermod -aG docker、alpine 用 addgroup <使用者> docker），再重新登入" >&2
}

docker_verify() {
  local attempt
  command -v docker >/dev/null || {
    echo "Error: 找不到 docker" >&2
    return 1
  }
  # dockerd 剛啟動需要幾秒才回應，最多等 30 秒
  for attempt in $(seq 1 30); do
    if docker info >/dev/null 2>&1; then
      return 0
    fi
    echo "Info: 等待 docker daemon（${attempt}/30）" >&2
    sleep 1
  done
  echo "Error: docker daemon 沒有回應（docker info 失敗）" >&2
  return 1
}
