#!/bin/bash
# 本機 console 無 CJK 字型故用 en_US；SSH 終端機可顯示中文故用 zh_TW
if [ -z "${SSH_TTY}" ]; then
  export LANG=en_US.UTF-8
  export LC_ALL=en_US.UTF-8
  export LANGUAGE=en_US.UTF-8
else
  export LC_ALL=zh_TW.UTF-8
fi
