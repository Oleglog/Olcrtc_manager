#!/bin/sh
set -eu

die() {
    echo "olcrtc-entrypoint: $*" >&2
    exit 1
}

if [ "${1:-}" = "olcrtc" ]; then
    shift
fi

if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/olcrtc "$@"
fi

mode="${OLCRTC_MODE:-srv}"
room_id="${OLCRTC_ROOM_ID:-}"
carrier="${OLCRTC_CARRIER:-${OLCRTC_AUTH:-}}"
transport="${OLCRTC_TRANSPORT:-}"
data_dir="${OLCRTC_DATA_DIR:-/usr/share/olcrtc}"
dns_server="${OLCRTC_DNS:-8.8.8.8:53}"
key="${OLCRTC_KEY:-}"
key_file="${OLCRTC_KEY_FILE:-/var/lib/olcrtc/key.hex}"
socks_proxy="${OLCRTC_SOCKS_PROXY:-}"
socks_proxy_port="${OLCRTC_SOCKS_PROXY_PORT:-1080}"
socks_host="${OLCRTC_SOCKS_HOST:-127.0.0.1}"
socks_port="${OLCRTC_SOCKS_PORT:-8808}"
socks_user="${OLCRTC_SOCKS_USER:-}"
socks_pass="${OLCRTC_SOCKS_PASS:-}"

video_w="${OLCRTC_VIDEO_W:-0}"
video_h="${OLCRTC_VIDEO_H:-0}"
video_fps="${OLCRTC_VIDEO_FPS:-0}"
video_bitrate="${OLCRTC_VIDEO_BITRATE:-}"
video_hw="${OLCRTC_VIDEO_HW:-none}"
video_codec="${OLCRTC_VIDEO_CODEC:-qrcode}"
video_qr_size="${OLCRTC_VIDEO_QR_SIZE:-0}"
video_qr_recovery="${OLCRTC_VIDEO_QR_RECOVERY:-low}"
video_tile_module="${OLCRTC_VIDEO_TILE_MODULE:-0}"
video_tile_rs="${OLCRTC_VIDEO_TILE_RS:-0}"

vp8_fps="${OLCRTC_VP8_FPS:-0}"
vp8_batch="${OLCRTC_VP8_BATCH:-0}"

sei_fps="${OLCRTC_SEI_FPS:-0}"
sei_batch="${OLCRTC_SEI_BATCH:-0}"
sei_frag="${OLCRTC_SEI_FRAG:-0}"
sei_ack="${OLCRTC_SEI_ACK:-0}"

debug="${OLCRTC_DEBUG:-false}"
ffmpeg="${OLCRTC_FFMPEG:-ffmpeg}"

case "$mode" in
    srv|cnc) ;;
    *) die "set OLCRTC_MODE to srv or cnc" ;;
esac
[ -n "$carrier" ] || die "set OLCRTC_CARRIER (e.g. jitsi, telemost, wbstream)"
[ -n "$transport" ] || die "set OLCRTC_TRANSPORT (e.g. datachannel, videochannel, seichannel, vp8channel)"

make_key() {
    if command -v od >/dev/null 2>&1; then
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    else
        hexdump -n 32 -e '32/1 "%02x"' /dev/urandom
    fi
}

if [ -z "$room_id" ]; then
    die "set OLCRTC_ROOM_ID to the room identifier"
fi

if [ -z "$key" ]; then
    if [ -s "$key_file" ]; then
        key="$(tr -d '[:space:]' < "$key_file")"
    elif [ "$mode" = "srv" ]; then
        key="$(make_key)"
        umask 077
        printf '%s\n' "$key" > "$key_file"
        echo "olcrtc-entrypoint: generated encryption key and saved it to $key_file" >&2
        echo "olcrtc-entrypoint: OLCRTC_KEY=$key" >&2
    else
        die "set OLCRTC_KEY or mount OLCRTC_KEY_FILE with the server encryption key"
    fi
fi

case "$key" in
    *[!0-9a-fA-F]*)
        die "OLCRTC_KEY must be a 64-character hex string"
        ;;
esac

[ "${#key}" -eq 64 ] || die "OLCRTC_KEY must be 64 hex characters"

# Generate YAML config
config="/tmp/olcrtc-${mode}.yaml"
cat > "$config" <<EOF
mode: $mode
auth:
  provider: "$carrier"
room:
  id: "$room_id"
crypto:
  key: "$key"
net:
  transport: "$transport"
  dns: "$dns_server"
data: "$data_dir"
EOF

if [ "$mode" = "srv" ] && [ -n "$socks_proxy" ]; then
    cat >> "$config" <<EOF
socks:
  proxy_addr: "$socks_proxy"
  proxy_port: $socks_proxy_port
EOF
fi

if [ "$mode" = "cnc" ]; then
    cat >> "$config" <<EOF
socks:
  host: "$socks_host"
  port: $socks_port
EOF
    if [ -n "$socks_user" ]; then
        cat >> "$config" <<EOF
  user: "$socks_user"
  pass: "$socks_pass"
EOF
    fi
fi

if [ "$transport" = "videochannel" ]; then
    cat >> "$config" <<EOF
video:
  width: $video_w
  height: $video_h
  fps: $video_fps
  hw: $video_hw
  codec: $video_codec
  qr_recovery: $video_qr_recovery
EOF
    [ -n "$video_bitrate" ] && printf '  bitrate: "%s"\n' "$video_bitrate" >> "$config"
    [ "$video_qr_size" -gt 0 ] 2>/dev/null && printf '  qr_size: %s\n' "$video_qr_size" >> "$config"
    [ "$video_tile_module" -gt 0 ] 2>/dev/null && printf '  tile_module: %s\n' "$video_tile_module" >> "$config"
    [ "$video_tile_rs" -gt 0 ] 2>/dev/null && printf '  tile_rs: %s\n' "$video_tile_rs" >> "$config"
fi

if [ "$transport" = "vp8channel" ]; then
    cat >> "$config" <<EOF
vp8:
  fps: $vp8_fps
  batch_size: $vp8_batch
EOF
fi

if [ "$transport" = "seichannel" ]; then
    cat >> "$config" <<EOF
sei:
  fps: $sei_fps
  batch_size: $sei_batch
  fragment_size: $sei_frag
  ack_timeout_ms: $sei_ack
EOF
fi

case "${debug}" in
    1|true|TRUE|yes|YES|on|ON)
        printf 'debug: true\n' >> "$config"
        ;;
esac

[ -n "$ffmpeg" ] && printf 'ffmpeg: "%s"\n' "$ffmpeg" >> "$config"

exec /usr/local/bin/olcrtc "$config"
