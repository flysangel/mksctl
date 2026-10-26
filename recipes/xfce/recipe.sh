#!/bin/bash
# recipe.sh — xfce 配方：xfce4 桌面；帳號、xrdp 與輸入法設定在 xrdp 配方（needs）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       附屬檔在 files/（執行時在 MKSCTL_RECIPE_DIR 底下）：etc/ 鏡射到 /etc
#       桌面 session 的指令寫在 /etc/xrdp/session.sh，由 xrdp 配方的 startwm.sh source
#       與舊版不同的地方：
#       - 核心套件仍用 --no-install-recommends 直接呼叫 apt-get，因為 pkg_install 不能帶旗標
#       - 設定檔一律先備份成 .bk（舊版只備份部分檔）

# 要藏出選單的項目（/usr/share/applications 底下的 .desktop）
_XFCE_HIDDEN=(vim debian-uxterm debian-xterm thunar thunar-bulk-rename thunar-settings
  xfce4-mail-reader xfce4-file-manager xfce4-about)

# _xfce_backup <路徑> — 檔存在且還沒有備份時，備份成 .bk；已有備份就不蓋掉更早的原始版本
_xfce_backup() {
  if [[ -f "$1" && ! -f "$1.bk" ]]; then
    cp "$1" "$1.bk" || return 1
  fi
}

# _xfce_apt_pin — 讓 Mozilla 來源的套件優先，firefox 才不會被 ubuntu 的 snap 轉接套件取代
# 必須在 pkg_install firefox 之前寫好
_xfce_apt_pin() {
  # 清掉舊版腳本殘留的 mozilla.list 與 mozillateam PPA 設定，避免來源重複
  rm -f /etc/apt/sources.list.d/mozilla.list /etc/apt/sources.list.d/mozillateam.list \
    /etc/apt/preferences.d/mozillateam /etc/apt/keyrings/mozillateam.gpg || return 1
  mkdir -p /etc/apt/preferences.d || return 1
  cat >/etc/apt/preferences.d/mozilla <<'PIN' || return 1
Package: *
Pin: origin packages.mozilla.org
Pin-Priority: 1000
PIN
}

# _xfce_packages — 安裝桌面套件；核心層不裝建議套件，與舊版相同
_xfce_packages() {
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    -o DPkg::Lock::Timeout=300 xfce4 || return 1
  pkg_install xfce4-goodies || return 1
  pkg_install firefox mousepad xfce4-terminal || return 1
}

# _xfce_session — 桌面 session 的指令；xrdp 的 startwm.sh 會 source 它
_xfce_session() {
  echo 'exec startxfce4' >/etc/xrdp/session.sh || return 1
}

# _xfce_hide_launchers — 把不要的選單項目改名成 .desktop.bk 藏起來，不刪除
# 已有 .bk 時，套件升級放回來的 .desktop 直接刪掉，保留最早的 .bk
_xfce_hide_launchers() {
  local app f
  for app in "${_XFCE_HIDDEN[@]}"; do
    f="/usr/share/applications/${app}.desktop"
    [[ -f "${f}" ]] || continue
    if [[ -f "${f}.bk" ]]; then
      rm -f "${f}" || return 1
    else
      mv "${f}" "${f}.bk" || return 1
    fi
  done
}

# _xfce_desktop_config — files/etc 鏡射到 /etc（覆寫前先備份）
_xfce_desktop_config() {
  local rel
  while IFS= read -r rel; do
    _xfce_backup "/etc/${rel}" || return 1
    install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/${rel}" "/etc/${rel}" || return 1
  done < <(cd "${MKSCTL_RECIPE_DIR}/etc" && find . -type f | sed 's|^\./||' | sort)
}

xfce_install() {
  _xfce_apt_pin || return 1
  _xfce_packages || return 1
  _xfce_session || return 1
  _xfce_hide_launchers || return 1
  _xfce_desktop_config || return 1
  apt-get clean -y || return 1
}

xfce_verify() {
  local app policy

  command -v xfce4-session >/dev/null || {
    echo "Error: 找不到 xfce4-session" >&2
    return 1
  }
  grep -q startxfce4 /etc/xrdp/session.sh || {
    echo "Error: /etc/xrdp/session.sh 沒有 startxfce4" >&2
    return 1
  }
  # firefox 要來自 Mozilla 的 apt 來源，不是 snap 轉接套件
  policy="$(apt-cache policy firefox)" || return 1
  [[ "${policy}" == *packages.mozilla.org* ]] || {
    echo "Error: firefox 不是來自 packages.mozilla.org" >&2
    return 1
  }
  # 轉接套件的版本字串帶 snap，例如 1:1snap1-0ubuntu5
  [[ "$(dpkg-query -W -f='${Version}' firefox)" != *snap* ]] || {
    echo "Error: firefox 是 snap 轉接套件" >&2
    return 1
  }
  grep -qx 'Pin-Priority: 1000' /etc/apt/preferences.d/mozilla || {
    echo "Error: 找不到 Mozilla 的 pin 檔" >&2
    return 1
  }
  # 改名藏起來的項目不該再以 .desktop 出現
  for app in "${_XFCE_HIDDEN[@]}"; do
    if [[ -f "/usr/share/applications/${app}.desktop" ]]; then
      echo "Error: ${app}.desktop 沒有被改名成 .bk" >&2
      return 1
    fi
  done
}
