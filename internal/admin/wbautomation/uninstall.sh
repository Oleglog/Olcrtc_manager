#!/usr/bin/env bash
set -euo pipefail

STATE_FILE=/run/olcrtc-wb-components-state.json
INSTALL_DIR=/opt/olcrtc-wb-automation
ASSET_DIR=/usr/local/lib/olcrtc/wb-automation

write_state() {
    local phase="$1" message="$2" percent="$3"
    printf '{"phase":"%s","message":"%s","percent":%s,"updated_at":%s}\n' \
        "$phase" "$message" "$percent" "$(date +%s)" > "$STATE_FILE"
}

write_state stopping "Остановка браузерной сессии..." 10
systemctl stop olcrtc-wb-session.service 2>/dev/null || true
systemctl disable olcrtc-wb-session.service 2>/dev/null || true

packages=""
[ -f "$INSTALL_DIR/packages-installed-by-olcrtc" ] && packages="$(tr '\n' ' ' < "$INSTALL_DIR/packages-installed-by-olcrtc")"

write_state cleaning "Удаление профиля и приватных данных WB..." 35
rm -rf /var/lib/olcrtc-wb /run/olcrtc-wb
rm -f /etc/olcrtc/wb-automation.json /etc/olcrtc/wb-account.json
rm -f /etc/systemd/system/olcrtc-wb-session.service
systemctl daemon-reload

write_state removing "Удаление Playwright и браузера..." 60
rm -rf "$INSTALL_DIR"
userdel olcrtc-wb 2>/dev/null || true

if [ -n "$packages" ]; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get purge -y $packages || true
fi

rm -rf "$ASSET_DIR"
write_state completed "Компоненты автоматизации удалены" 100
