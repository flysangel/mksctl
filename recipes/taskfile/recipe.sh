#!/bin/bash
# recipe.sh — taskfile 配方：取 Task 的 tarball，裝 task 進 bin 目錄，並寫 bash 補全
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       補全檔直接取自 tarball 內的 completion/bash/task.bash，不另外下載
#       補全寫 /etc/bash_completion.d/（bash 專用），不可放 /etc/profile.d/：
#       後者連 dash、busybox ash 都會 source，task.bash 是 bash 語法，
#       dash 解析失敗會中止整個 /etc/profile，曾害 xrdp 桌面啟動失敗

_taskfile_extract_install() {
  local tarball="$1" tmp="$2"

  tar -xzf "${tarball}" -C "${tmp}" task completion/bash/task.bash || return 1
  install_bin "${tmp}/task" task || return 1
  mkdir -p /etc/bash_completion.d || return 1
  install -m 0644 "${tmp}/completion/bash/task.bash" /etc/bash_completion.d/task || return 1
}

taskfile_install() {
  local tarball tmp rc

  tarball="$(fetch taskfile)" || return 1
  tmp="$(mktemp -d)" || return 1
  # 不用 trap：EXIT trap 屬於整個 shell，會蓋掉組合腳本的 trap
  rc=0
  _taskfile_extract_install "${tarball}" "${tmp}" || rc=$?
  rm -rf "${tmp}"
  [[ "${rc}" -eq 0 ]] || return "${rc}"

  # dash-safety：/etc/profile 必須能被 dash（/bin/sh）source
  sh -c '. /etc/profile' >/dev/null 2>&1 || {
    echo "Error: /etc/profile 無法被 sh source（bash 專用的檔是否誤入 /etc/profile.d/？）" >&2
    return 1
  }
}

taskfile_verify() {
  command -v task >/dev/null || {
    echo "Error: 找不到 task" >&2
    return 1
  }
  task --version >/dev/null || {
    echo "Error: task --version 失敗" >&2
    return 1
  }
  [[ -s /etc/bash_completion.d/task ]] || {
    echo "Error: /etc/bash_completion.d/task 不存在或是空的" >&2
    return 1
  }
}
