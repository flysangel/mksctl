# mksctl 進度

## 1. 目前狀態

- 標準驗證路徑：`task lint`、`task test`、`task test:recipe -- <配方>`、`task check:verified`。
- 計畫 1（垂直切片）與計畫 2 的引擎前置（A 段）已在 main。計畫 2 的配方（B 段）已搬進 `recipes/`，只有 k8s 配方延後（見〈待辦〉）。
- 配方的實測狀態：podman 層的配方不留紀錄，用 `task test:recipe -- <配方>` 重跑。lxc 與 vm 層的紀錄在 `tests/verified.conf`，用 `task check:verified` 查是否齊全、相符、未過期。配方清單用 `mksctl list --all` 查。
- Actions 的 `release.yml`：計畫 1 的最後一個 commit 上 check 與 smoke 全綠（https://github.com/flysangel/mksctl/actions/runs/40319610488）。B 段之後的 commit 還沒在 Actions 跑過。build 與 release 照設計只在 tag 上跑。
- 未驗證：
  - `weekly.yml` 沒有在 Actions 跑過。workflow 要在 main 才能手動觸發，合併後第一件事是觸發一次。
  - `mksctl update` 的 attestation 驗證沒有真實 release 樣本，只有 fixture 測試。首次 release 後要實跑一次。
- 改了引擎、配方、`platforms/` 或 `pkgmap.yaml` 後，`tests/verified.conf` 的紀錄會過期，發版前要重跑受影響配方。
- 還沒有 tag。

## 2. 進行中

分支 `feat/recipes`：搬計畫 2 的配方。

- 做到哪：B1 到 B10 完成；最終審查的修正已做完（含補搬 gpicview、hadooplib、vlc），受影響配方的實測紀錄已重驗。
- 下一步：推分支讓 Actions 跑三層實測，全綠後改〈目前狀態〉、刪掉本節，再 merge 回 main。

## 3. 待辦

### 計畫 2：剩餘項目

- 容器裡裝不了 aconfig、xrdp、xfce、lxqt：它們的步驟 `requires: kernel`，容器平台不提供 kernel，安裝前檢查會整個擋下，不會略過那一步。images 的 rdp 映像（容器建置裡跑 xfce、lxqt）因此無法直接改用 mksctl。由使用者決定做法：
  - (a) resolver 或 preflight 遇到 steps 裡平台不支援的配方時略過，不擋。要改 `internal/`，動手前先通知 provision-windows。
  - (b) 另做一個不含 kernel 步驟的容器用聚合器。
- k8s 配方：等計畫 4（Windows）merge 進 main 後，從新的 main 另開分支做。`apt_sources` 樣板要多一個 minor 版本變數（例如 `{{.Minor}}`，由 `latest:` 的版本取 `vX.Y`），要改 `internal/aptsource` 的 expand、FindConflicts、FetchKey 與 `internal/install/install.go` 的呼叫端。動 `internal/` 前先通知 provision-windows。
- `apt_sources` 支援 dearmor，讓金鑰路徑能對齊官方的 `.gpg` keyring。
- `apt_sources` 支援 pin 檔。xfce 目前在 install 裡自己寫 `/etc/apt/preferences.d/mozilla`。
- 移除沒人用的 `jetbrains-iic` provider（`internal/upstream`）。動 `internal/` 前先通知 provision-windows。
- `pkgmap.yaml` 的 `openssh-client` 登記等於原樣對照，可以刪。
- vscode 配方裡 `foxundermoon.shell-format` 的釘版理由待查（見 `recipes/vscode/recipe.sh` 註解）。

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
