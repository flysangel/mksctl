#!/bin/bash
# recipe.sh — vscode 配方：用 Microsoft 官方 apt 來源裝 Visual Studio Code，加繁中字型與各使用者設定
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       latest 只代表字型版本（讀 GitHub releases/latest 的轉址，不打 api.github.com），
#       字型雜湊用 GitHub 公布的資產 digest（github_digest）；舊腳本沒有雜湊驗證
#       apt 來源（deb822）與金鑰由 mksctl 依 recipe.yaml 的 apt_sources 處理，
#       為什麼金鑰放 .asc 而不是官方文件的 .gpg，見 recipe.yaml 的註解
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過
#       設定檔放 files/，由 MKSCTL_RECIPE_DIR 取得
#       刻意例外「下載不經 fetch」：擴充套件由 code --install-extension 從 VS Code Marketplace 下載，
#       屬應用程式自己的套件管理，跟 npm 同類；網路失敗時只警告，不中止整個配方

_VSCODE_FONT_DIR=/usr/local/share/fonts/lxgw-wenkai-tc

# _vscode_install_font <tarball> — 解壓字型到系統字型目錄並更新快取
_vscode_install_font() {
  local tarball="$1"

  mkdir -p "${_VSCODE_FONT_DIR}" || return 1
  tar -xzf "${tarball}" -C "${_VSCODE_FONT_DIR}" --strip-components=1 || return 1
  fc-cache -f || return 1
}

# _vscode_setup_user <帳號> — 落地設定檔並裝擴充套件；擁有者改回該帳號
_vscode_setup_user() {
  local user="$1"
  local home="/home/${user}"
  local ext

  install -D -o "${user}" -g "${user}" -m 0644 "${MKSCTL_RECIPE_DIR}/Code/User/settings.json" \
    "${home}/.config/Code/User/settings.json" || return 1
  install -D -o "${user}" -g "${user}" -m 0644 "${MKSCTL_RECIPE_DIR}/crossnote/style.less" \
    "${home}/.config/crossnote/style.less" || return 1
  install -D -o "${user}" -g "${user}" -m 0644 "${MKSCTL_RECIPE_DIR}/vscode/argv.json" \
    "${home}/.vscode/argv.json" || return 1
  # install -D 建立的上層目錄屬於 root，改回該帳號
  chown -R "${user}:${user}" "${home}/.config/Code" "${home}/.config/crossnote" "${home}/.vscode" || return 1

  while IFS= read -r ext; do
    [[ -n "${ext}" ]] || continue
    su - "${user}" -s /bin/bash -c 'code --install-extension "$1" --force' _ "${ext}" >/dev/null 2>&1 ||
      echo "Warn: ${user} 的擴充套件 ${ext} 安裝失敗，略過" >&2
  done <"${MKSCTL_RECIPE_DIR}/extensions.txt"
}

vscode_install() {
  local tarball user_home

  pkg_install code tar fontconfig libx11-xcb1 libdbus-1-3 libxkbfile1 || return 1

  tarball="$(fetch lxgw-wenkai-tc)" || return 1
  _vscode_install_font "${tarball}" || return 1

  for user_home in /home/*; do
    [[ -d "${user_home}" ]] || continue
    _vscode_setup_user "$(basename "${user_home}")" || return 1
  done
}

vscode_verify() {
  local fonts

  command -v code >/dev/null || {
    echo "Error: 找不到 code" >&2
    return 1
  }
  # 真的 Microsoft apt build，不是 snap 墊片
  dpkg -s code >/dev/null 2>&1 || {
    echo "Error: code 不是 apt 套件（可能是 snap 墊片）" >&2
    return 1
  }
  fonts="$(fc-list)" || return 1
  [[ "${fonts,,}" == *lxgw* ]] || {
    echo "Error: 中文字型（lxgw）未落地" >&2
    return 1
  }
}
