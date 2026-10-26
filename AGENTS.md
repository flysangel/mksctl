# AGENTS.md

寫給 AI agent 的 mksctl 作業指引。只收 mksctl 專屬、而且跨檔的事：地雷、驗證指令、不要做的事。只跟單一檔案有關的坑寫在該檔註解。

## 1. 30 秒定位

- mksctl 是 Go CLI，取代 `provision` 的 bash 腳本庫。產出物是 git tag 加 release 附件。
- Git remote 是 **GitHub**（`flysangel/mksctl`），不是 GitLab。
- 配方在 `recipes/<名稱>/`：`recipe.yaml` 寫宣告，`recipe.sh` 只寫 `<名稱>_install` 與 `<名稱>_verify`。配方、平台檔、`pkgmap.yaml` 與 `trust/` 都編進執行檔。
- 結構與取捨見 `docs/ARCHITECTURE.md`。
- 你會改到的東西大多是 `recipes/`、`platforms/`、`internal/` 與 `tests/`。

## 2. 開工

1. 讀 `docs/PROGRESS.md`〈目前狀態〉。
2. 跑 `task doctor`。
3. 跑 `task lint` 與 `task test`。

基準是紅的時，先修好，再開始新工作。

`task doctor` 查命令、hook 路徑與 `.githooks/pre-push` 的執行位元。

### 寫或改配方

1. 在 `recipes/<名稱>/` 寫 `recipe.yaml` 與 `recipe.sh`。欄位的意思見 `internal/recipe/recipe.go` 的註解。
2. 遵守配方慣例，見〈配方慣例〉。
3. `venue` 宣告實測場地，判準見〈宣告 lxc 場地前先看判準〉。
4. 跑 `task test:recipe -- <名稱>`，三個平台都要 `[OK]`。

## 3. 地雷

### 配方慣例

配方函式由 `mksctl` 組成腳本後執行。呼叫端可能把函式放在 `||` 左邊，這會關掉 `set -e`。所以：

- 每一步都明寫 `|| return 1`。不能只靠 `set -e`。
- 函式內不用 `trap`。暫存目錄用 `local` 變數，並顯式 `rm -rf`。
- 下載一律經 `fetch`，不直接呼叫 `curl` 或 `wget`。
- 服務啟用用 `svc_enable`。沒有 init 時它只啟用，不啟動。

### 程式碼不放 sha256

`internal/recipe/catalog_test.go` 的 `TestNoSha256OutsideTrust` 掃全 repo。sha256 值只准出現在 `trust/` 與 `testdata/`，另外豁免 `tests/verified.conf`：那裡存的是配方內容的指紋，不是上游下載的雜湊。把上游雜湊貼進 `tests/verified.conf`，掃描抓不到。

### podman 預設的 cgroup manager 在某些 session 會壞

症狀是 `crun` 報 sd-bus 錯誤。繞法是指定一份用 `cgroupfs` 的設定檔：

```bash
printf '[engine]\ncgroup_manager = "cgroupfs"\n' > "${TMPDIR:-/tmp}/containers.conf"
CONTAINERS_CONF="${TMPDIR:-/tmp}/containers.conf" task test:recipe -- <名稱>
```

### vm 層需要 kvm 群組

剛加入 `kvm` 群組、還沒重新登入時，改跑 `sg kvm -c 'task test:recipe -- <名稱>'`。

### 宣告 lxc 場地前先看判準

LXC 層是 privileged 容器，容器內的 root 等於 host 的 root。會關 swap 或載入 kernel 模組的配方會動到 host，所以必須宣告 `vm`。判準與取捨寫在 `tests/test_lxc.sh` 檔頭。

### VM 的 `/repo` 唯讀只由 guest 端保證

virtiofsd 1.10.0 沒有唯讀選項。harness 只在 guest 內用 `mount -o ro` 加寫入探針。guest 的 root 可以 remount 成可寫。所以不要在 VM 層跑不可信的配方。

### `tests/verified.conf` 只在乾淨工作樹寫入

髒樹的內容雜湊不對應任何 commit，記下去就是假紀錄，所以 harness 在髒樹不寫。實測後 `git status` 看到 `tests/verified.conf` 沒變，先檢查是否有未 commit 的改動。紀錄的欄位與時效見該檔檔頭，檢查用 `task check:verified`。

### workflow_dispatch 只認 main 上的 workflow 檔

feature 分支上新增或改的 workflow，不能用 `workflow_dispatch` 手動觸發。`release.yml` 因此也在推 `feat/**` 分支時觸發 check 與 smoke。build 與 release 只在 tag 上跑。

### Actions 的 runner 要放行 lxcbr0

docker 把 FORWARD 設成 DROP，lxc 層的容器就沒有網路。`.github/actions/setup-harness/action.yml` 已處理。改那個 action 時，lxc 與 vm 層都要在 Actions 上再跑一次。

## 4. 完成定義

本 repo 的實測在本機跑，發版閘門在 Actions。以下各層全部通過才算完成：

- L1：`task lint`。含 `bin/check-docs.py`、`bin/check-taskfile.py`、`go vet`、Windows 交叉編譯、shellcheck 與 shfmt。
- L2：`task test`。含 `go test ./...` 與 `tests/` 底下的 shell 自我檢查。
- L3：實測。改了任何配方、`platforms/`、`pkgmap.yaml` 或引擎，只跑你動到的配方：`task test:recipe -- <名稱>`。

判讀 `task test:recipe` 的輸出：

- 每個平台、每個配方連跑兩次 `mksctl install`，兩次都要 exit 0 才是 `[OK]`。第二次驗證可以重跑。
- 回 `77` 是 `[SKIP]`，表示這個場地驗不了。全部都 `[SKIP]` 時整體回 1。
- `[FAIL]` 代表真的壞了，或撞到外部因素。先讀失敗配方的註解。

### 改了哪類檔要多驗什麼

- 改配方、`platforms/`、`pkgmap.yaml` 或 Go 原始碼：lxc 與 vm 層的紀錄會過期。重跑受影響配方，再跑 `task check:verified`。
- 改了 `.github/` 底下任何東西：推 `feat/**` 分支，看 Actions 的 `release.yml` 是否全綠。
- 改了 `.githooks/` 或 `bin/check-*.py`：照 `~/.claude/rules/githooks.md`〈改了 `.githooks/` 底下任何東西〉再跑一次真的 hook。
- 改了 Unix 專屬的程式碼：確認有 build tag，並跑 `GOOS=windows go build ./...`。

## 5. 不要做的事

| 不要 | 為什麼 |
|---|---|
| 在程式碼裡放 sha256 值 | 反目標。雜湊來源是必填，值放 `trust/hashes.yaml`。見〈程式碼不放 sha256〉 |
| 為使用者參數加環境變數形式的設定 | 環境變數經過 `sudo` 會靜默消失。指定 tag 用 `--tag`，配方參數用 `--set` |
| 讓配方繞過 `fetch` 直接下載 | 下載必經驗證是核心承諾 |
| 為了讓實測變綠而改判準、刪測試或把 `[FAIL]` 改成 `[SKIP]` | `[FAIL]` 是唯一的訊號 |
| 在髒工作樹跑 `task test:recipe` 後 commit `tests/verified.conf` | 紀錄對不上 commit，發版閘門會被假紀錄放行 |
| 在配方裡釘上游版本號 | 設計是一律用最新版。壞版本寫進 `exclude` |
| 加第三方 apt 來源時自訂 keyring 路徑 | 同一來源出現兩個不同的 `Signed-By`，apt 會拒讀全部來源清單。照官方 `.sources`，欄位寫進 `apt_source` |
