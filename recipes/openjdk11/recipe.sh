#!/bin/bash
# recipe.sh — openjdk11 配方：裝 OpenJDK 11 的 JDK 與 JRE
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；套件已裝時 apt、apk 的安裝是冪等的
#       刻意釘主版號 11：配方名稱就是版本，不是浮動參照
#       套件名稱差異在 pkgmap.yaml：ubuntu 是 openjdk-11-jdk 與 openjdk-11-jre，alpine 的 openjdk11 已含兩者

openjdk11_install() {
  pkg_install openjdk-11-jdk openjdk-11-jre || return 1
}

openjdk11_verify() {
  command -v java >/dev/null || {
    echo "Error: 找不到 java" >&2
    return 1
  }
  java -version 2>&1 | grep -q '"11' || {
    echo "Error: java 主版號不是 11" >&2
    return 1
  }
}
