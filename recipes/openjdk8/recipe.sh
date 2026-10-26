#!/bin/bash
# recipe.sh — openjdk8 配方：裝 OpenJDK 8 的 JDK 與 JRE
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt、apk 的安裝是冪等的
#       刻意釘主版號 8：配方名稱就是版本，不是浮動參照
#       套件名稱差異在 pkgmap.yaml：ubuntu 是 openjdk-8-jdk 與 openjdk-8-jre，alpine 的 openjdk8 已含兩者

openjdk8_install() {
  pkg_install openjdk-8-jdk openjdk-8-jre || return 1
}

openjdk8_verify() {
  command -v java >/dev/null || {
    echo "Error: 找不到 java" >&2
    return 1
  }
  java -version 2>&1 | grep -q '"1\.8' || {
    echo "Error: java 主版號不是 8" >&2
    return 1
  }
}
