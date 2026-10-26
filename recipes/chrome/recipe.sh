#!/bin/bash
# recipe.sh — chrome 配方：用 Google 官方 apt 來源裝 Chrome，套用企業政策與桌面捷徑旗標
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt 的安裝是冪等的
#       舊 provision 直接下載 current 的 .deb；現在改走官方 apt 來源，完整性由 apt 驗套件庫簽章
#       （spec〈6.6〉第 5 項）。apt 來源（deb822）與金鑰由 mksctl 依 recipe.yaml 的 apt_sources 處理；
#       為什麼 URIs 與套件自己寫的不同，見 recipe.yaml 的註解
#       requires: desktop：只有桌面用途的平台才裝；alpine 沒有這個能力，自動略過
#       政策檔 files/managed_policies.json 由 MKSCTL_RECIPE_DIR 取得

_CHROME_DESKTOP=/usr/share/applications/google-chrome.desktop
_CHROME_POLICY_DIR=/etc/opt/chrome/policies/managed

# _chrome_patch_desktop — 對 .desktop 每個 Exec 加旗標，已加過就略過（重跑冪等）
# 旗標全域關閉 TLS／SSL 憑證驗證，這是高風險設定，但是使用者決定的刻意取捨，沿用舊 provision。
# 取捨依據是計畫 2 的 B4 任務；spec 與 docs/ARCHITECTURE.md〈設計取捨〉目前沒有專門條目，待補。
_chrome_patch_desktop() {
  [[ -f "${_CHROME_DESKTOP}" ]] || {
    echo "Error: 找不到 ${_CHROME_DESKTOP}" >&2
    return 1
  }
  if ! command grep -q -- '--ignore-certificate-errors' "${_CHROME_DESKTOP}"; then
    sed -i '/Exec/s/$/ --ignore-certificate-errors --ignore-ssl-errors --ignore-urlfetcher-cert-requests/' \
      "${_CHROME_DESKTOP}" || return 1
  fi
}

# _chrome_install_policy — 落地企業政策檔；首次覆寫前保留原檔備份，已有備份就不再蓋掉更早的原始版本
_chrome_install_policy() {
  local target="${_CHROME_POLICY_DIR}/managed_policies.json"

  if [[ -f "${target}" && ! -f "${target}.bk" ]]; then
    cp "${target}" "${target}.bk" || return 1
  fi
  install -D -o root -g root -m 0644 "${MKSCTL_RECIPE_DIR}/managed_policies.json" "${target}" || return 1
}

# _chrome_reset_users — 為 /home/ 各帳號清掉舊設定並補 First Run 標記，讓每位使用者從乾淨狀態啟動
_chrome_reset_users() {
  local user_home user

  for user_home in /home/*; do
    [[ -d "${user_home}" ]] || continue
    user="$(basename "${user_home}")"
    su - "${user}" -s /bin/bash -c '
      find "$HOME/.config/google-chrome" -mindepth 1 -delete 2>/dev/null || true
      rmdir "$HOME/.config/google-chrome" 2>/dev/null || true
      mkdir -p "$HOME/.config/google-chrome" && touch "$HOME/.config/google-chrome/First Run"
    ' || {
      echo "Error: 重設 ${user} 的 Chrome 設定失敗" >&2
      return 1
    }
  done
}

chrome_install() {
  pkg_install google-chrome-stable jq || return 1
  _chrome_patch_desktop || return 1
  _chrome_install_policy || return 1
  _chrome_reset_users || return 1
}

chrome_verify() {
  command -v google-chrome >/dev/null || {
    echo "Error: 找不到 google-chrome" >&2
    return 1
  }
  google-chrome --version >/dev/null || {
    echo "Error: google-chrome --version 失敗" >&2
    return 1
  }
  jq empty "${_CHROME_POLICY_DIR}/managed_policies.json" || {
    echo "Error: ${_CHROME_POLICY_DIR}/managed_policies.json 不是合法 JSON" >&2
    return 1
  }
}
