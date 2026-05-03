#!/usr/bin/env bash
# Reproducible build of the olcrtc server binaries that the installer ships.
# Outputs:
#   server-install/bin/olcrtc-linux-amd64
#   server-install/bin/olcrtc-linux-arm64
#
# After running this, server-install/install.sh will pick up the local binaries
# instead of downloading them from GitHub Releases.
#
# Requires: Go 1.22+ on the host. CGO is NOT used; the binaries are static.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="$SCRIPT_DIR/bin"

if [ ! -f "$REPO_ROOT/cmd/olcrtc/main.go" ]; then
    echo "[!] Run this from a checkout of Oleglog/Olcrtc_manager (or openlibrecommunity/olcrtc)" >&2
    echo "    expected $REPO_ROOT/cmd/olcrtc/main.go to exist" >&2
    exit 1
fi

if ! command -v go >/dev/null 2>&1; then
    echo "[!] go toolchain is required (https://go.dev/dl/)" >&2
    exit 1
fi

mkdir -p "$OUT_DIR"

build() {
    local goos="$1" goarch="$2" out="$3"
    echo "[*] Building $out (GOOS=$goos GOARCH=$goarch)..."
    (
        cd "$REPO_ROOT"
        CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
            go build -trimpath -ldflags='-s -w' \
            -o "$OUT_DIR/$out" \
            ./cmd/olcrtc
    )
    chmod +x "$OUT_DIR/$out"
    echo "    -> $OUT_DIR/$out ($(du -h "$OUT_DIR/$out" | awk '{print $1}'))"
}

build linux amd64 olcrtc-linux-amd64
build linux arm64 olcrtc-linux-arm64

echo
echo "[+] Done. Binaries are in $OUT_DIR/"
echo "    Now run: sudo $SCRIPT_DIR/install.sh"
