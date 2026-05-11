#!/usr/bin/env bash
# olcRTC server uninstaller.
# Removes the systemd service, binaries, config, keys, and system user.
#
# Usage:
#   sudo bash olcrtc-uninstall.sh
#   curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash

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

# Count extra instances
extra_count=0
for d in /etc/olcrtc/*/env; do
    [ -f "$d" ] || continue
    n="$(basename "$(dirname "$d")")"
    [[ "$n" =~ ^[0-9]+$ ]] && extra_count=$((extra_count + 1))
done

echo ""
echo "============================================================"
echo "  olcRTC Uninstaller"
echo "============================================================"
echo ""
echo "  This will remove:"
echo "    - systemd service olcrtc-server"
echo "    - systemd service olcrtc-admin"
if [ "$extra_count" -gt 0 ]; then
    echo "    - $extra_count additional instance(s) (olcrtc-server@*.service)"
fi
echo "    - /usr/local/bin/olcrtc"
echo "    - /usr/local/bin/olcrtc-admin"
echo "    - /usr/local/bin/olcrtc-launcher"
echo "    - /etc/olcrtc/  (all configs + encryption keys + admin.env)"
echo "    - /var/lib/olcrtc/ and /var/lib/olcrtc-*/"
echo "    - /var/lib/olcrtc/admin-tls/ (certificates)"
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
echo "[*] Останавливаю и удаляю основной сервис..."
systemctl disable --now olcrtc-server 2>/dev/null || true
systemctl reset-failed olcrtc-server 2>/dev/null || true
rm -f /etc/systemd/system/olcrtc-server.service

echo "[*] Останавливаю и удаляю Admin UI..."
systemctl disable --now olcrtc-admin 2>/dev/null || true
systemctl reset-failed olcrtc-admin 2>/dev/null || true
rm -f /etc/systemd/system/olcrtc-admin.service

echo "[*] Останавливаю и удаляю wireproxy-warp (если установлен)..."
systemctl disable --now wireproxy-warp 2>/dev/null || true
rm -f /etc/systemd/system/wireproxy-warp.service

echo "[*] Останавливаю и удаляю все дополнительные инстансы..."
for d in /etc/olcrtc/*/env; do
    [ -f "$d" ] || continue
    n="$(basename "$(dirname "$d")")"
    [[ "$n" =~ ^[0-9]+$ ]] || continue
    systemctl disable --now "olcrtc-server@${n}.service" 2>/dev/null || true
    systemctl reset-failed "olcrtc-server@${n}.service" 2>/dev/null || true
done
rm -f /etc/systemd/system/olcrtc-server@.service
systemctl daemon-reload

# Subscription database
sub_db_found=0
for _sdb in /var/lib/olcrtc/subscriptions.db /var/lib/olcrtc-*/subscriptions.db; do
    [ -f "$_sdb" ] && sub_db_found=1 && break
done
if [ "$sub_db_found" -eq 1 ]; then
    tty_read -rp "  Удалить базу данных подписок? (y/N): " del_sub_db
    if [ "$del_sub_db" = "y" ] || [ "$del_sub_db" = "Y" ]; then
        rm -f /var/lib/olcrtc/subscriptions.db /var/lib/olcrtc-*/subscriptions.db 2>/dev/null || true
        echo "  База подписок удалена."
    else
        echo "  База подписок сохранена для переноса на новый сервер."
        if [ -f /var/lib/olcrtc/subscriptions.db ]; then
            cp /var/lib/olcrtc/subscriptions.db /tmp/olcrtc-subscriptions.db 2>/dev/null || true
            echo "  Копия: /tmp/olcrtc-subscriptions.db"
        fi
    fi
fi

echo "[*] Удаляю файлы..."
rm -rf /etc/olcrtc /var/lib/olcrtc /var/lib/olcrtc-* /usr/local/bin/olcrtc /usr/local/bin/olcrtc-admin /usr/local/bin/olcrtc-launcher /usr/local/bin/wireproxy /etc/olcrtc

echo "[*] Удаляю пользователя olcrtc..."
userdel olcrtc 2>/dev/null || true

echo ""
echo "  olcRTC полностью удалён."
echo ""
