#!/bin/bash
# recipe.sh — hello 配方：實測 harness 的測試對象，裝一支只印 hello 的指令
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟

hello_install() {
  printf '#!/bin/bash\necho hello\n' >/usr/local/bin/hello
  chmod 0755 /usr/local/bin/hello
}

hello_verify() {
  local out

  out="$(/usr/local/bin/hello)"
  [[ "${out}" == "hello" ]] || {
    echo "Error: hello 輸出不是 hello：${out}" >&2
    return 1
  }
}
