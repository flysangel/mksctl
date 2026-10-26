#!/bin/bash
# doctor.sh — 檢查本 repo 的執行環境：命令存在與種類、core.hooksPath 的來源、pre-push 的執行位元
# 註記：逐項容錯，捨棄 -e；一項沒過不中斷其餘項，讓使用者一次看到全部缺項
set -uo pipefail

fail=0

# 必要命令：need <命令> <種類說明> <種類樣式> <取樣指令...>；樣式比對取樣指令的輸出
need() {
  local cmd="$1"
  local kind="$2"
  local pattern="$3"
  local out

  shift 3
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Error: 找不到 ${cmd}（${kind}）" >&2
    fail=1
    return 0
  fi
  out="$("$@" 2>&1 || true)"
  if [[ ! "${out}" =~ ${pattern} ]]; then
    echo "Error: ${cmd} 不是 ${kind}" >&2
    fail=1
    return 0
  fi
  printf 'Info: %s 是 %s\n' "${cmd}" "${kind}"
}

# 實測層與發版用的工具：optional <命令> <哪個用途>；缺了只提示，task lint 與 task test 不受影響
optional() {
  local cmd="$1"
  local purpose="$2"

  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "Info: 找不到 ${cmd}，${purpose}跑不了；其他 task 不受影響"
  fi
}

# main
cd "$(dirname "${BASH_SOURCE[0]}")/.." || exit 1

# 種類樣式取自各命令的版本或說明輸出（go 1.27.3、task 3.54.0、shfmt 3.8.0（輸出不帶 v）、ShellCheck 0.11.0 實測）。
# go-task 的 --version 只印版本號，認不出種類，改看 --help 的開頭說明。
need go 'Go' '^go version go1\.' go version
need task 'go-task' 'Falls back to the "default" task' task --help
need shellcheck 'ShellCheck' 'ShellCheck' shellcheck --version
need shfmt 'shfmt v3' '^v?3\.' shfmt --version
need python3 'Python 3' '^Python 3\.' python3 --version
optional podman 'podman 層實測'
optional lxc-create 'LXC 層實測'
optional qemu-system-x86_64 'VM 層實測'
optional cloud-localds 'VM 層實測'
optional gh '建立遠端 repo 與開 PR'

# local 或 worktree 作用域的 core.hooksPath 會讓 git 不經過帳號層派工 hook，機密掃描靜默失效
scopes="$(git config --show-scope --get-all core.hooksPath || true)"
if [[ "${scopes}" =~ (^|$'\n')(local|worktree)$'\t' ]]; then
  echo "Error: core.hooksPath 有 local 或 worktree 來源，帳號層的機密掃描不會跑。修法：" >&2
  echo "  git config --local --unset core.hooksPath" >&2
  fail=1
fi

# core.hooksPath 沒有值，或指向的目錄沒有可執行的 pre-push，git 不出聲、直接放行，所有掃描都不跑。
#   --type=path 讓 ~ 開頭的值照 git 自己的方式展開。
hooks_dir="$(git config --type=path --get core.hooksPath || true)"
if [[ -z "${hooks_dir}" ]]; then
  echo "Error: core.hooksPath 沒有值，pre-push 不會被觸發。裝好帳號層派工 hook 後再跑一次" >&2
  fail=1
elif [[ ! -x "${hooks_dir}/pre-push" ]]; then
  echo "Error: ${hooks_dir}/pre-push 不存在或沒有執行位元，pre-push 不會被觸發" >&2
  fail=1
fi

# 沒有執行位元時，派工 hook 找不到可執行的 .githooks/pre-push，repo 層的掃描整道略過
if [[ ! -x .githooks/pre-push ]]; then
  echo "Error: .githooks/pre-push 沒有執行位元，repo 層的掃描不會跑。修法：" >&2
  echo "  chmod +x .githooks/pre-push，再 commit 權限變更" >&2
  fail=1
fi

if [[ "${fail}" -eq 0 ]]; then
  echo "Success: 執行環境齊全"
fi
exit "${fail}"
