#!/bin/bash
# check-verified.sh — 確認 venue 是 lxc 或 vm 的每個配方、每個支援平台，在 tests/verified.conf 都有新鮮的通過紀錄
# 驅動：本機工具（task check:verified）。release.yml 在 runner 上直接跑 lxc 與 vm 層，不呼叫它
# 註記：逐項容錯，捨棄 -e；缺的全部列出後才 exit 1
set -uo pipefail

# 環境變數覆寫只給 tests/test_check_verified.sh 用
MKSCTL_BIN="${MKSCTL_BIN:-dist/mksctl}"
VERIFIED_CONF="${VERIFIED_CONF:-tests/verified.conf}"
PLATFORMS_CONF="${PLATFORMS_CONF:-tests/platforms.conf}"
VERIFIED_HASH_CMD="${VERIFIED_HASH_CMD:-bin/verified-hash.sh}"
MAX_AGE_DAYS=7

cd "$(git rev-parse --show-toplevel)" || {
  echo "Error: 不在 git repo 裡" >&2
  exit 1
}
[[ -x "${MKSCTL_BIN}" ]] || {
  echo "Error: 找不到 ${MKSCTL_BIN}，先跑 task build" >&2
  exit 1
}
[[ -f "${VERIFIED_CONF}" ]] || {
  echo "Error: 讀不到 ${VERIFIED_CONF}" >&2
  exit 1
}

# 平台層：platforms.conf 每行的第一欄
layers=()
while IFS= read -r line; do
  line="${line%%|*}"
  line="${line//[[:space:]]/}"
  case "${line}" in
  "" | "#"*) continue ;;
  esac
  layers+=("${line}")
done <"${PLATFORMS_CONF}"
((${#layers[@]} > 0)) || {
  echo "Error: ${PLATFORMS_CONF} 沒有任何平台" >&2
  exit 1
}

listing="$("${MKSCTL_BIN}" list --all)" || {
  echo "Error: ${MKSCTL_BIN} list --all 失敗" >&2
  exit 1
}

now="$(date -u +%s)"
missing=()
checked=0

while read -r name venue _; do
  case "${venue}" in
  lxc | vm) ;;
  *) continue ;;
  esac
  hash="$(bash "${VERIFIED_HASH_CMD}" "${name}")" || {
    echo "Error: 算不出 ${name} 的雜湊" >&2
    exit 1
  }
  for layer in "${layers[@]}"; do
    supported="$("${MKSCTL_BIN}" list --platform "${layer}")" || {
      echo "Error: list --platform ${layer} 失敗" >&2
      exit 1
    }
    grep -qx -- "${name}" <<<"${supported}" || continue
    checked=$((checked + 1))

    # 找這個「配方 平台層 venue」的紀錄
    rec="$(awk -v k="${name} ${layer} ${venue}" '!/^#/ && NF && ($1 " " $2 " " $3) == k { print $4, $5 }' "${VERIFIED_CONF}")"
    if [[ -z "${rec}" ]]; then
      missing+=("${name} ${layer} ${venue}：沒有紀錄")
      continue
    fi
    read -r rec_hash rec_date <<<"${rec}"
    if [[ "${rec_hash}" != "${hash}" ]]; then
      missing+=("${name} ${layer} ${venue}：雜湊過期")
      continue
    fi
    rec_epoch="$(date -u -d "${rec_date}" +%s 2>/dev/null)" || {
      missing+=("${name} ${layer} ${venue}：日期 ${rec_date} 格式錯誤")
      continue
    }
    age_days=$(((now - rec_epoch) / 86400))
    if ((age_days > MAX_AGE_DAYS)); then
      missing+=("${name} ${layer} ${venue}：紀錄已 ${age_days} 天，超過 ${MAX_AGE_DAYS} 天")
    fi
  done
done <<<"${listing}"

if ((${#missing[@]} > 0)); then
  echo "Error: 以下 ${#missing[@]} 項沒有新鮮的通過紀錄：" >&2
  printf '  %s\n' "${missing[@]}" >&2
  exit 1
fi
echo "[OK] ${checked} 項紀錄都在 ${MAX_AGE_DAYS} 天內且雜湊相符"
