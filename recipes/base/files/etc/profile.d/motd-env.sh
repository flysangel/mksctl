#!/bin/bash
clear
# 兩個發行版共用本檔，只有歡迎詞不同
if [ -f /etc/alpine-release ]; then
  echo -e "\nWelcome to \"Micro Kube System\" (based on Alpine $(cat /etc/alpine-release))
The Alpine Wiki see <http://wiki.alpinelinux.org/>.\n"
else
  echo -e "\nWelcome to \"Micro Kube System\" (based on $(lsb_release -s -d))
The Documentation see <https://help.ubuntu.com>.\n"
fi

export SHELL='/bin/bash'

# 容錯設計：profile 腳本失敗時 export 空字串，不中斷登入
# shellcheck disable=SC2155
export GW=$(ip route | awk '/default/ {print $3}')
# shellcheck disable=SC2155
export GWIF=$(ip route | awk '/default/ {print $5}')
# shellcheck disable=SC2155
export IP=$(ip a show "${GWIF}" | awk '/inet / {print $2}' | cut -d'/' -f1)
# shellcheck disable=SC2155
export MAC=$(ip a show "${GWIF}" | awk '/ether/ {print $2}')
# shellcheck disable=SC2155
export NETMASK=$(ip a show "${GWIF}" | awk '/inet / {print $2}' | cut -d'/' -f2)

if [ "${IP}" != "" ]; then
  echo "
${GWIF} IP: ${IP}/${NETMASK}
${GWIF} GW: ${GW}
"
fi
