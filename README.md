# mksctl

用 Go 寫的佈建 CLI，取代 `provision` 的 bash 腳本庫。
`mksctl` 像 `kubectl` 一樣用：先裝 `mksctl`，再用它安裝工具或改系統設定。
配方編在執行檔裡，所以 `mksctl` 的版本就是配方的版本。上游工具一律裝最新版。

## 1. 安裝

安裝和更新是同一行。已經裝過的機器跑這一行，就更新到最新版：

```bash
curl -fsSL https://github.com/flysangel/mksctl/releases/latest/download/install.sh | sudo bash
```

要回到舊版時，從該版的 release 下載 `install.sh`，再用 `--tag` 指定版本：

```bash
curl -fsSL https://github.com/flysangel/mksctl/releases/download/vX.Y.Z/install.sh | sudo bash -s -- --tag vX.Y.Z
```

cloud-init 沒有管線保護，先下載成檔案，再執行：

```bash
curl -fsSL -o /tmp/install.sh https://github.com/flysangel/mksctl/releases/latest/download/install.sh
bash /tmp/install.sh
```

裝好後，`mksctl update` 做同樣的事：

- `mksctl update`：更新到最新版。
- `mksctl update --tag vX.Y.Z`：換到指定版本。

支援平台是 alpine 最新穩定版、ubuntu 24.04 與 ubuntu 26.04，只出 amd64。
`mksctl list --platform <層>` 列出某個平台能裝的配方。

沒有帳號層 hook 時，開發本 repo 要先跑 `git config core.hooksPath .githooks`。

## 2. 用法

```bash
sudo mksctl install helm ntp
mksctl check docker
mksctl list --all
mksctl status
mksctl version
```

| 子命令 | 做什麼 |
|---|---|
| `install <名稱>...` | 安裝配方。前置依賴自動處理，可以重跑 |
| `check <名稱>...` | 只做安裝前檢查，不改機器 |
| `list` | 列出目前平台能裝的配方。`--all` 列出全部配方與它們的實測場地，`--platform <層>` 指定平台 |
| `status` | 列出已裝配方的版本與時間 |
| `update [--tag vX.Y.Z]` | 更新 `mksctl` 本身，並驗證 artifact attestation |
| `version` | 印出版本 |

### 2.1 參數

- `--set KEY=VALUE`：傳參數給配方。只有配方在 `recipe.yaml` 的 `params` 宣告過的名稱才收。
- 值寫 `-` 時從 stdin 讀。密碼用這個方式傳，才不會出現在 `ps` 裡。目前的配方都沒有宣告參數，之後有參數的配方照這個寫法：

  ```bash
  printf '%s' "${PASSWORD}" | sudo mksctl install <配方> --set <名稱>=-
  ```

- `--allow-unverified`：允許安裝沒有任何雜湊來源的配方。沒帶這個旗標時，安裝前檢查會停止。

### 2.2 結束碼

| 碼 | 意思 |
|---|---|
| 0 | 成功 |
| 1 | 執行失敗，例如下載、安裝或驗證失敗 |
| 2 | 用法錯誤 |
| 3 | 安裝前檢查發現差異。機器保持原樣（只可能多了套件來源和新的套件索引） |

### 2.3 GitHub token

查 GitHub 上游最新版會受 API 限流影響。要提高額度時，把 token 寫進 `/etc/mksctl/github-token`。

## 3. 貢獻

1. 讀 `AGENTS.md`，照〈開工〉跑 `task doctor`、`task lint` 與 `task test`。
2. 結構與設計取捨見 `docs/ARCHITECTURE.md`。
3. 待辦與進度見 `docs/PROGRESS.md`。
4. 改配方後，跑 `task test:recipe -- <配方>`。

## 4. 授權

Apache-2.0，全文見 `LICENSE`。
