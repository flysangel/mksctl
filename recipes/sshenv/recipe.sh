#!/bin/bash
# recipe.sh — sshenv 配方：SSH 客戶端與伺服端設定，並替每個使用者產生免密碼金鑰
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       設定寫成「缺才附加」，重跑不會重複；套用前先 sshd -t，語法錯就停止，不重啟 sshd
#       StrictHostKeyChecking no 與 ssh-rsa 相容設定是舊 provision 的刻意選擇（供自動化佈建與舊客戶端）
#       服務名稱：alpine 是 sshd，ubuntu 是 ssh

# _sshenv_append <檔案> <整行字串> — 檔內沒有該字串才附加
_sshenv_append() {
  local file="$1" line="$2"
  if ! grep -qF -- "${line}" "${file}"; then
    printf '%s\n' "${line}" >>"${file}" || return 1
  fi
}

# _sshenv_user_keys <使用者> <家目錄> — 尚無金鑰才產生，權限一律校正
_sshenv_user_keys() {
  local user="$1" home="$2"
  if [[ ! -f "${home}/.ssh/id_rsa" ]]; then
    mkdir -p "${home}/.ssh" || return 1
    chown "${user}" "${home}/.ssh" || return 1
    su - "${user}" -s /bin/bash -c "ssh-keygen -t rsa -P '' -f ~/.ssh/id_rsa >/dev/null" || return 1
    su - "${user}" -s /bin/bash -c "cat ~/.ssh/id_rsa.pub >> ~/.ssh/authorized_keys" || return 1
  fi

  # 無論金鑰是否既有，一律校正權限，修掉先前殘留的錯誤權限
  chmod 700 "${home}/.ssh" || return 1
  chmod 600 "${home}/.ssh/id_rsa" || return 1
  chmod 644 "${home}/.ssh/id_rsa.pub" || return 1
  touch "${home}/.ssh/authorized_keys" || return 1
  chmod 600 "${home}/.ssh/authorized_keys" || return 1
  chown -R "${user}:$(id -gn "${user}")" "${home}/.ssh" || return 1
}

# _sshenv_ubuntu_shell — ubuntu 專屬：不讓 PAM 擋 SSH 登入，非 SSH 的互動 shell 也載入 profile.d
_sshenv_ubuntu_shell() {
  # 停用 pam_limits 與 pam_nologin；pattern 只比對還沒註解的行，重跑不會再動
  sed -i '/pam_limits.so/s/^session/#session/' /etc/pam.d/sshd || return 1
  sed -i '/pam_nologin.so/s/^account/#account/' /etc/pam.d/sshd || return 1

  if ! grep -qF 'for i in /etc/profile.d/*.sh' /etc/bash.bashrc; then
    # 單引號內的 ${SSH_TTY} 與 ${i} 是字面字串，寫入 bashrc 後才由 shell 啟動時展開
    # shellcheck disable=SC2016
    printf '%s\n' '
# Load profile.d scripts for non-SSH interactive shells
if [ -z "${SSH_TTY:-}" ]; then
  for i in /etc/profile.d/*.sh; do
    if [ -r "${i}" ]; then
      . "${i}"
    fi
  done
  unset i
fi' >>/etc/bash.bashrc || return 1
  fi
}

sshenv_install() {
  local svc home user

  case "${OS_ID}" in
  alpine) svc=sshd ;;
  ubuntu) svc=ssh ;;
  *)
    echo "Error: sshenv 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac

  # ubuntu 的 openssh-server 套件相依 openssh-client，所以只裝 server。
  # 不經 pkgmap 的 openssh-client：它對到 apt 的 ssh，那在 24.04 是虛擬套件，跟 server 一起裝會找不到候選。
  # alpine 的 openssh-server 不一定帶客戶端，要另外裝。
  pkg_install openssh-server || return 1
  if [[ "${OS_ID}" == "alpine" ]]; then
    pkg_install openssh-client || return 1
  fi

  # 客戶端自動信任主機金鑰；附加在檔尾，會落在預設 ssh_config 的 Host * 區塊內，故沿用縮排
  _sshenv_append /etc/ssh/ssh_config '    StrictHostKeyChecking no' || return 1

  # 停用 root 直接登入；允許讀 ~/.ssh/environment；放行舊式 ssh-rsa 相容舊客戶端
  _sshenv_append /etc/ssh/sshd_config 'PermitRootLogin no' || return 1
  _sshenv_append /etc/ssh/sshd_config 'PermitUserEnvironment yes' || return 1
  _sshenv_append /etc/ssh/sshd_config 'HostKeyAlgorithms +ssh-rsa' || return 1
  _sshenv_append /etc/ssh/sshd_config 'PubkeyAcceptedKeyTypes +ssh-rsa' || return 1

  # 缺哪種主機金鑰補哪種；已存在的不覆寫
  ssh-keygen -A || return 1

  if [[ "${OS_ID}" == "ubuntu" ]]; then
    _sshenv_ubuntu_shell || return 1
  fi

  for home in /home/*; do
    [[ -d "${home}" ]] || continue
    user="$(basename "${home}")"
    _sshenv_user_keys "${user}" "${home}" || return 1
  done

  # 設定寫錯會讓 sshd 重啟後起不來，先檢查再套用
  mkdir -p /run/sshd || return 1
  sshd -t || {
    echo "Error: sshd -t 檢查 /etc/ssh/sshd_config 失敗，不重啟 sshd" >&2
    return 1
  }

  svc_enable "${svc}" || return 1
  # svc_enable 對已在跑的服務不會重載；有 init 時重啟，新設定才會生效
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    if [[ "${OS_ID}" == "alpine" ]]; then
      rc-service "${svc}" restart || return 1
    else
      systemctl restart "${svc}" || return 1
    fi
  fi
}

sshenv_verify() {
  grep -q 'StrictHostKeyChecking no' /etc/ssh/ssh_config || {
    echo "Error: /etc/ssh/ssh_config 沒有 StrictHostKeyChecking no" >&2
    return 1
  }
  grep -q 'PermitRootLogin no' /etc/ssh/sshd_config || {
    echo "Error: /etc/ssh/sshd_config 沒有 PermitRootLogin no" >&2
    return 1
  }
  sshd -t || {
    echo "Error: sshd -t 失敗" >&2
    return 1
  }
  if [[ "${MKSCTL_INIT_RUNNING:-0}" == "1" ]]; then
    # alpine 沒有 ss，直接讀核心的 TCP 表：本地埠 0016（22）、狀態 0A（LISTEN）
    grep -qE ':0016 [0-9A-F:]+ 0A ' /proc/net/tcp /proc/net/tcp6 || {
      echo "Error: 沒有程序在聽 22 埠" >&2
      return 1
    }
  fi
}
