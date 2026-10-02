#!/usr/bin/env bash
# 在每台被监控的机器（宿主或虚拟机）上跑，安装采集工具 vm-collect。
# 用法：
#   sudo ./setup-target.sh [vm-collect 二进制路径]
#   可选：再给一个公钥文件，顺便写入 root 的 authorized_keys
set -euo pipefail

DEST="/usr/local/bin/vm-collect"
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="${1:-$SRC_DIR/vm-collect}"

[ -f "$BIN_SRC" ] || { echo "找不到 vm-collect 二进制"; exit 1; }

echo ">> 安装 $DEST"
install -m 0755 "$BIN_SRC" "$DEST"

# 让 libvirt 下的虚拟机也能被宿主机拿到 IP 等信息
if command -v apt-get >/dev/null 2>&1; then
  dpkg -l qemu-guest-agent >/dev/null 2>&1 || {
    echo ">> 提示: 建议安装 qemu-guest-agent（可让宿主机读到 guest 网络信息）"
    echo "   apt-get install -y qemu-guest-agent"
  }
fi

echo ">> 自检:"
"$DEST" | head -c 200 || true
echo
echo ">> 完成。"
