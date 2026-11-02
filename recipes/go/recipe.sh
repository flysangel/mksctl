#!/bin/bash
# recipe.sh — go 配方：從 go.dev 取官方 tarball，裝到 /usr/local/go，並設全系統 PATH
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       版本與雜湊都由 go.dev/dl 的 JSON 提供（latest 與 checksum 的 provider: godev），不釘版本
#       requires: apt：舊 provision 只有 ubuntu 版，alpine 不支援
#       PATH 設定檔 files/etc/profile.d/go-env.sh 由 MKSCTL_RECIPE_DIR 取得

# _go_extract_swap <tarball> <暫存目錄> — 解壓到暫存目錄，成功後才換掉 /usr/local/go
# 先解壓再換：解壓失敗時不動既有安裝。舊樹移開保存，每次升級各帶時間戳備份
_go_extract_swap() {
  local tarball="$1" tmp="$2"

  tar -xzf "${tarball}" -C "${tmp}" || return 1
  [[ -x "${tmp}/go/bin/go" ]] || {
    echo "Error: tarball 內找不到 go/bin/go" >&2
    return 1
  }
  if [[ -d /usr/local/go ]]; then
    mv /usr/local/go "/usr/local/go.bk-$(date +%Y%m%d-%H%M%S)" || return 1
  fi
  mv "${tmp}/go" /usr/local/go || return 1
}

go_install() {
  local tarball tmp rc

  # apt 裝的舊版 Go 與 /usr/local/go 會在 PATH 上衝突，先移除
  if dpkg -s golang-go >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get purge -y golang-go golang || return 1
  fi

  tarball="$(fetch go)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  _go_extract_swap "${tarball}" "${tmp}" || rc=$?
  rm -rf "${tmp}"
  [[ "${rc}" -eq 0 ]] || return "${rc}"

  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/profile.d/go-env.sh" \
    /etc/profile.d/go-env.sh || return 1
}

go_verify() {
  # go 不在預設 PATH，驗 go-env.sh 有把 PATH 接對（避免非登入 shell 假紅）
  [[ -f /etc/profile.d/go-env.sh ]] || {
    echo "Error: 找不到 /etc/profile.d/go-env.sh" >&2
    return 1
  }
  # shellcheck source=/dev/null
  . /etc/profile.d/go-env.sh || return 1
  command -v go >/dev/null || {
    echo "Error: 找不到 go" >&2
    return 1
  }
  go version >/dev/null || {
    echo "Error: go version 失敗" >&2
    return 1
  }
}
