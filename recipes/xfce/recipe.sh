#!/bin/bash
# recipe.sh — xfce 配方：xfce4 桌面加 xrdp，建遠端桌面使用者
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       使用者參數：MKSCTL_PARAM_XRDP_USER、MKSCTL_PARAM_XRDP_PASSWD；沒給時用舊腳本的預設值 desktop／desktop520
#       （預設值是方便值，已知限制）
#       附屬檔在 files/（執行時在 MKSCTL_RECIPE_DIR 底下）：etc/ 鏡射到 /etc，config/fcitx5 放進 XRDP 使用者的 ~/.config/fcitx5
#       與舊版不同的地方：
#       - 核心套件仍用 --no-install-recommends 直接呼叫 apt-get，因為 pkg_install 不能帶旗標
#       - 不做 apt-get upgrade、autoremove 與刪 /var/lib/apt/lists：升級是平台層的事，
#         刪索引會讓同一次 install 的後續配方裝不了套件
#       - 設定檔一律先備份成 .bk（舊版只備份部分檔）
#       - 服務重啟只在 init 正在跑時做

_XFCE_PAM=/etc/pam.d/xrdp-sesman
_XFCE_PAM_LINE='session required pam_env.so readenv=1 user_readenv=0'

# 要藏出選單的項目（/usr/share/applications 底下的 .desktop）
_XFCE_HIDDEN=(vim debian-uxterm debian-xterm thunar thunar-bulk-rename thunar-settings
  xfce4-mail-reader xfce4-file-manager xfce4-about)

_xfce_user() { printf '%s' "${MKSCTL_PARAM_XRDP_USER:-desktop}"; }
_xfce_passwd() { printf '%s' "${MKSCTL_PARAM_XRDP_PASSWD:-desktop520}"; }

# _xfce_in_group <使用者> <群組> — 使用者在該群組裡才成功
# 先把輸出收進變數再比對：開著 pipefail 時，grep -q 提早結束會讓上游收到 SIGPIPE，整條管線誤判失敗
_xfce_in_group() {
  local groups
  groups="$(id -nG "$1")" || return 1
  [[ " ${groups} " == *" $2 "* ]]
}

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

# _xfce_packages — 安裝全部套件；核心層不裝建議套件，與舊版相同
_xfce_packages() {
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    -o DPkg::Lock::Timeout=300 \
    xserver-xorg xserver-xorg-legacy xorgxrdp xfce4 xinit || return 1
  pkg_install xfce4-goodies || return 1
  pkg_install firefox mousepad xfce4-terminal || return 1
  pkg_install xrdp xtightvncviewer dbus-x11 \
    fcitx5 fcitx5-chewing fcitx5-chinese-addons numlockx \
    fonts-wqy-zenhei language-pack-zh-hant language-pack-gnome-zh-hant \
    zenity xterm || return 1
}

# _xfce_harden_xrdp — 三個修補，都能重跑：ssl-cert 群組、Xwrapper 放行任意使用者、PAM 載入環境變數
_xfce_harden_xrdp() {
  # xrdp 要讀 SSL 憑證私鑰
  if ! _xfce_in_group xrdp ssl-cert; then
    adduser xrdp ssl-cert || return 1
  fi

  # Xwrapper 放行任意使用者啟動 X server（xrdp 必要）；檔不存在就直接建立
  if [[ -f /etc/X11/Xwrapper.config ]]; then
    _xfce_backup /etc/X11/Xwrapper.config || return 1
    sed -i 's/allowed_users=console/allowed_users=anybody/' /etc/X11/Xwrapper.config || return 1
  else
    echo 'allowed_users=anybody' >/etc/X11/Xwrapper.config || return 1
  fi

  # 為 xrdp-sesman 補上 pam_env（登入時載入使用者環境變數）；整行比對，已有就不再插
  if [[ -f "${_XFCE_PAM}" ]] && ! grep -Fxq "${_XFCE_PAM_LINE}" "${_XFCE_PAM}"; then
    _xfce_backup "${_XFCE_PAM}" || return 1
    sed -i "1 a ${_XFCE_PAM_LINE}" "${_XFCE_PAM}" || return 1
  fi
}

# _xfce_startwm — 自寫最小的 startwm.sh（不改發行版的檔、不抓外部範例）
# 用 bash 承載：dash 對 session leader 生命週期的處理不同，曾害桌面約 20 秒就掉線
_xfce_startwm() {
  cat >/etc/xrdp/startwm.sh <<'WM' || return 1
#!/bin/bash
if test -r /etc/profile; then . /etc/profile; fi
# 載入系統 locale，避免 RDP session 的 LANG 掉到 C 害 CJK 字型與 fcitx 出問題
if test -r /etc/default/locale; then . /etc/default/locale; export LANG LANGUAGE; fi
exec startxfce4
WM
  chmod 755 /etc/xrdp/startwm.sh || return 1
}

# _xfce_hide_launchers — 把不要的選單項目改名成 .desktop.bk 藏起來，不刪除
_xfce_hide_launchers() {
  local app f
  for app in "${_XFCE_HIDDEN[@]}"; do
    f="/usr/share/applications/${app}.desktop"
    if [[ -f "${f}" && ! -f "${f}.bk" ]]; then
      mv "${f}" "${f}.bk" || return 1
    fi
  done
}

# _xfce_account <使用者> <密碼> — 建帳號（已有就沿用）、加進 adm 與 sudo、設密碼
_xfce_account() {
  local user="$1" passwd="$2"
  [[ "${user}" =~ ^[a-z_][a-z0-9_-]*$ ]] || {
    echo "Error: XRDP_USER 不是合法的帳號名稱：${user}" >&2
    return 1
  }
  if ! id -u "${user}" >/dev/null 2>&1; then
    useradd -m -s /bin/bash "${user}" || return 1
  fi
  usermod -a -G adm,sudo "${user}" || return 1
  printf '%s:%s\n' "${user}" "${passwd}" | chpasswd || return 1
}

# _xfce_desktop_config — files/etc 鏡射到 /etc（覆寫前先備份）
_xfce_desktop_config() {
  local rel
  while IFS= read -r rel; do
    _xfce_backup "/etc/${rel}" || return 1
    install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/etc/${rel}" "/etc/${rel}" || return 1
  done < <(cd "${MKSCTL_RECIPE_DIR}/etc" && find . -type f | sed 's|^\./||' | sort)
}

# _xfce_user_config <使用者> — fcitx5 設定放進使用者的 ~/.config，擁有者設成該使用者
_xfce_user_config() {
  local user="$1" home rel
  home="$(getent passwd "${user}" | cut -d: -f6)" || return 1
  while IFS= read -r rel; do
    install -D -o "${user}" -g "${user}" -m 0644 "${MKSCTL_RECIPE_DIR}/config/${rel}" \
      "${home}/.config/${rel}" || return 1
  done < <(cd "${MKSCTL_RECIPE_DIR}/config" && find . -type f | sed 's|^\./||' | sort)
  chown -R "${user}:${user}" "${home}" || return 1
  chmod 700 "${home}" || return 1
}

xfce_install() {
  local user passwd
  user="$(_xfce_user)"
  passwd="$(_xfce_passwd)"

  _xfce_apt_pin || return 1
  _xfce_packages || return 1
  _xfce_harden_xrdp || return 1
  _xfce_startwm || return 1
  _xfce_hide_launchers || return 1
  _xfce_account "${user}" "${passwd}" || return 1
  _xfce_desktop_config || return 1
  _xfce_user_config "${user}" || return 1

  apt-get clean -y || return 1

  svc_enable xrdp || return 1
  # svc_enable 對已在跑的服務不會重載；有 init 時重啟，新設定才會生效
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    systemctl restart xrdp || return 1
  fi
}

xfce_verify() {
  local user n app policy
  user="$(_xfce_user)"

  command -v xfce4-session >/dev/null || {
    echo "Error: 找不到 xfce4-session" >&2
    return 1
  }
  grep -q startxfce4 /etc/xrdp/startwm.sh || {
    echo "Error: /etc/xrdp/startwm.sh 沒有 startxfce4" >&2
    return 1
  }
  if grep -q xterm /etc/xrdp/startwm.sh; then
    echo "Error: /etc/xrdp/startwm.sh 不該出現 xterm（RDP 會只剩黑底終端機）" >&2
    return 1
  fi
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
  grep -q 'allowed_users=anybody' /etc/X11/Xwrapper.config || {
    echo "Error: /etc/X11/Xwrapper.config 沒有 allowed_users=anybody" >&2
    return 1
  }
  _xfce_in_group xrdp ssl-cert || {
    echo "Error: xrdp 不在 ssl-cert 群組" >&2
    return 1
  }
  # 冪等檢查：PAM 那一行剛好出現一次
  n="$(grep -Fxc "${_XFCE_PAM_LINE}" "${_XFCE_PAM}")" || n=0
  [[ "${n}" -eq 1 ]] || {
    echo "Error: ${_XFCE_PAM} 的 pam_env 行出現 ${n} 次，應該剛好 1 次" >&2
    return 1
  }
  id -u "${user}" >/dev/null 2>&1 || {
    echo "Error: 找不到使用者 ${user}" >&2
    return 1
  }
  _xfce_in_group "${user}" sudo || {
    echo "Error: ${user} 不在 sudo 群組" >&2
    return 1
  }
  [[ -f "$(getent passwd "${user}" | cut -d: -f6)/.config/fcitx5/profile" ]] || {
    echo "Error: 找不到 ${user} 的 fcitx5 設定" >&2
    return 1
  }
  # 改名藏起來的項目不該再以 .desktop 出現
  for app in "${_XFCE_HIDDEN[@]}"; do
    if [[ -f "/usr/share/applications/${app}.desktop" ]]; then
      echo "Error: ${app}.desktop 沒有被改名成 .bk" >&2
      return 1
    fi
  done

  # 有 init 時，xrdp 要在跑並聽 3389；剛重啟時可能還沒開始聽，等幾秒
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    systemctl is-active --quiet xrdp || {
      echo "Error: xrdp 沒在跑" >&2
      return 1
    }
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      if (exec 3<>/dev/tcp/127.0.0.1/3389) 2>/dev/null; then
        return 0
      fi
      sleep 1
    done
    echo "Error: 3389 沒有在聽" >&2
    return 1
  fi
}
