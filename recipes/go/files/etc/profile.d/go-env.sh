#!/bin/bash
# go-env.sh — 全系統 PATH，供所有登入 shell 生效；由 go 配方落地到 /etc/profile.d/
export GOROOT=/usr/local/go
export GOPATH=${HOME}/go
export PATH=${PATH}:${GOROOT}/bin:${GOPATH}/bin
