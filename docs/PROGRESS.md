# mksctl 進度

## 1. 目前狀態

- 標準驗證路徑：`task lint`、`task test`、`task test:recipe -- <配方>`、`task check:verified`。
- 計畫 1（垂直切片）功能完成：核心引擎、`install.sh` 與 `mksctl update`、`release.yml`、`weekly.yml`。
- 第一批五個配方 base、helm、ntp、docker、kubelinter，在 alpine-3.24、ubuntu-24.04、ubuntu-26.04 三個平台都實測 `[OK]`，每個場地連跑兩次 `mksctl install` 都 exit 0。
- Actions 的 `release.yml` 在 `feat/slice` 上跑過，check 與 smoke（podman、lxc、vm）全綠；build 與 release 照設計只在 tag 上跑，所以是 skipped。實測：https://github.com/flysangel/mksctl/actions/runs/40313568150
- 未驗證：
  - `weekly.yml` 沒有在 Actions 跑過。workflow 要在 main 才能手動觸發，合併後第一件事是觸發一次。
  - `mksctl update` 的 attestation 驗證沒有真實 release 樣本，只有 fixture 測試。首次 release 後要實跑一次。
  - `pkg_install` 沒有在真機執行過（helm 與 kubelinter 走下載路徑）。
- `tests/verified.conf` 的 lxc 與 vm 紀錄已過期（內容雜湊涵蓋 Go 原始碼，Task 13 之後的引擎修改讓它失效），`task check:verified` 會失敗。發版前要在本機重跑受影響配方。
- 還沒有 tag。

## 2. 進行中

分支 `feat/slice`。

做到哪：計畫 1 的 Task 1 到 17 已完成，四大文件已寫。

下一步：

1. final code review。
2. 合併回 main，刪掉本節。
3. 問使用者要不要 push；本計畫不打 tag。

## 3. 待辦

### 計畫 2：其餘配方

- 搬其餘配方。範圍是舊腳本合併兩發行版後的全部配方，只有 `ubuntu/app/mks` 不搬。
- 做 `task trust:refresh -- <名稱>`，並替 gitkraken、postman 這類上游不提供雜湊的檔記下自己驗證的雜湊。
- ubuntu 的 `apt` 配方與 `base` 的先後順序要定。
- 聚合器 `aconfig` 與 `k8stools`，以及 xfce、lxqt 加 XRDP。

### 計畫 3：介面與發版

- `mksctl ui`、`mksctl dashboard`。
- 儀表板落地：`base` 目前只覆寫自己的檔，儀表板的 `/mks/dialog/*.info` 與 `/mks/script/*.sh` 擴充點要由 dashboard 接手。
- `mksctl render`、`mksctl upgrade --all`。
- 發第一個 tag，並實跑真實的 attestation 驗證。
- 下游階段（images、infra-pve）見設計文件。

### 計畫 4：Windows

- provision-windows 併入：`restore`、`scope: machine | user`、PowerShell 步驟、`venue: actions`、發版前掃毒。
- 補 Windows 接縫的缺口：
  - `os.Geteuid` 在 Windows 回 -1。
  - Windows 的 `killProc` 只殺主行程。
  - `internal/platform/detect_test.go` 沒有 build tag，Windows 上 `go test` 會失敗。

### 已知問題：完整性與網路（優先）

- keyless 簽章驗證沒有要求 SCT（signed certificate timestamp）。
- `rootOnce` 與 `Latest` 記住第一次的錯誤，暫時的網路錯誤會拖垮整輪。
- 沒有 tag 時查到的版本與 `/latest/download` 下載的可能不同；attestation 沒綁到要裝的 tag。
- `install.sh` 不檢查 CPU 架構、`curl` 沒限 https、下載沒有大小上限。
- 離線時查最新版失敗會擋已裝配方；離線時 `list` 與 `status` 多等 3 秒。

### 已知問題：安裝流程

- `Runner == nil` 的檢查太晚（第 5 步已寫來源）。
- 第 5 步金鑰下載失敗會中途回錯，可以移到第 3 步預抓。
- 同次安裝兩個配方用同一個 apt 檔名時，沒有查衝突。
- apt 衝突偵測不掃 `/etc/apt/sources.list`，不解析 `Enabled: no`。
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
- `recipes/ntp/recipe.sh` 的註解還指向已刪的 `ubuntu-26.04.sh`。
- `bin/check-taskfile.py` 目前略過本 repo，因為 Taskfile 還沒有選單 task；`build` 也不在它的 `VERBS`。
- `-h` 回 2；`parseInterspersed` 不處理 `--`。
