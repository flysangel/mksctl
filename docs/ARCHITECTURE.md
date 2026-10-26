# ARCHITECTURE

## 1. 定位與邊界

mksctl 是 Go 寫的單一靜態執行檔，取代 `provision` 的 bash 腳本庫。其他專案先裝 `mksctl`，再用 `mksctl install <名稱>` 安裝工具或改系統設定。

做的事：

- 平台判斷、依賴排序、套件名稱對照。
- 安裝前檢查：有差異就整批列出並停止。
- 下載與完整性驗證：配方只能經 `mksctl fetch` 下載。
- 寫第三方套件來源。
- 更新 `mksctl` 本身。

不做的事：

- 不建置容器映像（images 專案的工作）。
- 不支援 arm64。只出 amd64。
- 不支援 debian。架構能加，這一版不保證。
- 不釘上游版本。配方一律裝最新版。
- Windows 平台由計畫 4 實作，現在只有兩個接縫，見〈4. 設計取捨〉的「Windows 接縫」。

支援平台：alpine 最新穩定版、ubuntu 24.04、ubuntu 26.04。

## 2. 目錄結構

| 路徑 | 內容 |
|---|---|
| `cmd/mksctl/` | 進入點 |
| `internal/cli/` | 子命令、結束碼 |
| `internal/install/` | `install.Run`：安裝流程 |
| `internal/platform/` | 平台與執行環境判斷，Unix 與 Windows 的判斷用 build tag 分開 |
| `internal/recipe/` | 讀 `recipe.yaml`、驗證、目錄層級測試 |
| `internal/resolve/` | `needs` 與聚合器 `steps` 的排序 |
| `internal/preflight/` | 安裝前檢查，輸出差異清單 |
| `internal/fetch/`、`internal/upstream/` | 查最新版、下載、雜湊與簽章驗證 |
| `internal/aptsource/`、`internal/apkrepo/`、`internal/pkgindex/` | 寫套件來源、衝突偵測、問套件索引 |
| `internal/runner/` | 組合腳本並執行配方，管理行程組 |
| `internal/state/` | 狀態檔 |
| `internal/selfupdate/` | `mksctl update` 與新版提示 |
| `recipes/<名稱>/` | `recipe.yaml`、`recipe.sh`、`files/`（路徑等於目標機器上的落地路徑） |
| `platforms/` | 分層平台定義：`<層>.yaml` 宣告能力，`<層>.sh` 是該層的 shell 函式 |
| `pkgmap.yaml` | 套件名稱對照，按平台層寫 |
| `trust/` | `hashes.yaml` 是自己驗證後記下的雜湊，`keys/` 是釘住的簽章公鑰，來源見 `trust/keys/README.md` |
| `tests/` | 三層實測 harness、`platforms.conf`、已驗紀錄 `verified.conf` |
| `bin/` | `install.sh`（release 附件）與開發用腳本 |
| `.github/` | `release.yml`、`weekly.yml`、`kvm-probe.yml` 與共用的 `setup-harness` action |

## 3. 依賴與資料流

```
recipes/ platforms/ pkgmap.yaml trust/
        |  go:embed
        v
mksctl 執行檔  ---- mksctl fetch ----> 上游（GitHub、官方下載點）
        |                                   |
        |  組成腳本：層檔 + common.sh + recipe.sh
        v
bash 執行 <名稱>_install、<名稱>_verify
```

`mksctl` 的版本就是配方的版本。配方 shell 呼叫 `mksctl fetch <artifact ID>` 取得已驗證的檔，呼叫 `mksctl pkgname` 取得本平台的套件名稱。這兩個子命令讀 `install.Run` 第 3 步寫下的 `run.json`，路徑由環境變數 `MKSCTL_RUN` 傳入。

`install.Run` 的步驟順序，由程式碼決定，順序本身是承諾：

| 步驟 | 動作 | 改機器？ |
|---|---|---|
| 0 | CLI 檢查 root | 否 |
| 1 | 讀平台、排順序 | 否 |
| 2 | 靜態檢查：缺平台檔、缺能力、參數名稱、`unverified` 未允許 | 否 |
| 3 | 查最新版，下載並驗證到暫存目錄 | 否（暫存目錄結束時清掉） |
| 4 | 偵測 apt 來源衝突 | 否 |
| 5 | 寫套件來源（第三方 apt、alpine community 與 edge），更新套件索引 | 是，唯一例外 |
| 6 | 問套件索引，確認套件名稱存在 | 否 |
| 7 | 依序執行配方，每成功一個就存狀態檔 | 是 |

第 2、3、4、6 步有差異時，全部列出再停止。`mksctl check` 只跑到第 4 步，不改機器，也不寫 sigstore 的 TUF 快取。

結束碼：

| 碼 | 意思 |
|---|---|
| 0 | 成功 |
| 1 | 執行失敗 |
| 2 | 用法錯誤 |
| 3 | 安裝前檢查發現差異 |

狀態檔在 `/var/lib/mksctl/state.json`，記每個配方的版本與時間。狀態檔的版本等於查到的最新版時，跳過該配方。沒有版本概念的配方每次都執行。

### 平台分層

從最具體的往上找：`ubuntu-26.04` → `ubuntu` → `apt` → `common`；`alpine-3.24` → `apk` → `common`。alpine 按 minor 版本分。

- 平台宣告 `provides`（能力），配方宣告 `requires`。`go test` 檢查每個 `requires` 都至少有一個平台提供。
- runtime 能力 `init` 和 `kernel` 由執行環境決定，不由平台檔宣告。init 在跑時，服務啟用並啟動；沒有 init（Docker build）時只啟用。
- `pkgmap.yaml` 用 `null` 表示該平台沒有這個套件。

### 雜湊來源順序

1. sigstore 簽章。有 `key` 時用金鑰驗證，沒有時用 keyless（比對 identity 與 issuer）。
2. 上游直連的 checksum 檔或官方 API。
3. 自己驗證後的紀錄：`trust/hashes.yaml`。
4. GitHub API 的 asset digest，只當備援。
5. 套件庫簽章：apt、apk 與官方 apt 來源。

GitHub 上游優先直連：版本號讀 `releases/latest` 的轉址，雜湊直連下載。同一次執行裡，同一個 repo 不重複查。

### 實測與發版

- 每個配方在每個平台實測。場地由 `venue` 決定：`podman`、`lxc`、`vm`。
- `release.yml` 在 Actions 的 runner 上直接跑三個場地，因為 `ubuntu-latest` 有 KVM。
- lxc 與 vm 層通過後寫 `tests/verified.conf`。紀錄綁配方目錄、`platforms/`、`pkgmap.yaml` 與 Go 原始碼的內容雜湊，另有時效。檢查用 `task check:verified`，這是本機工具，發版 workflow 不用。
- `weekly.yml` 每週排程，抓上游改版造成的失敗。
- 發版順序：check → smoke → build → 產生 `checksums.txt` 與 attestation → release。附件是 `mksctl-linux-amd64`、`checksums.txt` 與 `install.sh`。

## 4. 設計取捨

| 取捨 | 內容 | 擋下它的檢查 |
|---|---|---|
| Go 寫 ctl，配方用 bash | 下載只能經 `mksctl`，配方保持可讀。代價是要建置並散布執行檔 | `TestNoSha256OutsideTrust` 與 `fetch` 介面；配方直接下載無程式擋 |
| 程式碼裡沒有 sha256 | 反目標。雜湊來源是必填 | `internal/recipe/catalog_test.go` 的 `TestNoSha256OutsideTrust`，豁免 `tests/verified.conf` |
| 一律裝最新版 | 不釘版本。壞版本寫進 `exclude`，發新 tag 才生效 | 無程式擋 |
| 配方的函式不用 trap，每步明寫 `|| return 1` | 呼叫端可能把函式放在 `||` 左邊，`set -e` 會失效 | `internal/runner` 的測試 |
| 指定版本只用 `--tag`，參數只用 `--set` | 環境變數經 `sudo` 會靜默消失 | 無程式擋 |
| 沒有帶 `--allow-unverified` 不裝沒有雜湊來源的配方 | 完整性驗證不能被無聲繞過 | `internal/preflight` 的靜態檢查 |
| 套件來源先寫、再查套件名稱 | 不先寫來源，就無法確認來源裡的套件查得到。代價是第 5 步會改機器 | `internal/install` 的測試 |
| 第三方 apt 來源寫 deb822 `.sources`，欄位照抄官方文件 | 金鑰用 HTTPS 下載，不比對指紋。寫入前偵測同一 URI 的舊 `.list` 與金鑰衝突，有就停止 | `internal/aptsource` 的測試 |
| `apt_source` 是否適用由平台能力決定 | 看有沒有 `apt` 能力，不看層名稱 | `internal/install` 的測試 |
| 簽章公鑰釘在 `trust/keys/` | 從上游抓公鑰，等於信任能換掉檔案的人。代價是上游換金鑰要手動更新 | 無程式擋 |
| sigstore 同時支援金鑰與 keyless | kube-linter 用 cosign 金鑰簽章，沒有憑證可比對 identity。兩者並存或皆無，載入時報錯 | `internal/recipe` 的 `Validate` |
| guest agent 只在 init 在跑時裝並啟用 | 容器 build 沒有 init，裝了只是變胖或失敗。agent 安裝失敗只警告 | base 配方的實測 |
| ubuntu 24.04 兩種校時服務都沒裝時補裝 chrony | 沿用舊腳本行為。26.04 預設 chrony，24.04 預設 `systemd-timesyncd`，兩者互斥 | ntp 配方的實測 |
| 容器內的 preflight 仍查 guest agent 套件名 | 容器用同一套件庫，查得到 | 無程式擋 |
| `check` 不含套件名稱檢查 | 套件名稱要先寫來源才查得到，`check` 承諾不改機器 | `internal/install` 的 `CheckOnly` 測試 |
| `list --platform` 以 VM 視角計算 | init 與 kernel 都算提供，harness 要的是 VM 上會支援什麼 | 無程式擋 |
| `MKSCTL_SYSROOT` 只給測試 | 它不是使用者設定，不違反「不用環境變數」 | 無程式擋 |
| 用 `go install` 取得 actionlint | 只在開發機用，不進執行檔 | 無程式擋 |
| Windows 接縫 | Unix 專屬程式碼用 build tag 隔開，平台判斷收成一個函式。計畫 4 才實作 Windows 平台 | `task lint` 跑 `GOOS=windows go build ./...` |

## 5. 已知的結構性限制

- 離線時查最新版失敗，已安裝的配方也會被擋。這是「一律最新版」的直接後果。
- VM 層的 `/repo` 唯讀只靠 guest 端 `mount -o ro`，guest root 可以 remount 成可寫。virtiofsd 1.10.0 沒有唯讀選項。
- LXC 層是 privileged 容器，容器內 root 等於 host root。
- 舊 provision 佈建過的機器，不保證能直接改跑 `mksctl`。第 4 步遇到舊 `.list` 衝突時停止並印出清除指令。
- `base` 配方只覆寫自己的檔，不清空 `/mks`。儀表板的落地由計畫 3 處理。
- 發版驗證的 attestation 路徑（`mksctl update`）在第一個 release 前沒有真實樣本驗過，只有 fixture 測試。
- 取消時 runner 用 `Setpgid` 殺整個行程組。Windows 的 `killProc` 只殺主行程，計畫 4 處理。
