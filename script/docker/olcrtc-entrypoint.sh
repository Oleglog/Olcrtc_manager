#!/bin/sh
set -eu

die() {
    echo "olcrtc-entrypoint: $*" >&2
    exit 1
}

bool_flag() {
    case "${1:-}" in
        1|true|TRUE|yes|YES|on|ON) return 0 ;;
        *) return 1 ;;
    esac
}

make_key() {
    if command -v od >/dev/null 2>&1; then
        od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    else
        hexdump -n 32 -e '32/1 "%02x"' /dev/urandom
    fi
}

if [ "${1:-}" = "olcrtc" ]; then
    shift
fi

if [ "$#" -gt 0 ]; then
    exec /usr/local/bin/olcrtc "$@"
fi

mode="${OLCRTC_MODE:-srv}"
room_id="${OLCRTC_ROOM_ID:-${ROOM_ID:-}}"
carrier="${OLCRTC_CARRIER:-${OLCRTC_PROVIDER:-}}"
transport="${OLCRTC_TRANSPORT:-}"
link="${OLCRTC_LINK:-direct}"
data_dir="${OLCRTC_DATA_DIR:-/usr/share/olcrtc}"
dns_server="${OLCRTC_DNS:-1.1.1.1:53}"
key="${OLCRTC_KEY:-${KEY:-}}"
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

[ "$mode" = "srv" ] || die "server image defaults to OLCRTC_MODE=srv; got '$mode'"
[ -n "$carrier" ] || die "set OLCRTC_CARRIER (e.g. telemost, jazz, wbstream)"
[ -n "$transport" ] || die "set OLCRTC_TRANSPORT (e.g. datachannel, videochannel, seichannel, vp8channel)"
[ -n "$room_id" ] || die "set OLCRTC_ROOM_ID to the room identifier"

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

set -- /usr/local/bin/olcrtc \
    -mode "$mode" \
    -carrier "$carrier" \
    -id "$room_id" \
    -key "$key" \
    -link "$link" \
    -transport "$transport" \
    -data "$data_dir" \
    -dns "$dns_server"

if [ -n "$socks_proxy" ]; then
    set -- "$@" -socks-proxy "$socks_proxy" -socks-proxy-port "$socks_proxy_port"
fi

if [ "$transport" = "videochannel" ]; then
    set -- "$@" \
        -video-w "$video_w" \
        -video-h "$video_h" \
        -video-fps "$video_fps" \
        -video-hw "$video_hw" \
        -video-codec "$video_codec" \
        -video-qr-recovery "$video_qr_recovery"

    [ -n "$video_bitrate" ] && set -- "$@" -video-bitrate "$video_bitrate"
    [ "$video_qr_size" -gt 0 ] && set -- "$@" -video-qr-size "$video_qr_size"
    [ "$video_tile_module" -gt 0 ] && set -- "$@" -video-tile-module "$video_tile_module"
    [ "$video_tile_rs" -gt 0 ] && set -- "$@" -video-tile-rs "$video_tile_rs"
fi

if [ "$transport" = "vp8channel" ]; then
    set -- "$@" -vp8-fps "$vp8_fps" -vp8-batch "$vp8_batch"
fi

if bool_flag "${OLCRTC_DEBUG:-}"; then
    set -- "$@" -debug
fi

exec "$@"
