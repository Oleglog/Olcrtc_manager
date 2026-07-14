#!/usr/bin/env bash
set -euo pipefail

STATE_FILE=/run/olcrtc-wb-components-state.json
ASSET_DIR=/usr/local/lib/olcrtc/wb-automation
INSTALL_DIR=/opt/olcrtc-wb-automation
PACKAGE_MANIFEST="$INSTALL_DIR/packages-installed-by-olcrtc"
PLAYWRIGHT_VERSION=1.55.0
NODE_VERSION=20.19.4
NODE_ARCHIVE="node-v${NODE_VERSION}-linux-x64.tar.xz"
PACKAGES_BEFORE=$(mktemp)
PACKAGES_AFTER=$(mktemp)
DOWNLOAD_DIR=$(mktemp -d)
trap 'rm -f "$PACKAGES_BEFORE" "$PACKAGES_AFTER"; rm -rf "$DOWNLOAD_DIR"' EXIT

write_state() {
    local phase="$1" message="$2" percent="$3"
    printf '{"phase":"%s","message":"%s","percent":%s,"updated_at":%s}\n' \
        "$phase" "$message" "$percent" "$(date +%s)" > "$STATE_FILE"
}

fail() {
    write_state error "$1" 0
    exit 1
}
trap 'write_state error "Установка компонентов завершилась с ошибкой" 0' ERR

. /etc/os-release
case "${ID:-}" in
    ubuntu|debian) ;;
    *) fail "Поддерживаются только Ubuntu и Debian" ;;
esac
if [ "$(dpkg --print-architecture)" != "amd64" ]; then
    fail "Автоматизация WB поддерживается только на x86_64"
fi

write_state preparing "Подготовка установки..." 5
packages=(xvfb x11vnc novnc openbox ca-certificates curl xz-utils)
dpkg-query -W -f='${binary:Package}\n' 2>/dev/null | sort -u > "$PACKAGES_BEFORE"

write_state apt "Установка системных пакетов..." 15
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends "${packages[@]}"

id olcrtc-wb >/dev/null 2>&1 || useradd --system --create-home --home-dir /var/lib/olcrtc-wb --shell /usr/sbin/nologin olcrtc-wb
install -d -m 0755 -o root -g root "$INSTALL_DIR"
install -d -m 0700 -o olcrtc-wb -g olcrtc-wb /var/lib/olcrtc-wb /var/lib/olcrtc-wb/profile
install -m 0644 "$ASSET_DIR/worker.mjs" "$INSTALL_DIR/worker.mjs"
install -m 0755 "$ASSET_DIR/run-session.sh" "$INSTALL_DIR/run-session.sh"
install -m 0644 "$ASSET_DIR/olcrtc-wb-session.service" /etc/systemd/system/olcrtc-wb-session.service

write_state node "Установка Node.js..." 35
cd "$DOWNLOAD_DIR"
curl -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/${NODE_ARCHIVE}"
curl -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/SHASUMS256.txt"
grep "  ${NODE_ARCHIVE}$" SHASUMS256.txt | sha256sum -c -
tar -xJf "$NODE_ARCHIVE"
rm -rf "$INSTALL_DIR/node"
mv "node-v${NODE_VERSION}-linux-x64" "$INSTALL_DIR/node"

write_state playwright "Установка Playwright..." 50
cd "$INSTALL_DIR"
PATH="$INSTALL_DIR/node/bin:$PATH" npm install --omit=dev --no-audit --no-fund "playwright@${PLAYWRIGHT_VERSION}"

write_state browser "Установка Chromium и библиотек..." 70
PATH="$INSTALL_DIR/node/bin:$PATH" PLAYWRIGHT_BROWSERS_PATH="$INSTALL_DIR/browsers" ./node_modules/.bin/playwright install --with-deps chromium
dpkg-query -W -f='${binary:Package}\n' 2>/dev/null | sort -u > "$PACKAGES_AFTER"
comm -13 "$PACKAGES_BEFORE" "$PACKAGES_AFTER" > "$PACKAGE_MANIFEST"
chown -R root:root "$INSTALL_DIR"
chmod -R go-w "$INSTALL_DIR"

systemctl daemon-reload
write_state completed "Компоненты автоматизации установлены" 100
