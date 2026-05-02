#!/usr/bin/env bash
# olcRTC server uninstaller.
# Removes the systemd service, binaries, config, keys, and system user.
#
# Usage:
#   sudo bash olcrtc-uninstall.sh
#   curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/olcrtc-uninstall.sh | sudo bash

set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] This script must be run as root (try: sudo $0)" >&2
    exit 1
fi

# Check if anything is installed
if [ ! -f /usr/local/bin/olcrtc ] && [ ! -f /etc/systemd/system/olcrtc-server.service ] && [ ! -d /etc/olcrtc ]; then
    echo "[*] olcRTC is not installed. Nothing to do."
    exit 0
fi

echo ""
echo "============================================================"
echo "  olcRTC Uninstaller"
echo "============================================================"
echo ""
echo "  This will remove:"
echo "    - systemd service olcrtc-server"
echo "    - /usr/local/bin/olcrtc"
echo "    - /usr/local/bin/olcrtc-launcher"
echo "    - /etc/olcrtc/  (config + encryption key)"
echo "    - /var/lib/olcrtc/"
echo "    - system user 'olcrtc'"
echo ""

# Read from /dev/tty to support curl | bash
tty_read() {
    if [ -t 0 ]; then
        read "$@"
    else
        read "$@" < /dev/tty
    fi
}

tty_read -rp "  Удалить olcRTC полностью? Это необратимо! [y/N] " confirm
if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
    echo "  Отменено."
    exit 0
fi

echo ""
echo "[*] Останавливаю и удаляю сервис..."
systemctl disable --now olcrtc-server 2>/dev/null || true
systemctl reset-failed olcrtc-server 2>/dev/null || true
rm -f /etc/systemd/system/olcrtc-server.service
systemctl daemon-reload

echo "[*] Удаляю файлы..."
rm -rf /etc/olcrtc /var/lib/olcrtc /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher

echo "[*] Удаляю пользователя olcrtc..."
userdel olcrtc 2>/dev/null || true

echo ""
echo "  olcRTC полностью удалён."
echo ""
