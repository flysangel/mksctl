#!/bin/bash
# harness.sh — 三層實測 harness 共用的平台清單、場地判斷、三態判定與驗證紀錄
# 註記：函式庫不設 set 旗標，set -e 會污染呼叫端的 shell；失敗由各函式 return 1
#
# 呼叫端（tests/test_{pkg,lxc,vm}.sh）要先提供 run_one <平台層> <配方>，
# 實作在該層的測試機裡跑 install_twice_cmd 的內容，回傳 mksctl 的結束碼。
# 然後呼叫 run_recipe <venue> <配方>。
#
# 判定：mksctl install 在同一台機器連跑兩次，兩次都回 0 才是 [OK]。
#   第二次是為了證明重跑可行。回 3 是安裝前檢查有差異（stderr 有表），其他非 0 是壞掉；兩者都算 [FAIL]。
#   平台不支援的配方（mksctl list --platform 不含它）印 [SKIP]，不影響結束碼。

# SC2034：PODMAN_IMAGE、LXC_TEMPLATE、VM_URL 由 source 本檔的各層腳本讀取，shellcheck 看不到
# shellcheck disable=SC2034
MKSCTL_BIN_PATH="dist/mksctl"
PLATFORMS_CONF="tests/platforms.conf"
VERIFIED_CONF="tests/verified.conf"

# 平台表，由 load_platforms 填入，鍵是平台層
PLATFORM_LAYERS=()
declare -A PODMAN_IMAGE=()
declare -A LXC_TEMPLATE=()
declare -A VM_URL=()

# 給 trap 用，也是各層 run_one 登記待清資源的回傳通道：暫存路徑、背景行程 pid、lxc 容器名
CLEAN_PATHS=()
CLEAN_PIDS=()
CLEAN_LXC=()

# clean_path <路徑> — 登記結束時要 rm -rf 的路徑
clean_path() {
  CLEAN_PATHS+=("$1")
}

# clean_pid <pid> — 登記結束時要 kill 的背景行程
clean_pid() {
  CLEAN_PIDS+=("$1")
}

# clean_lxc <容器名> — 登記結束時要 stop 與 destroy 的容器
clean_lxc() {
  CLEAN_LXC+=("$1")
}

# cleanup — 清掉所有登記的資源，可重複呼叫；每個平台跑完與 EXIT、INT、TERM 都會呼叫
cleanup() {
  local item

  for item in ${CLEAN_PIDS[@]+"${CLEAN_PIDS[@]}"}; do
    kill "${item}" 2>/dev/null || true
  done
  for item in ${CLEAN_LXC[@]+"${CLEAN_LXC[@]}"}; do
    sudo lxc-stop -k -n "${item}" >/dev/null 2>&1 || true
    sudo lxc-destroy -f -n "${item}" >/dev/null 2>&1 || true
  done
  for item in ${CLEAN_PATHS[@]+"${CLEAN_PATHS[@]}"}; do
    rm -rf -- "${item}" 2>/dev/null || sudo -n rm -rf -- "${item}" 2>/dev/null || true
  done
  CLEAN_PIDS=()
  CLEAN_LXC=()
  CLEAN_PATHS=()
}

# trim <字串> — 去掉前後空白
trim() {
  local s="$1"

  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s\n' "${s}"
}

# load_platforms — 讀 tests/platforms.conf
load_platforms() {
  local line layer podman lxc vm

  [[ -f "${PLATFORMS_CONF}" ]] || {
    echo "Error: 讀不到 ${PLATFORMS_CONF}" >&2
    return 1
  }
  while IFS= read -r line; do
    line="$(trim "${line}")"
    case "${line}" in
    "" | "#"*) continue ;;
    esac
    IFS='|' read -r layer podman lxc vm <<<"${line}"
    layer="$(trim "${layer}")"
    PODMAN_IMAGE["${layer}"]="$(trim "${podman}")"
    LXC_TEMPLATE["${layer}"]="$(trim "${lxc}")"
    VM_URL["${layer}"]="$(trim "${vm}")"
    PLATFORM_LAYERS+=("${layer}")
  done <"${PLATFORMS_CONF}"
  ((${#PLATFORM_LAYERS[@]} > 0)) || {
    echo "Error: ${PLATFORMS_CONF} 沒有任何平台" >&2
    return 1
  }
}

# distro_of <平台層> — alpine-3.24 印 alpine，ubuntu-24.04 印 ubuntu
distro_of() {
  printf '%s\n' "${1%%-*}"
}

# boot_cmd <平台層> — 測試機裡先裝 bash（配方是 bash 腳本）與憑證（fetch 走 TLS）
boot_cmd() {
  case "$(distro_of "$1")" in
  alpine) printf '%s\n' 'apk add --no-cache bash ca-certificates >/dev/null 2>&1' ;;
  ubuntu) printf '%s\n' 'export DEBIAN_FRONTEND=noninteractive; apt-get update -qq && apt-get install -y -qq ca-certificates >/dev/null' ;;
  *)
    echo "Error: 不認得的平台層 $1" >&2
    return 1
    ;;
  esac
}

# install_twice_cmd <配方> — 印一段 POSIX sh：連跑兩次 install，任一次非 0 就用該結束碼離開
install_twice_cmd() {
  local tmpl

  # 單引號是刻意的：$n 與 $rc 要留到測試機裡才展開
  # shellcheck disable=SC2016
  tmpl='for n in 1 2; do echo "Info: 第 $n 次 install __NAME__"; /repo/dist/mksctl install __NAME__ || { rc=$?; echo "Error: 第 $n 次 install 結束碼 $rc" >&2; exit "$rc"; }; done'
  printf '%s\n' "${tmpl//__NAME__/"$1"}"
}

# recipe_venue <配方> — 印配方宣告的場地，找不到時 return 1
recipe_venue() {
  local name="$1" listing venue

  listing="$("${MKSCTL_BIN_PATH}" list --all)" || return 1
  venue="$(awk -v n="${name}" '$1 == n { print $2 }' <<<"${listing}")"
  [[ -n "${venue}" ]] || return 1
  printf '%s\n' "${venue}"
}

# supports_platform <配方> <平台層> — 清單含它 return 0，不含 return 1，list 本身失敗 return 2
supports_platform() {
  local name="$1" layer="$2" listing

  listing="$("${MKSCTL_BIN_PATH}" list --platform "${layer}")" || return 2
  if grep -qx -- "${name}" <<<"${listing}"; then
    return 0
  fi
  return 1
}

# record_verified <配方> <平台層> <venue> — 更新 tests/verified.conf 對應的一行
record_verified() {
  local name="$1" layer="$2" venue="$3"
  local hash today tmp dirty

  # 雜湊讀 git index：工作樹有未提交的改動時，紀錄對不上實際測過的內容
  dirty="$(git status --porcelain -- . ':!tests/verified.conf')" || return 1
  if [[ -n "${dirty}" ]]; then
    echo "[SKIP] 工作樹有未提交的改動，不寫 verified.conf"
    return 0
  fi

  hash="$(bash bin/verified-hash.sh "${name}")" || return 1
  today="$(date -u +%F)"
  tmp="$(mktemp)"
  {
    awk '/^#/' "${VERIFIED_CONF}"
    {
      awk -v k="${name} ${layer} ${venue}" '!/^#/ && NF && ($1 " " $2 " " $3) != k' "${VERIFIED_CONF}"
      printf '%s %s %s %s %s\n' "${name}" "${layer}" "${venue}" "${hash}" "${today}"
    } | LC_ALL=C sort
  } >"${tmp}"
  cat "${tmp}" >"${VERIFIED_CONF}"
  rm -f "${tmp}"
}

# run_recipe <venue> <配方> — 對 platforms.conf 的每個平台各跑一次，印三態與統計
run_recipe() {
  local venue="$1" name="$2"
  local actual pass=0 skip=0 fail=0
  local fails=()
  local layer rc sup logdir log catpid

  load_platforms || return 1
  actual="$(recipe_venue "${name}")" || {
    echo "Error: mksctl list --all 沒有配方 ${name}" >&2
    return 1
  }
  if [[ "${actual}" != "${venue}" ]]; then
    echo "Error: 配方 ${name} 的 venue 是 ${actual}，不是這一層的 ${venue}" >&2
    return 1
  fi

  trap cleanup EXIT
  trap 'cleanup; exit 130' INT
  trap 'cleanup; exit 143' TERM

  for layer in "${PLATFORM_LAYERS[@]}"; do
    sup=0
    supports_platform "${name}" "${layer}" || sup=$?
    if ((sup == 1)); then
      echo "[SKIP] ${name} ${layer}（平台不支援）"
      skip=$((skip + 1))
      continue
    elif ((sup != 0)); then
      echo "[FAIL] ${name} ${layer}（mksctl list --platform ${layer} 執行失敗）" >&2
      fails+=("${name}@${layer}")
      fail=$((fail + 1))
      continue
    fi

    echo "[....] ${name} ${layer}"
    logdir="$(mktemp -d)"
    clean_path "${logdir}"
    log="${logdir}/run.log"
    # 輸出走具名管線交給 cat 寫 log：測試機（尤其 lxc）拿到的是管線，不是 log 檔本身，
    # log 檔就不會變成 root 所有。用具名管線而非 | cat，是為了讓 run_one 留在本行程，
    # 它登記的待清資源 trap 才看得到。
    mkfifo "${logdir}/fifo"
    cat <"${logdir}/fifo" >"${log}" &
    catpid=$!
    rc=0
    run_one "${layer}" "${name}" >"${logdir}/fifo" 2>&1 </dev/null || rc=$?
    wait "${catpid}" || true

    if ((rc == 0)); then
      echo "[OK]   ${name} ${layer}"
      pass=$((pass + 1))
      case "${venue}" in
      lxc | vm) record_verified "${name}" "${layer}" "${venue}" || echo "Error: 更新 ${VERIFIED_CONF} 失敗" >&2 ;;
      esac
    else
      if ((rc == 3)); then
        echo "[FAIL] ${name} ${layer}（安裝前檢查有差異，結束碼 3）" >&2
        sed 's/^/    /' "${log}" >&2
      else
        echo "[FAIL] ${name} ${layer}（結束碼 ${rc}）" >&2
        tail -n 40 "${log}" | sed 's/^/    /' >&2
      fi
      fails+=("${name}@${layer}")
      fail=$((fail + 1))
    fi
    cleanup
  done

  echo "---------------------------------------"
  echo "[OK] ${pass}  [SKIP] ${skip}  [FAIL] ${fail}"
  if ((fail > 0)); then
    printf '失敗清單：%s\n' "${fails[*]}" >&2
    return 1
  fi
  if ((pass == 0)); then
    echo "[FAIL] 沒有任何一項實際執行" >&2
    return 1
  fi
  return 0
}
