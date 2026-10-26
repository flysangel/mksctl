# mksctl 進度

## 1. 目前狀態

- 標準驗證路徑：`task lint`、`task test`、`task test:recipe -- <配方>`、`task check:verified`。
- 計畫 1（垂直切片）功能完成：核心引擎、`install.sh` 與 `mksctl update`、`release.yml`、`weekly.yml`。
- 第一批五個配方 base、helm、ntp、docker、kubelinter，在 alpine-3.24、ubuntu-24.04、ubuntu-26.04 三個平台都實測 `[OK]`，每個場地連跑兩次 `mksctl install` 都 exit 0。base、ntp、docker 走 `pkg_install`，helm 與 kubelinter 走下載路徑。
- Actions 的 `release.yml` 在 `feat/slice` 合併前的最後一個 commit 上跑過，check 與 smoke（podman、lxc、vm）全綠；build 與 release 照設計只在 tag 上跑，所以是 skipped。實測：https://github.com/flysangel/mksctl/actions/runs/40319610488
- 未驗證：
  - `weekly.yml` 沒有在 Actions 跑過。workflow 要在 main 才能手動觸發，合併後第一件事是觸發一次。
  - `mksctl update` 的 attestation 驗證沒有真實 release 樣本，只有 fixture 測試。首次 release 後要實跑一次。
- `tests/verified.conf` 的 lxc 與 vm 紀錄已過期（內容雜湊涵蓋 Go 原始碼，引擎修改之後讓它失效），`task check:verified` 會失敗。發版前要在本機重跑受影響配方。
- 還沒有 tag。

## 2. 進行中

- 分支 `feat/windows`：計畫 4，把 provision-windows 併入成 Windows 平台。計畫與設計文件只存本機，在主工作樹的 `docs/superpowers/` 底下。
- 做到哪：已 rebase 到計畫 2 A 段 merge 之後的 main。計畫 4 的 Task 1 到 7（schema、平台判斷、參數環境變數、PowerShell runner、登錄值執行器、身分與路徑、狀態檔與 restore）已完成並通過審查。Task 8 的 harness、`windows.yml` 與測試配方 `winhello`、`winhellouser` 已 commit，還沒 push。Windows 專屬的程式與測試只驗過能編譯。
- 下一步：push 本分支，在 Actions 跑 `windows.yml`，修到全綠，再做 Task 9（vm 場地）。

## 3. 待辦

### 計畫 2：其餘配方

- 搬其餘配方。範圍是舊腳本合併兩發行版後的全部配方，只有 `ubuntu/app/mks` 不搬。
- 替 gitkraken、postman 這類上游不提供雜湊的檔，用 `task trust:refresh` 記下自己驗證的雜湊。
- 聚合器 `aconfig` 與 `k8stools`，以及 xfce、lxqt 加 XRDP。

### 計畫 3：介面與發版

- `mksctl ui`、`mksctl dashboard`。
- 儀表板落地：`base` 目前只覆寫自己的檔，儀表板的 `/mks/dialog/*.info` 與 `/mks/script/*.sh` 擴充點要由 dashboard 接手。
- `mksctl render`、`mksctl upgrade --all`。
- 發第一個 tag，並實跑真實的 attestation 驗證。
- smoke 測的是開發版建置，release 用 ldflags 重新建置；要改成 smoke 直接測 release 的那個檔。
- 下游階段（images、infra-pve）見設計文件。

### 計畫 4：Windows

- provision-windows 併入：`restore`、`scope: machine | user`、PowerShell 步驟、`venue: actions`、發版前掃毒。
- 補 Windows 接縫的缺口：
  - `os.Geteuid` 在 Windows 回 -1。
  - Windows 的 `killProc` 只殺主行程。
  - `internal/platform/detect_test.go` 沒有 build tag，Windows 上 `go test` 會失敗。

### 已知問題：完整性與網路（優先）

- `install.sh` 不檢查 CPU 架構、`curl` 沒限 https、下載沒有大小上限。
- 離線時查最新版失敗會擋已裝配方；離線時 `list` 與 `status` 多等 3 秒。

### 已知問題：安裝流程

- `Runner == nil` 的檢查太晚（第 5 步已寫來源）。
- 同次安裝兩個配方用同一個 apt 檔名時，沒有查衝突。
- apt 衝突偵測不解析 `Enabled: no`。
- `_svc_enable` 缺工具時一律 Info，會掩蓋服務名拼錯。

### 已知問題：測試與 CI

- 缺的測試：真 runner、`--allow-unverified` 傳遞、`install_bin`、鑽石依賴與循環依賴。
- harness 的 `record_verified` 非原子、無鎖；`run_recipe` 的 trap 會覆蓋呼叫端。
- `weekly.yml` 失敗沒有主動通知；`feat/**` 沒有 concurrency 群組。
- Actions 的 Node.js 20 棄用警告；`ubuntu-latest` 將遷到 Ubuntu 26。
- `kvm-probe.yml` 缺頂層 `permissions: contents: read`。

### 已知問題：小項

- `LoadDefs` 沒有 `KnownFields(true)`，打錯欄位會靜默忽略。
- `VERSION_ID` 缺漏時產生 `ubuntu-` 層名，而不是報錯。
- `bin/check-taskfile.py` 目前略過本 repo，因為 Taskfile 還沒有選單 task；`build` 也不在它的 `VERBS`。
- `-h` 回 2；`parseInterspersed` 不處理 `--`。
