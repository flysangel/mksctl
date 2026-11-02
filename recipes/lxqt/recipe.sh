#!/bin/bash
# recipe.sh — lxqt 配方：lxqt 桌面；帳號、xrdp 與輸入法設定在 xrdp 配方（needs）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       附屬檔在 files/（執行時在 MKSCTL_RECIPE_DIR 底下）：etc/ 鏡射到 /etc
#       桌面 session 的指令寫在 /etc/xrdp/session.sh，由 xrdp 配方的 startwm.sh source
#       與舊版不同的地方：
#       - 核心套件仍用 --no-install-recommends 直接呼叫 apt-get，因為 pkg_install 不能帶旗標
#       - 設定檔一律先備份成 .bk（舊版只備份部分檔）

# 要藏出選單的項目（/usr/share/applications 底下的 .desktop）
_LXQT_HIDDEN=(qterminal_drop qterminal-drop x11vnc vim debian-uxterm debian-xterm)

# _lxqt_backup <路徑> — 檔存在且還沒有備份時，備份成 .bk；已有備份就不蓋掉更早的原始版本
_lxqt_backup() {
  if [[ -f "$1" && ! -f "$1.bk" ]]; then
    cp "$1" "$1.bk" || return 1
  fi
}

# _lxqt_packages — 安裝桌面套件；核心層不裝建議套件，與舊版相同
_lxqt_packages() {
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    -o DPkg::Lock::Timeout=300 lxqt-core || return 1
  pkg_install lxqt-theme-debian openbox || return 1
}

# _lxqt_session — 桌面 session 的指令；xrdp 的 startwm.sh 會 source 它
# LXQt 不像 startxfce4 自帶 session bus，所以用 dbus-run-session
_lxqt_session() {
  echo 'exec dbus-run-session startlxqt' >/etc/xrdp/session.sh || return 1
}

# _lxqt_hide_launchers — 把不要的選單項目改名成 .desktop.bk 藏起來，不刪除
# 已有 .bk 時，套件升級放回來的 .desktop 直接刪掉，保留最早的 .bk
_lxqt_hide_launchers() {
  local app f
  for app in "${_LXQT_HIDDEN[@]}"; do
    f="/usr/share/applications/${app}.desktop"
    [[ -f "${f}" ]] || continue
    if [[ -f "${f}.bk" ]]; then
      rm -f "${f}" || return 1
    else
      mv "${f}" "${f}.bk" || return 1
    fi
  done
}

# _lxqt_desktop_config — files/etc 鏡射到 /etc（覆寫前先備份）
_lxqt_desktop_config() {
  local rel
  while IFS= read -r rel; do
    _lxqt_backup "/etc/${rel}" || return 1
    install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/${rel}" "/etc/${rel}" || return 1
  done < <(cd "${MKSCTL_RECIPE_DIR}/etc" && find . -type f | sed 's|^\./||' | sort)
}

lxqt_install() {
  _lxqt_packages || return 1
  _lxqt_session || return 1
  _lxqt_hide_launchers || return 1
  _lxqt_desktop_config || return 1
  apt-get clean -y || return 1
}

lxqt_verify() {
  local app

  command -v lxqt-session >/dev/null || {
    echo "Error: 找不到 lxqt-session" >&2
    return 1
  }
  grep -q 'dbus-run-session startlxqt' /etc/xrdp/session.sh || {
    echo "Error: /etc/xrdp/session.sh 沒有 dbus-run-session startlxqt" >&2
    return 1
  }
  # 改名藏起來的項目不該再以 .desktop 出現
  for app in "${_LXQT_HIDDEN[@]}"; do
    if [[ -f "/usr/share/applications/${app}.desktop" ]]; then
      echo "Error: ${app}.desktop 沒有被改名成 .bk" >&2
      return 1
    fi
  done
}
