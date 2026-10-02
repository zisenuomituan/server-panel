#!/usr/bin/env bash
#
# 服务器面板一键安装（交互式）。
#
# 用法一（从已有面板分发，二进制由面板提供）：
#   curl -fsSL http://<面板地址>/install.sh | sudo bash
#
# 用法二（直接从 GitHub/Gitee Release 安装，二进制来自发布附件）：
#   curl -fsSL <Release下载目录>/install.sh | sudo bash
#
# 也可以显式指定来源：
#   curl -fsSL <任意>/install.sh | sudo bash -s -- --release-base <Release下载目录>
#   bash install.sh --panel-base http://<面板地址>
#
# 非交互用法（全部用环境变量指定，就没有提示了）：
#   PANEL_BASE=http://x/release PANEL_PORT=8080 \
#   PANEL_ADMIN_USER=admin PANEL_ADMIN_PASSWORD=xxxx \
#   PANEL_HOST_NAME=机房A PANEL_HOST_ADDR=1.2.3.4 PANEL_HOST_PORT=29 \
#   PANEL_HOST_USER=panel PANEL_LIBVIRT_URI=qemu:///system \
#   bash install.sh
#
set -euo pipefail

# 这两个占位符会在对应分发方式下被替换成真实地址
PANEL_BASE="${PANEL_BASE:-__PANEL_BASE__}"
RELEASE_BASE="${PANEL_RELEASE_BASE:-__RELEASE_BASE__}"

# 命令行参数优先
while [ $# -gt 0 ]; do
  case "$1" in
    --panel-base)   PANEL_BASE="${2:-}"; shift 2 ;;
    --release-base) RELEASE_BASE="${2:-}"; shift 2 ;;
    --version)      PANEL_VERSION="${2:-}"; shift 2 ;;
    -h|--help)      sed -n '2,20p' "$0" 2>/dev/null; exit 0 ;;
    *)              shift ;;
  esac
done

INSTALL_DIR="${PANEL_INSTALL_DIR:-/opt/server-panel}"
CONF="$INSTALL_DIR/center.json"
UNIT="${PANEL_UNIT:-/etc/systemd/system/panel-center.service}"

# ---------- 小工具 ----------

INTERACTIVE=0
[ -r /dev/tty ] && INTERACTIVE=1

say() { printf '%s\n' "$*"; }

ask() { # ask VAR 提示 默认值
  local var="$1" prompt="$2" def="${3:-}" ans=""
  if [ "$INTERACTIVE" = 1 ]; then
    if [ -n "$def" ]; then
      printf '%s [%s]: ' "$prompt" "$def" >/dev/tty
    else
      printf '%s: ' "$prompt" >/dev/tty
    fi
    read -r ans </dev/tty || true
  fi
  [ -z "$ans" ] && ans="$def"
  printf -v "$var" '%s' "$ans"
}

ask_secret() { # ask_secret VAR 提示
  local var="$1" prompt="$2" ans=""
  if [ "$INTERACTIVE" = 1 ]; then
    printf '%s: ' "$prompt" >/dev/tty
    read -rs ans </dev/tty || true
    printf '\n' >/dev/tty
  fi
  printf -v "$var" '%s' "$ans"
}

ask_yes() { # ask_yes 提示 默认(y/n)
  local prompt="$1" def="${2:-y}" ans=""
  if [ "$INTERACTIVE" = 1 ]; then
    printf '%s [%s]: ' "$prompt" "$def" >/dev/tty
    read -r ans </dev/tty || true
  fi
  [ -z "$ans" ] && ans="$def"
  case "$ans" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

need_root() {
  [ "$(id -u)" -eq 0 ] || { say "请用 root 运行（sudo bash）"; exit 1; }
}

# 下载文件：优先 curl，退回 wget
download() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    say "系统里没有 curl 也没有 wget，装一个再试"; exit 1
  fi
}

# 从 JSON 里抠一个字符串字段，不依赖 python
json_get() {
  sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p" <<<"$1" | head -1
}

# ---------- 开始 ----------

need_root

cat <<'BANNER'

  服务器面板安装程序
  ------------------

BANNER

# 判断下载来源：优先命令行/环境指定的面板地址，其次 Release 目录
if [ "$PANEL_BASE" != "__PANEL_BASE__" ] && [ -n "$PANEL_BASE" ]; then
  MODE="panel"
elif [ "$RELEASE_BASE" != "__RELEASE_BASE__" ] && [ -n "$RELEASE_BASE" ]; then
  MODE="release"
else
  say "没有拿到下载地址。可以这样运行："
  say "  curl -fsSL <Release下载目录>/install.sh | sudo bash"
  say "  或 bash install.sh --panel-base http://面板地址"
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) say "暂不支持的架构: $(uname -m)"; exit 1 ;;
esac

if [ "$MODE" = "panel" ]; then
  say "安装来源 : 面板 $PANEL_BASE"
else
  # 版本号从 Release 下载目录名（.../v0.1.0）里取
  VERSION="${PANEL_VERSION:-$(basename "$RELEASE_BASE")}"
  VERSION="${VERSION#v}"
  say "安装来源 : Release $RELEASE_BASE (v$VERSION)"
fi
say "系统架构 : $ARCH"
say ""

# 1) 管理员账号
if [ -z "${PANEL_ADMIN_USER:-}" ]; then
  ask PANEL_ADMIN_USER "管理员用户名" "admin"
fi
if [ -z "${PANEL_ADMIN_PASSWORD:-}" ]; then
  ask_secret PANEL_ADMIN_PASSWORD "管理员密码（至少 8 位）"
  if [ "$INTERACTIVE" = 1 ]; then
    confirm_pw=""
    ask_secret confirm_pw "再输一次密码"
    if [ "$confirm_pw" != "$PANEL_ADMIN_PASSWORD" ]; then
      say "两次密码不一致"; exit 1
    fi
  fi
fi
if [ "${#PANEL_ADMIN_PASSWORD}" -lt 8 ]; then
  say "管理员密码至少要 8 位"; exit 1
fi

# 2) 监听端口
if [ -z "${PANEL_PORT:-}" ]; then
  ask PANEL_PORT "面板监听端口" "8080"
fi

# 3) 宿主机信息（可选）
REGISTER_HOST=0
if [ -n "${PANEL_HOST_ADDR:-}" ]; then
  REGISTER_HOST=1
elif [ "$INTERACTIVE" = 1 ] && ask_yes "现在登记一台 KVM 宿主机吗？" "y"; then
  REGISTER_HOST=1
fi

if [ "$REGISTER_HOST" = 1 ]; then
  [ -n "${PANEL_HOST_NAME:-}" ] || ask PANEL_HOST_NAME "宿主机名称" "机房A"
  [ -n "${PANEL_HOST_ADDR:-}" ] || ask PANEL_HOST_ADDR "宿主机地址（frp 映射后的 IP 或域名）"
  [ -n "${PANEL_HOST_PORT:-}" ] || ask PANEL_HOST_PORT "宿主机 SSH 端口" "29"
  [ -n "${PANEL_HOST_USER:-}" ] || ask PANEL_HOST_USER "宿主机 SSH 用户" "panel"
  [ -n "${PANEL_LIBVIRT_URI:-}" ] || ask PANEL_LIBVIRT_URI "libvirt URI" "qemu:///system"
  if [ -z "$PANEL_HOST_ADDR" ]; then
    say "宿主机地址不能为空"; exit 1
  fi
fi

say ""
say "开始安装 ..."

# 4) 下载二进制
mkdir -p "$INSTALL_DIR"

if [ "$MODE" = "panel" ]; then
  CENTER_URL="$PANEL_BASE/release/center"
  COLLECT_URL="$PANEL_BASE/release/vm-collect"
else
  CENTER_URL="$RELEASE_BASE/center_${VERSION}_linux_${ARCH}"
  COLLECT_URL="$RELEASE_BASE/vm-collect_${VERSION}_linux_${ARCH}"
fi

say ">> 下载 center"
download "$CENTER_URL" "$INSTALL_DIR/center.new"
chmod +x "$INSTALL_DIR/center.new"

say ">> 下载 vm-collect"
if download "$COLLECT_URL" "$INSTALL_DIR/vm-collect"; then
  chmod +x "$INSTALL_DIR/vm-collect"
else
  say "   警告: 没能取到 vm-collect，虚拟机内部指标会缺失（可稍后补）"
fi

# 5) SSH 密钥（面板用它连宿主机）
if [ ! -f "$INSTALL_DIR/id_ed25519" ]; then
  say ">> 生成面板 SSH 密钥"
  ssh-keygen -t ed25519 -f "$INSTALL_DIR/id_ed25519" -N "" -q
fi

# 6) 配置文件
if [ ! -f "$CONF" ]; then
  cat > "$CONF" <<EOF
{
  "listen": "0.0.0.0:$PANEL_PORT",
  "base_url": "http://127.0.0.1:$PANEL_PORT",
  "db_path": "$INSTALL_DIR/data/panel.db",
  "token_hours": 12,
  "poll_seconds": 5,
  "backend": "ssh",
  "libvirt_uri": "qemu:///system",
  "ssh_key_path": "$INSTALL_DIR/id_ed25519",
  "allow_register": true,
  "invite_code": "",
  "enable_exec": true,
  "admin_user": "$PANEL_ADMIN_USER",
  "admin_password": "$PANEL_ADMIN_PASSWORD"
}
EOF
  chmod 600 "$CONF"
else
  say ">> 配置已存在，保留不动：$CONF"
fi

# 7) 替换二进制并起服务
mv "$INSTALL_DIR/center.new" "$INSTALL_DIR/center"
mkdir -p "$(dirname "$UNIT")"
cat > "$UNIT" <<EOF
[Unit]
Description=Server Panel (center)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/center -config $INSTALL_DIR/center.json
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable panel-center >/dev/null 2>&1
systemctl restart panel-center

# 8) 等服务起来
API="http://127.0.0.1:$PANEL_PORT"
for i in $(seq 1 30); do
  if curl -fsS -o /dev/null "$API/" 2>/dev/null; then break; fi
  sleep 0.5
done

# 9) 登录并登记宿主机
BIND_KEY=""
if [ "$REGISTER_HOST" = 1 ]; then
  say ">> 登录面板并登记宿主机"
  LOGIN=$(curl -fsS -X POST "$API/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"$PANEL_ADMIN_USER\",\"password\":\"$PANEL_ADMIN_PASSWORD\"}" || true)
  TOKEN=$(json_get "$LOGIN" "token")
  if [ -z "$TOKEN" ]; then
    say "登录失败，宿主机未登记。可稍后在网页“管理”里手动登记。"
  else
    RESP=$(curl -fsS -X POST "$API/api/admin/hosts" \
      -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
      -d "{\"name\":\"$PANEL_HOST_NAME\",\"ssh_host\":\"$PANEL_HOST_ADDR\",\"ssh_port\":$PANEL_HOST_PORT,\"ssh_user\":\"$PANEL_HOST_USER\",\"libvirt_uri\":\"$PANEL_LIBVIRT_URI\"}" || true)
    BIND_KEY=$(json_get "$RESP" "formatted")
  fi
fi

PUBIP="$(curl -fsS --max-time 3 ifconfig.me 2>/dev/null || true)"
[ -z "$PUBIP" ] && PUBIP="<服务器IP>"

say ""
say "================ 安装完成 ================"
say "面板地址 : http://$PUBIP:$PANEL_PORT"
say "管理员   : $PANEL_ADMIN_USER"
say ""
if [ -n "$BIND_KEY" ]; then
  say "宿主机 $PANEL_HOST_NAME 已登记，绑定密钥："
  say "  $BIND_KEY"
  say "（登录后在顶部“输入绑定密钥”处绑定，或给其他账号用）"
  say ""
fi
say "宿主机上还要装一个受限账号（面板才能操作 virsh）："
say "  把下面这段公钥放到宿主机，或直接运行面板提供的 setup-host.sh"
say ""
cat "$INSTALL_DIR/id_ed25519.pub" 2>/dev/null || true
say ""
say "面板自带下载地址："
say "  curl -fsSL http://$PUBIP:$PANEL_PORT/setup-host.sh | sudo bash"
say "=========================================="
