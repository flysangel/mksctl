#!/bin/bash
# recipe.sh — krew 配方：把 krew 裝進使用者家目錄（kubectl 外掛管理器）
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       krew 是每個使用者自裝（~/.krew），不是系統層。對象帳號照舊 provision：
#       root，加上 /home/ 底下每個目錄對應的帳號。已有 ~/.krew 的帳號略過。
#       PATH 靠 /etc/profile.d/krew-env.sh 接上（POSIX 語法，dash 也能 source）
#       krew 用 git 抓外掛索引（krew-index），所以要 git 套件
#       tarball 內的 krew-linux_<arch> 是啟動用執行檔，執行 install krew 才把 krew 真正裝進 ~/.krew

# _krew_install_for <帳號> <家目錄> <啟動執行檔> — 為單一帳號安裝 krew
_krew_install_for() {
  local user_name="$1" user_home="$2" boot="$3"

  [[ -d "${user_home}/.krew" ]] && return 0
  echo "Info: krew: 為 ${user_name} 安裝" >&2

  if [[ "${user_name}" == "root" ]]; then
    HOME="${user_home}" "${boot}" install krew || return 1
  else
    # su - 開新的登入 shell，HOME 與 PATH 都是該帳號自己的
    su - "${user_name}" -s /bin/bash -c "'${boot}' install krew" || return 1
  fi
}

# _krew_install_all <tarball> <暫存目錄> — 解壓啟動執行檔，為 root 與 /home/ 各帳號安裝
_krew_install_all() {
  local tarball="$1" tmp="$2"
  local found user_home

  # mktemp -d 預設 0700，其他帳號進不去；開成 0755，其他帳號才讀得到啟動執行檔
  chmod 0755 "${tmp}" || return 1
  tar -xzf "${tarball}" -C "${tmp}" || return 1
  found=("${tmp}"/krew-linux_*)
  [[ "${#found[@]}" -eq 1 && -f "${found[0]}" ]] || {
    echo "Error: tarball 內找不到唯一的 krew-linux_*" >&2
    return 1
  }

  _krew_install_for root /root "${found[0]}" || return 1
  for user_home in /home/*; do
    [[ -d "${user_home}" ]] || continue
    # /home 底下可能有不屬於任何帳號的孤兒目錄（lost+found、已刪帳號的殘留），跳過
    id -u "$(basename "${user_home}")" >/dev/null 2>&1 || continue
    _krew_install_for "$(basename "${user_home}")" "${user_home}" "${found[0]}" || return 1
  done
}

krew_install() {
  local tarball tmp rc

  pkg_install tar git || return 1
  tarball="$(fetch krew)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  _krew_install_all "${tarball}" "${tmp}" || rc=$?
  rm -rf "${tmp}"
  [[ "${rc}" -eq 0 ]] || return "${rc}"

  # 'KREW_ENV' 加引號：${HOME} 與 ${PATH} 留到使用者登入時才展開
  cat >/etc/profile.d/krew-env.sh <<'KREW_ENV' || return 1
if [ -d "${HOME}/.krew/bin" ]; then
  export PATH="${HOME}/.krew/bin:${PATH}"
fi
KREW_ENV
  [[ -d /root/.krew ]] || {
    echo "Error: /root/.krew 不存在" >&2
    return 1
  }
}

krew_verify() {
  # krew 裝在 ~/.krew，靠 profile.d 接 PATH；驗證時自己 source，不依賴呼叫端的 shell
  export HOME="${HOME:-/root}"
  # shellcheck source=/dev/null
  . /etc/profile.d/krew-env.sh || return 1
  command -v kubectl-krew >/dev/null || {
    echo "Error: 找不到 kubectl-krew" >&2
    return 1
  }
  kubectl-krew version >/dev/null || {
    echo "Error: kubectl-krew version 失敗" >&2
    return 1
  }
}
