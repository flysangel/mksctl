#!/bin/bash
# recipe.sh — libreoffice 配方：下載官方 deb 套件組（主程式、繁中語言包、繁中說明包）並安裝
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       latest 讀 TDF 下載站的目錄清單取最大版本；版本沒變時不重跑
#       下載點是 NCHC 鏡像，雜湊來自 TDF 主站的 .sha256，見 recipe.yaml
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過
#       舊 provision 用 dpkg -i || true 再 apt-get install -f 補相依；
#       現在把全部 .deb 一次交給 apt-get install，相依一併解開，失敗就中止

# _libreoffice_install_debs <暫存目錄> <tarball>... — 解開 tarball 並用 apt 一次裝全部 .deb
_libreoffice_install_debs() {
  local tmp="$1"
  shift
  local tarball debs

  for tarball in "$@"; do
    tar -xzf "${tarball}" -C "${tmp}" || return 1
  done
  # 每個 tarball 展開成 LibreOffice_*_deb*/DEBS/*.deb
  debs=("${tmp}"/LibreOffice_*_deb*/DEBS/*.deb)
  [[ -f "${debs[0]}" ]] || {
    echo "Error: tarball 內找不到 DEBS/*.deb" >&2
    return 1
  }
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    -o DPkg::Lock::Timeout=300 "${debs[@]}" || return 1
}

# _libreoffice_link_bin — 把 soffice 連到 /usr/local/bin，提供 libreoffice 與 soffice 兩個命令
_libreoffice_link_bin() {
  local soffice

  soffice="$(find /opt -maxdepth 3 -path '*/libreoffice*/program/soffice' -type f 2>/dev/null | head -n 1)" || return 1
  [[ -n "${soffice}" ]] || {
    echo "Error: /opt 底下找不到 libreoffice*/program/soffice" >&2
    return 1
  }
  ln -sf "${soffice}" /usr/local/bin/libreoffice || return 1
  ln -sf "${soffice}" /usr/local/bin/soffice || return 1
}

libreoffice_install() {
  local main lang help tmp rc

  pkg_install tar || return 1
  main="$(fetch libreoffice)" || return 1
  lang="$(fetch libreoffice-langpack)" || return 1
  help="$(fetch libreoffice-helppack)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  _libreoffice_install_debs "${tmp}" "${main}" "${lang}" "${help}" || rc=$?
  rm -rf "${tmp}"
  [[ "${rc}" -eq 0 ]] || return "${rc}"
  _libreoffice_link_bin || return 1
}

libreoffice_verify() {
  local lo_path

  lo_path="$(command -v libreoffice)" || {
    echo "Error: 找不到 libreoffice" >&2
    return 1
  }
  [[ "$(readlink -f "${lo_path}")" != /snap/* ]] || {
    echo "Error: libreoffice 指向 snap（${lo_path}）" >&2
    return 1
  }
  command -v soffice >/dev/null || {
    echo "Error: 找不到 soffice" >&2
    return 1
  }
}
