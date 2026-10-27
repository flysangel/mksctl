#!/bin/bash
# recipe.sh — hadooplib 配方：裝編譯 Hadoop 原生程式碼的建置工具與開發函式庫
# 註記：函式庫不設 set 旗標，set -e 由 runner 組合腳本統一開啟
#       本配方沒有 latest，每次安裝都會執行；apt 的安裝是冪等的
#       套件清單在 recipe.yaml 的 packages，分成核心建置工具與 Hadoop 專屬開發函式庫兩段

hadooplib_install() {
  pkg_install \
    build-essential autoconf automake libtool cmake \
    zlib1g-dev pkg-config libssl-dev libsnappy-dev \
    bzip2 libbz2-dev libjansson-dev fuse libfuse-dev liblzo2-dev lzop \
    maven ant gcc g++ libkrb5-dev libffi-dev \
    libsasl2-dev libsasl2-modules-gssapi-mit libsqlite3-dev \
    libtidy-dev libxml2-dev libxslt-dev make libldap2-dev \
    python3-dev python3-setuptools libgmp3-dev || return 1
}

hadooplib_verify() {
  local cmd
  for cmd in mvn gcc; do
    command -v "${cmd}" >/dev/null || {
      echo "Error: 找不到 ${cmd}" >&2
      return 1
    }
  done
}
