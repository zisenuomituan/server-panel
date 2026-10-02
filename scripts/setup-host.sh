#!/usr/bin/env bash
# 在 KVM 宿主机上跑一次，创建一个只能管 libvirt 的受限账号。
# 用法：
#   sudo ./setup-host.sh /path/to/center_id_ed25519.pub
# 不给公钥参数也行，之后再手动往 authorized_keys 里加。
set -euo pipefail

PANEL_USER="${PANEL_USER:-panel}"
PUBKEY="${1:-}"
SUDOERS="/etc/sudoers.d/server-panel"

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行"; exit 1
fi

echo ">> 创建用户 $PANEL_USER"
id "$PANEL_USER" >/dev/null 2>&1 || useradd -m -s /bin/bash "$PANEL_USER"

# 加入 libvirt 组，这样直接跑 virsh qemu:///system 不需要 sudo
if getent group libvirt >/dev/null 2>&1; then
  usermod -aG libvirt "$PANEL_USER"
fi

if [ -n "$PUBKEY" ]; then
  [ -f "$PUBKEY" ] || { echo "公钥文件不存在: $PUBKEY"; exit 1; }
  echo ">> 写入 authorized_keys"
  install -d -m 700 -o "$PANEL_USER" -g "$PANEL_USER" "/home/$PANEL_USER/.ssh"
  touch "/home/$PANEL_USER/.ssh/authorized_keys"
  grep -qxF "$(cat "$PUBKEY")" "/home/$PANEL_USER/.ssh/authorized_keys" 2>/dev/null || cat "$PUBKEY" >> "/home/$PANEL_USER/.ssh/authorized_keys"
  chown -R "$PANEL_USER:$PANEL_USER" "/home/$PANEL_USER/.ssh"
  chmod 600 "/home/$PANEL_USER/.ssh/authorized_keys"
fi

# 兜底：如果没进 libvirt 组，也允许用 sudo 跑 virsh
echo ">> 写入 sudoers 白名单（仅 virsh）"
cat > "$SUDOERS" <<EOF
$PANEL_USER ALL=(root) NOPASSWD: /usr/bin/virsh
EOF
chmod 440 "$SUDOERS"
visudo -cf "$SUDOERS" >/dev/null

echo
echo ">> 完成。检查一下："
echo "   sudo -u $PANEL_USER virsh -c qemu:///system list --all"
echo "   （如果这里报权限错，重新登录让组权限生效，或确认 libvirtd 在运行）"
