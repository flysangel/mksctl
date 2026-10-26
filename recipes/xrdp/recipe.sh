#!/bin/bash
# recipe.sh — xrdp 配方：X server、xrdp、輸入法與字型，建遠端桌面使用者；桌面本身由 xfce、lxqt 配方裝
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       使用者參數：MKSCTL_PARAM_XRDP_USER、MKSCTL_PARAM_XRDP_PASSWD；沒給時用舊腳本的預設值 desktop／desktop520
#       （預設值是方便值，已知限制）
#       附屬檔在 files/（執行時在 MKSCTL_RECIPE_DIR 底下）：config/fcitx5 放進 XRDP 使用者的 ~/.config/fcitx5
#       session 的最後一行不在這裡：startwm.sh 會 source /etc/xrdp/session.sh，由 xfce、lxqt 配方各寫自己那一行
#       與舊版不同的地方：
#       - X server 層仍用 --no-install-recommends 直接呼叫 apt-get，因為 pkg_install 不能帶旗標
#       - 不做 apt-get upgrade、autoremove 與刪 /var/lib/apt/lists：升級是平台層的事，
#         刪索引會讓同一次 install 的後續配方裝不了套件
#       - 服務重啟只在 init 正在跑時做
#       - 只動本配方寫入的 ~/.config/fcitx5；既有帳號的家目錄權限與其他檔不碰，chmod 700 只用在這次新建的帳號

_XRDP_PAM=/etc/pam.d/xrdp-sesman
_XRDP_PAM_LINE='session required pam_env.so readenv=1 user_readenv=0'

_xrdp_user() { printf '%s' "${MKSCTL_PARAM_XRDP_USER:-desktop}"; }
_xrdp_passwd() { printf '%s' "${MKSCTL_PARAM_XRDP_PASSWD:-desktop520}"; }

# _xrdp_in_group <使用者> <群組> — 使用者在該群組裡才成功
# 先把輸出收進變數再比對：開著 pipefail 時，grep -q 提早結束會讓上游收到 SIGPIPE，整條管線誤判失敗
_xrdp_in_group() {
  local groups
  groups="$(id -nG "$1")" || return 1
  [[ " ${groups} " == *" $2 "* ]]
}

# _xrdp_backup <路徑> — 檔存在且還沒有備份時，備份成 .bk；已有備份就不蓋掉更早的原始版本
_xrdp_backup() {
  if [[ -f "$1" && ! -f "$1.bk" ]]; then
    cp "$1" "$1.bk" || return 1
  fi
}

# _xrdp_packages — 安裝全部套件；X server 層不裝建議套件，與舊版相同
_xrdp_packages() {
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    -o DPkg::Lock::Timeout=300 \
    xserver-xorg xserver-xorg-legacy xorgxrdp xinit || return 1
  pkg_install xrdp xtightvncviewer dbus-x11 \
    fcitx5 fcitx5-chewing fcitx5-chinese-addons numlockx \
    fonts-wqy-zenhei language-pack-zh-hant language-pack-gnome-zh-hant \
    zenity xterm || return 1
}

# _xrdp_harden — 三個修補，都能重跑：ssl-cert 群組、Xwrapper 放行任意使用者、PAM 載入環境變數
_xrdp_harden() {
  # xrdp 要讀 SSL 憑證私鑰
  if ! _xrdp_in_group xrdp ssl-cert; then
    adduser xrdp ssl-cert || return 1
  fi

  # Xwrapper 放行任意使用者啟動 X server（xrdp 必要）；檔不存在就直接建立
  if [[ -f /etc/X11/Xwrapper.config ]]; then
    _xrdp_backup /etc/X11/Xwrapper.config || return 1
    sed -i 's/allowed_users=console/allowed_users=anybody/' /etc/X11/Xwrapper.config || return 1
  else
    echo 'allowed_users=anybody' >/etc/X11/Xwrapper.config || return 1
  fi

  # 為 xrdp-sesman 補上 pam_env（登入時載入使用者環境變數）；整行比對，已有就不再插
  if [[ -f "${_XRDP_PAM}" ]] && ! grep -Fxq "${_XRDP_PAM_LINE}" "${_XRDP_PAM}"; then
    _xrdp_backup "${_XRDP_PAM}" || return 1
    sed -i "1 a ${_XRDP_PAM_LINE}" "${_XRDP_PAM}" || return 1
  fi
}

# _xrdp_startwm — 自寫最小的 startwm.sh（不改發行版的檔、不抓外部範例）
# 用 bash 承載：dash 對 session leader 生命週期的處理不同，曾害桌面約 20 秒就掉線
# 桌面那一行放在 /etc/xrdp/session.sh，由桌面配方寫；用 source 才能讓裡面的 exec 取代本 shell
_xrdp_startwm() {
  cat >/etc/xrdp/startwm.sh <<'WM' || return 1
#!/bin/bash
if test -r /etc/profile; then . /etc/profile; fi
# 載入系統 locale，避免 RDP session 的 LANG 掉到 C 害 CJK 字型與 fcitx 出問題
if test -r /etc/default/locale; then . /etc/default/locale; export LANG LANGUAGE; fi
# 桌面配方（xfce、lxqt）寫的 session 指令
if test -r /etc/xrdp/session.sh; then . /etc/xrdp/session.sh; fi
WM
  chmod 755 /etc/xrdp/startwm.sh || return 1
}

# _xrdp_account <使用者> <密碼> — 建帳號（已有就沿用）、加進 adm 與 sudo、設密碼
# 拒絕 root 與 UID 小於 1000 的既有帳號：這個配方會改它的密碼與群組
# 成功時，帳號是這次新建的就印 new，否則印 existing（給 _xrdp_user_config 判斷要不要收緊家目錄權限）
_xrdp_account() {
  local user="$1" passwd="$2" uid
  [[ "${user}" =~ ^[a-z_][a-z0-9_-]*$ ]] || {
    echo "Error: XRDP_USER 不是合法的帳號名稱：${user}" >&2
    return 1
  }
  if uid="$(id -u "${user}" 2>/dev/null)"; then
    if [[ "${user}" == "root" || "${uid}" -lt 1000 ]]; then
      echo "Error: XRDP_USER 不能是 root 或系統帳號（UID 小於 1000）：${user}" >&2
      return 1
    fi
    usermod -a -G adm,sudo "${user}" || return 1
    printf '%s:%s\n' "${user}" "${passwd}" | chpasswd || return 1
    echo existing
    return 0
  fi
  useradd -m -s /bin/bash "${user}" || return 1
  usermod -a -G adm,sudo "${user}" || return 1
  printf '%s:%s\n' "${user}" "${passwd}" | chpasswd || return 1
  echo new
}

# _xrdp_user_config <使用者> <new|existing> — fcitx5 設定放進 ~/.config，只改本配方寫入的目錄與檔
# 新建帳號才把家目錄收緊成 700
_xrdp_user_config() {
  local user="$1" state="$2" home group rel dir
  home="$(getent passwd "${user}" | cut -d: -f6)" || return 1
  group="$(id -gn "${user}")" || return 1
  # install -D 建出來的上層目錄會是 root 的，所以逐層自己建
  if [[ ! -d "${home}/.config" ]]; then
    install -d -o "${user}" -g "${group}" -m 0755 "${home}/.config" || return 1
  fi
  while IFS= read -r rel; do
    dir="$(dirname "${rel}")"
    if [[ ! -d "${home}/.config/${dir}" ]]; then
      install -d -o "${user}" -g "${group}" -m 0755 "${home}/.config/${dir}" || return 1
    fi
    install -o "${user}" -g "${group}" -m 0644 "${MKSCTL_RECIPE_DIR}/config/${rel}" \
      "${home}/.config/${rel}" || return 1
  done < <(cd "${MKSCTL_RECIPE_DIR}/config" && find . -type f | sed 's|^\./||' | sort)
  if [[ "${state}" == "new" ]]; then
    chmod 700 "${home}" || return 1
  fi
}

xrdp_install() {
  local user passwd state
  user="$(_xrdp_user)"
  passwd="$(_xrdp_passwd)"

  _xrdp_packages || return 1
  _xrdp_harden || return 1
  _xrdp_startwm || return 1
  state="$(_xrdp_account "${user}" "${passwd}")" || return 1
  _xrdp_user_config "${user}" "${state}" || return 1

  apt-get clean -y || return 1

  svc_enable xrdp || return 1
  # svc_enable 對已在跑的服務不會重載；有 init 時重啟，新設定才會生效
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    systemctl restart xrdp || return 1
  fi
}

xrdp_verify() {
  local user n

  user="$(_xrdp_user)"

  command -v xrdp >/dev/null || {
    echo "Error: 找不到 xrdp" >&2
    return 1
  }
  if grep -q xterm /etc/xrdp/startwm.sh; then
    echo "Error: /etc/xrdp/startwm.sh 不該出現 xterm（RDP 會只剩黑底終端機）" >&2
    return 1
  fi
  grep -q 'allowed_users=anybody' /etc/X11/Xwrapper.config || {
    echo "Error: /etc/X11/Xwrapper.config 沒有 allowed_users=anybody" >&2
    return 1
  }
  _xrdp_in_group xrdp ssl-cert || {
    echo "Error: xrdp 不在 ssl-cert 群組" >&2
    return 1
  }
  # 冪等檢查：PAM 那一行剛好出現一次
  n="$(grep -Fxc "${_XRDP_PAM_LINE}" "${_XRDP_PAM}")" || n=0
  [[ "${n}" -eq 1 ]] || {
    echo "Error: ${_XRDP_PAM} 的 pam_env 行出現 ${n} 次，應該剛好 1 次" >&2
    return 1
  }
  id -u "${user}" >/dev/null 2>&1 || {
    echo "Error: 找不到使用者 ${user}" >&2
    return 1
  }
  _xrdp_in_group "${user}" sudo || {
    echo "Error: ${user} 不在 sudo 群組" >&2
    return 1
  }
  [[ -f "$(getent passwd "${user}" | cut -d: -f6)/.config/fcitx5/profile" ]] || {
    echo "Error: 找不到 ${user} 的 fcitx5 設定" >&2
    return 1
  }

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
