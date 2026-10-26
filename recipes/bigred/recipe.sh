#!/bin/bash
# recipe.sh — bigred 配方：建立 bigred 管理帳號，加入管理員群組（alpine 是 wheel，ubuntu 是 sudo）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行，所以每一步都要能重跑
#       帳號、密碼與 UID 寫死是已知限制（舊 provision 沿用至今），不要改成參數
#       兩個發行版都先查 UID 有沒有被別的帳號占用：舊 alpine 版沒有這道檢查，這裡收斂成一致

_BIGRED_USER=bigred
_BIGRED_PASSWD=bigred
_BIGRED_UID=1001

# _bigred_create_alpine — 建立帳號；busybox adduser 的參數與 useradd 不同
_bigred_create_alpine() {
  adduser -s /bin/bash -h "/home/${_BIGRED_USER}" -G wheel -u "${_BIGRED_UID}" -D "${_BIGRED_USER}" || return 1
}

_bigred_create_ubuntu() {
  useradd -s /bin/bash -m -G sudo -u "${_BIGRED_UID}" "${_BIGRED_USER}" || return 1
}

bigred_install() {
  local admin_group occupant existing_uid

  pkg_install sudo bash || return 1

  case "${OS_ID}" in
  alpine) admin_group=wheel ;;
  ubuntu) admin_group=sudo ;;
  *)
    echo "Error: bigred 不支援 ${OS_ID}" >&2
    return 1
    ;;
  esac

  if id -u "${_BIGRED_USER}" >/dev/null 2>&1; then
    # 帳號已存在：UID 與預期相同才算已完成；不同就停止，不動別人的檔案歸屬
    existing_uid="$(id -u "${_BIGRED_USER}")" || return 1
    if [[ "${existing_uid}" != "${_BIGRED_UID}" ]]; then
      echo "Error: 帳號 ${_BIGRED_USER} 已存在，UID 是 ${existing_uid}，預期 ${_BIGRED_UID}" >&2
      return 1
    fi
  else
    # UID 被別的帳號占用時停止，避免搶走別人的 UID 造成檔案歸屬錯亂
    if occupant="$(getent passwd "${_BIGRED_UID}")"; then
      echo "Error: UID ${_BIGRED_UID} 已被 '${occupant%%:*}' 使用，不覆寫" >&2
      return 1
    fi
    "_bigred_create_${OS_ID}" || return 1
  fi

  # 帳號可能是別處建的，缺管理員群組就補上
  if ! id -nG "${_BIGRED_USER}" | grep -qw "${admin_group}"; then
    if [[ "${OS_ID}" == "alpine" ]]; then
      addgroup "${_BIGRED_USER}" "${admin_group}" || return 1
    else
      usermod -aG "${admin_group}" "${_BIGRED_USER}" || return 1
    fi
  fi

  printf '%s:%s\n' "${_BIGRED_USER}" "${_BIGRED_PASSWD}" | chpasswd || return 1

  chown -R "${_BIGRED_USER}:$(id -gn "${_BIGRED_USER}")" "/home/${_BIGRED_USER}" || return 1
  chmod 700 "/home/${_BIGRED_USER}" || return 1
}

bigred_verify() {
  local admin_group
  command -v sudo >/dev/null || {
    echo "Error: 找不到 sudo" >&2
    return 1
  }
  id -u "${_BIGRED_USER}" >/dev/null 2>&1 || {
    echo "Error: ${_BIGRED_USER} 帳號不存在" >&2
    return 1
  }
  case "${OS_ID}" in
  alpine) admin_group=wheel ;;
  ubuntu) admin_group=sudo ;;
  esac
  id -nG "${_BIGRED_USER}" | grep -qw "${admin_group}" || {
    echo "Error: ${_BIGRED_USER} 不在 ${admin_group} 群組" >&2
    return 1
  }
}
