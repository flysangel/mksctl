# mksctl 進度

## 1. 目前狀態

- repo 骨架與護欄已建立。`task doctor`、`task lint`、`task test`、`task build` 可跑。
- `bin/check-taskfile.py` 目前會略過本 repo，因為 Taskfile 還沒有選單 task。`build` 也不在它的 `VERBS`，套用分類原則前要先決定動詞。
- Actions 的 `ubuntu-latest` 可用 KVM：`/dev/kvm` 存在，加 udev 規則設成 0666 後，qemu 用 `-enable-kvm` 開機成功。實測：https://github.com/flysangel/mksctl/actions/runs/40291786862

## 2. 待辦

- 計畫 1：切片實作。
