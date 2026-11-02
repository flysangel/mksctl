#!/bin/bash
# recipe.sh — locale 配方：產生 en_US 與 zh_TW 語系，裝語系與鍵盤設定檔（只有 ubuntu）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       附屬檔在 files/（執行時 files/ 的內容在 MKSCTL_RECIPE_DIR 底下）：locale、keyboard 是 /etc/default 的內容，locale-env.sh 裝到 /etc/profile.d

# _locale_install_file <來源檔名> <目標路徑> — 目標已存在時先備份一次（已有備份就不覆蓋），再裝新檔
# 最小化映像可能沒有 /etc/default/locale 或 /etc/default/keyboard，所以先判存在才備份
_locale_install_file() {
  local name="$1" dest="$2"
  if [[ -f "${dest}" && ! -f "${dest}.bk" ]]; then
    cp "${dest}" "${dest}.bk" || return 1
  fi
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/${name}" "${dest}" || return 1
}

locale_install() {
  pkg_install locales || return 1

  locale-gen en_US.UTF-8 zh_TW.UTF-8 || return 1
  update-locale LANG=en_US.UTF-8 || return 1

  _locale_install_file locale /etc/default/locale || return 1
  _locale_install_file keyboard /etc/default/keyboard || return 1
  # 語系變數輸出到 profile.d，登入 shell 會自動載入
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/locale-env.sh" \
    /etc/profile.d/locale-env.sh || return 1
}

locale_verify() {
  locale -a | grep -qi 'zh_TW' || {
    echo "Error: zh_TW 語系未產生" >&2
    return 1
  }
  grep -q 'en_US.UTF-8' /etc/default/locale || {
    echo "Error: /etc/default/locale 沒有 en_US.UTF-8" >&2
    return 1
  }
  [[ -f /etc/profile.d/locale-env.sh ]] || {
    echo "Error: 找不到 /etc/profile.d/locale-env.sh" >&2
    return 1
  }
}
