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
carrier="${OLCRTC_CARRIER:-}"
transport="${OLCRTC_TRANSPORT:-}"
link="${OLCRTC_LINK:-direct}"
data_dir="${OLCRTC_DATA_DIR:-/usr/share/olcrtc}"
dns_server="${OLCRTC_DNS:-1.1.1.1:53}"
key="${OLCRTC_KEY:-}"
key_file="${OLCRTC_KEY_FILE:-/var/lib/olcrtc/key.hex}"
socks_proxy="${OLCRTC_SOCKS_PROXY:-}"
socks_proxy_port="${OLCRTC_SOCKS_PROXY_PORT:-1080}"

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

[ "$mode" = "srv" ] || die "server image defaults to OLCRTC_MODE=srv; got '$mode'"
[ -n "$carrier" ] || die "set OLCRTC_CARRIER (e.g. telemost, jazz, wbstream)"
[ -n "$transport" ] || die "set OLCRTC_TRANSPORT (e.g. datachannel, videochannel, seichannel, vp8channel)"

make_key() {
    if command -v od >/dev/null 2>&1; then
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    else
        hexdump -n 32 -e '32/1 "%02x"' /dev/urandom
    fi
}

if [ -z "$room_id" ]; then
    case "$carrier" in
        jazz)
            echo "olcrtc-entrypoint: OLCRTC_ROOM_ID not set, generating room..." >&2
            gen_config="/tmp/olcrtc-gen.yaml"
            cat > "$gen_config" <<GENEOF
mode: gen
auth:
  provider: "$carrier"
net:
  dns: "$dns_server"
gen:
  amount: 1
data: "$data_dir"
GENEOF
            room_id=$(/usr/local/bin/olcrtc "$gen_config")
            [ -n "$room_id" ] || die "room generation failed for carrier '$carrier'"
            echo "olcrtc-entrypoint: generated room ID: $room_id" >&2
            rm -f "$gen_config"
            ;;
        *)
            die "set OLCRTC_ROOM_ID to the room identifier"
            ;;
    esac
fi

if [ -z "$key" ]; then
    if [ -s "$key_file" ]; then
        key="$(tr -d '[:space:]' < "$key_file")"
    else
        key="$(make_key)"
        umask 077
        printf '%s\n' "$key" > "$key_file"
        echo "olcrtc-entrypoint: generated encryption key and saved it to $key_file" >&2
        echo "olcrtc-entrypoint: OLCRTC_KEY=$key" >&2
    fi
fi

case "$key" in
    *[!0-9a-fA-F]*)
        die "OLCRTC_KEY must be a 64-character hex string"
        ;;
esac

[ "${#key}" -eq 64 ] || die "OLCRTC_KEY must be 64 hex characters"

# Generate YAML config
config="/tmp/olcrtc-server.yaml"
cat > "$config" <<EOF
mode: $mode
link: $link
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

if [ -n "$socks_proxy" ]; then
    cat >> "$config" <<EOF
socks:
  proxy_addr: "$socks_proxy"
  proxy_port: $socks_proxy_port
EOF
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

exec /usr/local/bin/olcrtc "$config"
