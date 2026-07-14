#!/usr/bin/env bash
set -euo pipefail
umask 077

export DISPLAY=:97
export HOME=/var/lib/olcrtc-wb
export XDG_RUNTIME_DIR=/run/olcrtc-wb/xdg
export PLAYWRIGHT_BROWSERS_PATH=/opt/olcrtc-wb-automation/browsers

mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"
rm -f /tmp/.X97-lock /tmp/.X11-unix/X97

cleanup() {
    jobs -pr | xargs -r kill 2>/dev/null || true
}
trap cleanup EXIT INT TERM

Xvfb "$DISPLAY" -screen 0 1280x800x24 -nolisten tcp -ac >/dev/null 2>&1 &
for _ in $(seq 1 50); do
    [ -S /tmp/.X11-unix/X97 ] && break
    sleep 0.1
done

openbox >/dev/null 2>&1 &
x11vnc -display "$DISPLAY" -rfbport 5907 -localhost -forever -shared -nopw -quiet -noxdamage >/dev/null 2>&1 &

exec /opt/olcrtc-wb-automation/node/bin/node /opt/olcrtc-wb-automation/worker.mjs /run/olcrtc-wb/job.json
