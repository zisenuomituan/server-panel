#!/usr/bin/env bash
# 在云服务器上安装面板服务端。用法：
#   ./install-center.sh [center 二进制路径]
# 可用环境变量：
#   PANEL_PORT=8080  PANEL_BACKEND=fake|ssh  PANEL_ADMIN_PASSWORD=xxx
set -euo pipefail

BIN_DIR="/opt/server-panel"
CONF="$BIN_DIR/center.json"
UNIT="/etc/systemd/system/panel-center.service"
PORT="${PANEL_PORT:-8080}"
BACKEND="${PANEL_BACKEND:-fake}"

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
BIN_SRC="${1:-$SRC_DIR/center}"
[ -f "$BIN_SRC" ] || { echo "找不到 center 二进制，请把 center 放在脚本同目录，或用参数指定路径"; exit 1; }

echo ">> 安装到 $BIN_DIR"
mkdir -p "$BIN_DIR"
install -m 0755 "$BIN_SRC" "$BIN_DIR/center"

# 顺手把 vm-collect 放到 center 旁边，面板就能通过 /release/vm-collect 分发给被监控机器
COLLECT_SRC="$(dirname "$BIN_SRC")/vm-collect"
if [ -f "$COLLECT_SRC" ]; then
  install -m 0755 "$COLLECT_SRC" "$BIN_DIR/vm-collect"
  echo ">> 已放入 vm-collect"
fi

if [ ! -f "$CONF" ]; then
  echo ">> 生成配置 $CONF"
  cat > "$CONF" <<EOF
{
  "listen": "0.0.0.0:$PORT",
  "base_url": "http://127.0.0.1:$PORT",
  "db_path": "/opt/server-panel/data/panel.db",
  "token_hours": 12,
  "poll_seconds": 5,
  "backend": "$BACKEND",
  "libvirt_uri": "qemu:///system",
  "ssh_key_path": "/opt/server-panel/id_ed25519",
  "allow_register": true,
  "invite_code": "",
  "enable_exec": true,
  "admin_user": "admin",
  "admin_password": "${PANEL_ADMIN_PASSWORD:-}"
}
EOF
  chmod 600 "$CONF"
else
  echo ">> 已存在配置，保持不变：$CONF"
fi

echo ">> 安装 systemd 服务"
cat > "$UNIT" <<'EOF'
[Unit]
Description=Server Panel (center)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=/opt/server-panel
ExecStart=/opt/server-panel/center -config /opt/server-panel/center.json
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now panel-center

PUBIP="$(curl -s --max-time 3 ifconfig.me || echo '<服务器IP>')"
echo
echo ">> 完成。面板监听 0.0.0.0:$PORT"
echo "   访问 http://$PUBIP:$PORT"
if [ -z "${PANEL_ADMIN_PASSWORD:-}" ]; then
  echo "   提示: 没有设置管理员密码，请到页面注册第一个账号（自动成为管理员），"
  echo "   或在 $CONF 里设置 admin_password 后重启服务。"
fi
