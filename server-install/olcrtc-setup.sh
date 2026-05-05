#!/usr/bin/env bash
# olcRTC server installer & manager for systemd-based Linux VPS.
#
# First run  (service not installed) → full installation & setup
# Repeat run (service installed)     → interactive management menu
#
# Installs:
#   - /usr/local/bin/olcrtc                    statically-linked olcrtc binary
#   - /usr/local/bin/olcrtc-launcher           wrapper that translates env to flags
#   - /etc/systemd/system/olcrtc-server.service hardened systemd unit
#   - /etc/olcrtc/env                          OLCRTC_CARRIER / TRANSPORT / LINK / ROOM_ID / KEY / DNS / etc.
#   - /etc/olcrtc/key.hex                      32-byte encryption key (hex)
#
# Idempotent. Re-run with --regenerate to get a fresh room ID; re-run with
# --regenerate-key to also rotate the encryption key.

set -euo pipefail

INSTALLER_VERSION="0.4.0"
CARRIER_DEFAULT="wbstream"
TRANSPORT_DEFAULT="datachannel"
DNS_DEFAULT="1.1.1.1:53"

REGENERATE_ROOM=0
REGENERATE_KEY=0
CARRIER="${OLCRTC_CARRIER:-${OLCRTC_PROVIDER:-$CARRIER_DEFAULT}}"
TRANSPORT="${OLCRTC_TRANSPORT:-$TRANSPORT_DEFAULT}"
SET_SOCKS_PROXY="__keep__"
SET_DEBUG="__keep__"
SET_TRANSPORT="__keep__"
SET_CARRIER="__keep__"
SET_NAME=""
SET_TELEMOST_ID=""

CONFIG_DIR=/etc/olcrtc
STATE_DIR=/var/lib/olcrtc
ENV_FILE=$CONFIG_DIR/env
KEY_FILE=$CONFIG_DIR/key.hex

# ── Helper: read from terminal even when piped via curl | bash ────────────────
tty_read() {
    if [ -t 0 ]; then
        read "$@"
    else
        read "$@" < /dev/tty
    fi
}

# ── Helper: update a single key in the env file ──────────────────────────────
set_env_value() {
    local key="$1" value="$2"
    local target_file="${3:-$ENV_FILE}"
    if grep -qE "^${key}=" "$target_file" 2>/dev/null; then
        # Build a clean temp file to avoid sed issues with special characters
        local tmpfile
        tmpfile="$(mktemp)"
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                "${key}="*) echo "${key}=${value}" ;;
                *) echo "$line" ;;
            esac
        done < "$target_file" > "$tmpfile"
        cat "$tmpfile" > "$target_file"
        rm -f "$tmpfile"
    else
        echo "${key}=${value}" >> "$target_file"
    fi
}

# ── Helper: read a value from the env file ───────────────────────────────────
get_env_value() {
    local key="$1"
    local target_file="${2:-$ENV_FILE}"
    grep -E "^${key}=" "$target_file" 2>/dev/null | tail -1 | cut -d= -f2- || true
}

# ── Helper: ensure qrencode is available ─────────────────────────────────────
ensure_qrencode() {
    if command -v qrencode >/dev/null 2>&1; then
        return 0
    fi
    echo "[*] Installing qrencode..."
    if command -v apt-get >/dev/null 2>&1; then
        apt-get install -y -qq qrencode >/dev/null 2>&1 || true
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y -q qrencode >/dev/null 2>&1 || true
    elif command -v yum >/dev/null 2>&1; then
        yum install -y -q qrencode >/dev/null 2>&1 || true
    fi
    if ! command -v qrencode >/dev/null 2>&1; then
        echo "[!] Не удалось установить qrencode — QR-код будет недоступен"
        return 1
    fi
    return 0
}

# ── Helper: show URI + QR code ───────────────────────────────────────────────
# Args: <provider> <room_id> <key_hex> <name> [<env_file>]
# When env_file is provided (or defaults to $ENV_FILE), the current transport
# and VP8 settings are read from it and embedded into the URI so that the
# Android client picks up the same transport on QR import. Defaults
# (datachannel) are omitted to keep the URI short and stay compatible with
# the client's own toUri() convention.
show_uri_qr() {
    local provider="$1" room_id="$2" key_hex="$3" name="$4"
    local env_file="${5:-$ENV_FILE}"

    local transport="" vp8_fps="" vp8_batch=""
    if [ -f "$env_file" ]; then
        transport="$(get_env_value OLCRTC_TRANSPORT "$env_file")"
        vp8_fps="$(get_env_value OLCRTC_VP8_FPS "$env_file")"
        vp8_batch="$(get_env_value OLCRTC_VP8_BATCH "$env_file")"
    fi

    local uri="olcrtc://${provider}@room/${room_id}?key=${key_hex}"
    if [ -n "$transport" ] && [ "$transport" != "datachannel" ]; then
        uri="${uri}&transport=${transport}"
        if [ "$transport" = "vp8channel" ]; then
            [ -n "$vp8_fps" ] && uri="${uri}&vp8_fps=${vp8_fps}"
            [ -n "$vp8_batch" ] && uri="${uri}&vp8_batch=${vp8_batch}"
        fi
    fi
    uri="${uri}#${name}"

    echo ""
    echo "  URI:  $uri"
    echo ""
    if command -v qrencode >/dev/null 2>&1; then
        qrencode -t UTF8 "$uri"
    else
        echo "  [!] qrencode не установлен — QR-код недоступен"
    fi
}

# ── Helper: wait for auto-created room ID from journal ───────────────────────
wait_for_room_id() {
    echo "[*] Waiting for $CARRIER to create a room..."
    local detected=""
    local pattern="(WB Stream room created|Jazz room created): "
    for i in $(seq 1 30); do
        sleep 1
        if detected="$(journalctl -u olcrtc-server.service --since '1 minute ago' --no-pager 2>/dev/null \
            | grep -oE "$pattern\\S+" \
            | tail -1 \
            | awk '{print $NF}')"; then
            if [ -n "$detected" ]; then
                ROOM_ID="$detected"
                break
            fi
        fi
    done

    if [ -z "$detected" ] || [ "$ROOM_ID" = "any" ]; then
        echo "[!] Failed to detect a generated room ID within 30s." >&2
        echo "[!] Last 30 log lines:" >&2
        journalctl -u olcrtc-server.service --no-pager -n 30 >&2 || true
        echo "[!] Service is still running (it may auto-recover); inspect logs and re-run with --regenerate." >&2
        return 1
    fi

    set_env_value "OLCRTC_ROOM_ID" "$ROOM_ID"
    echo "[*] Pinned room ID: $ROOM_ID"
    systemctl restart olcrtc-server.service
    sleep 2
    return 0
}

# ── Helper: detect public IP ────────────────────────────────────────────────
get_public_ip() {
    curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null || echo "unknown"
}

# ── Helper: human-readable debug string ──────────────────────────────────────
debug_human() {
    local val="$1"
    if [ -n "$val" ] && [ "$val" != "0" ] && [ "$val" != "false" ]; then
        echo "on (-debug)"
    else
        echo "off"
    fi
}

# ── Helper: human-readable proxy string ──────────────────────────────────────
proxy_human() {
    local val="$1"
    if [ -z "$val" ]; then
        echo "(direct)"
    elif [[ "$val" == *"@"* ]]; then
        echo "${val##*@} (SOCKS5 USER/PASSWORD)"
    else
        echo "$val (SOCKS5 NO_AUTH)"
    fi
}

# ══════════════════════════════════════════════════════════════════════════════
#  UI helpers (colours, separators, status indicators)
# ══════════════════════════════════════════════════════════════════════════════
#
# Colours are emitted only when stdout is a TTY and NO_COLOR is unset, so
# logs / pipes stay clean. Box-drawing uses ASCII fallbacks if the locale
# is not UTF-8.

ui_colors_enabled() {
    [ -t 1 ] && [ -z "${NO_COLOR:-}" ]
}

ui_color() {
    # $1 = code (e.g. "1;32"); prints ANSI escape if colours enabled.
    if ui_colors_enabled; then
        printf '\033[%sm' "$1"
    fi
}

ui_reset()  { ui_color "0"; }
ui_bold()   { ui_color "1"; }
ui_dim()    { ui_color "2"; }
ui_red()    { ui_color "31"; }
ui_green()  { ui_color "32"; }
ui_yellow() { ui_color "33"; }
ui_cyan()   { ui_color "36"; }

# Box-drawing chars + emoji icons; downgrade to ASCII when locale is not UTF-8.
if [[ "${LC_ALL:-${LANG:-}}" =~ [Uu][Tt][Ff]-?8 ]]; then
    UI_HBAR="═"
    UI_HBAR_LIGHT="─"
    UI_BULLET="●"
    UI_ARROW="▸"
    UI_TLEFT="╔"
    UI_TRIGHT="╗"
    UI_BLEFT="╚"
    UI_BRIGHT="╝"
    UI_VBAR="║"
    UI_SECTION_LEFT="┌─["
    UI_SECTION_RIGHT="]"
    UI_PIPE="│"

    # Emoji icons for menu items. No trailing spaces — every call site
    # supplies its own space between icon and text.
    UI_I_LOGO="🌐"
    UI_I_STATUS="📊"
    UI_I_QR="📡"
    UI_I_CARRIER="🛰️"
    UI_I_TRANSPORT="🚀"
    UI_I_ROOM="🔄"
    UI_I_KEY="🔑"
    UI_I_PROXY_ON="🧦"
    UI_I_PROXY_OFF="🚫"
    UI_I_DEBUG="🐛"
    UI_I_RENAME="✏️"
    UI_I_SWITCH="🔁"
    UI_I_INSTANCES="🗂️"
    UI_I_UPDATE="⬇️"
    UI_I_DELETE="🗑️"
    UI_I_EXIT="🚪"
    UI_I_NEW="➕"
    UI_I_RESTART="♻️"
    UI_I_CONFIGURE="🔧"
    UI_I_BACK="⬅️"
    UI_I_IP="📍"
    UI_I_NAME="🏷️"
    UI_I_INSTANCE="📦"
    UI_I_INFO="ℹ️"
    UI_I_SUB="📋"
    UI_I_LINK="🔗"
    UI_I_EXPORT="📤"
    UI_I_IMPORT="📥"
else
    UI_HBAR="="
    UI_HBAR_LIGHT="-"
    UI_BULLET="*"
    UI_ARROW=">"
    UI_TLEFT="+"
    UI_TRIGHT="+"
    UI_BLEFT="+"
    UI_BRIGHT="+"
    UI_VBAR="|"
    UI_SECTION_LEFT="--["
    UI_SECTION_RIGHT="]"
    UI_PIPE="|"

    UI_I_LOGO="[*]"
    UI_I_STATUS="[i]"
    UI_I_QR="[#]"
    UI_I_CARRIER="[c]"
    UI_I_TRANSPORT="[t]"
    UI_I_ROOM="[r]"
    UI_I_KEY="[k]"
    UI_I_PROXY_ON="[+]"
    UI_I_PROXY_OFF="[-]"
    UI_I_DEBUG="[d]"
    UI_I_RENAME="[w]"
    UI_I_SWITCH="[<>]"
    UI_I_INSTANCES="[I]"
    UI_I_UPDATE="[v]"
    UI_I_DELETE="[x]"
    UI_I_EXIT="[q]"
    UI_I_NEW="[+]"
    UI_I_RESTART="[~]"
    UI_I_CONFIGURE="[*]"
    UI_I_BACK="[<]"
    UI_I_IP="[@]"
    UI_I_NAME="[n]"
    UI_I_INSTANCE="[#]"
    UI_I_INFO="[i]"
    UI_I_SUB="[S]"
    UI_I_LINK="[L]"
    UI_I_EXPORT="[E]"
    UI_I_IMPORT="[I]"
fi

# Repeat a 1-column character N times. Used to build separator lines.
ui_repeat() {
    local ch="$1" n="$2" out=""
    local i=0
    while [ "$i" -lt "$n" ]; do
        out="${out}${ch}"
        i=$((i + 1))
    done
    printf '%s' "$out"
}

ui_hbar()       { ui_repeat "$UI_HBAR" "${1:-60}"; }
ui_hbar_light() { ui_repeat "$UI_HBAR_LIGHT" "${1:-60}"; }

# Colour-coded `systemctl is-active` indicator: "● running" / "● failed" / etc.
ui_status_indicator() {
    local svc="$1"
    local status
    status="$(systemctl is-active "$svc" 2>/dev/null || echo "unknown")"
    local color
    case "$status" in
        active)               color="$(ui_green)"; status="running" ;;
        failed)               color="$(ui_red)" ;;
        activating|reloading) color="$(ui_yellow)" ;;
        inactive|deactivating) color="$(ui_dim)" ;;
        *)                    color="$(ui_yellow)" ;;
    esac
    printf '%s%s %s%s' "$color" "$UI_BULLET" "$status" "$(ui_reset)"
}

# Print a section header like:
#   ┌─[ Управление активным инстансом ]─────────────────────
ui_section() {
    local title="$1" width="${2:-60}"
    # Count display width of the title in characters (not bytes) — `wc -m`
    # respects the current UTF-8 locale, so Cyrillic counts as 1 each.
    local title_len
    title_len="$(printf '%s' "$title" | wc -m | tr -d ' ')"
    # layout: "  <SECTION_LEFT> <title> <SECTION_RIGHT><tail>"
    # widths: 2 (indent) + 3 (left) + 1 (space) + title_len + 1 (space)
    #         + 1 (right) + tail
    local fixed=$((2 + 3 + 1 + title_len + 1 + 1))
    local remain=$((width - fixed))
    [ "$remain" -lt 3 ] && remain=3
    local tail
    tail="$(ui_repeat "$UI_HBAR_LIGHT" "$remain")"
    printf '  %s%s %s %s%s%s\n' \
        "$(ui_cyan)" "$UI_SECTION_LEFT" "$title" "$UI_SECTION_RIGHT" "$tail" \
        "$(ui_reset)"
}

# Decorative top banner with title, e.g.:
#   ╔════════════════════════════════════════════════════════╗
#       🌐  olcRTC Server Manager
#   ╚════════════════════════════════════════════════════════╝
ui_banner_top() {
    local width="${1:-60}"
    printf '%s%s%s%s%s\n' \
        "$(ui_bold)$(ui_cyan)" \
        "$UI_TLEFT" "$(ui_repeat "$UI_HBAR" $((width - 2)))" "$UI_TRIGHT" \
        "$(ui_reset)"
}
ui_banner_bottom() {
    local width="${1:-60}"
    printf '%s%s%s%s%s\n' \
        "$(ui_bold)$(ui_cyan)" \
        "$UI_BLEFT" "$(ui_repeat "$UI_HBAR" $((width - 2)))" "$UI_BRIGHT" \
        "$(ui_reset)"
}

# ── Helper: read carrier from env (backward compat OLCRTC_PROVIDER) ──────────
get_carrier() {
    local ef="${1:-$ENV_FILE}"
    local c
    c="$(get_env_value OLCRTC_CARRIER "$ef")"
    [ -z "$c" ] && c="$(get_env_value OLCRTC_PROVIDER "$ef")"
    [ "$c" = "wb_stream" ] && c="wbstream"
    echo "$c"
}

# ── Helper: normalize carrier name (backward compat) ────────────────────────
normalize_carrier() {
    local c="$1"
    case "$c" in
        wb_stream) echo "wbstream" ;;
        *) echo "$c" ;;
    esac
}

# ── Helper: human-readable carrier name ─────────────────────────────────────
carrier_human() {
    case "$1" in
        wbstream)  echo "Wildberries Stream" ;;
        jazz)      echo "SaluteJazz" ;;
        telemost)  echo "Yandex Telemost" ;;
        *) echo "$1" ;;
    esac
}

# ── Helper: human-readable transport name ───────────────────────────────────
transport_human() {
    case "$1" in
        datachannel)  echo "datachannel (~6 МБ/с)" ;;
        vp8channel)   echo "vp8channel (универсальный)" ;;
        seichannel)   echo "seichannel" ;;
        videochannel) echo "videochannel (~200 КБ/с)" ;;
        *) echo "$1" ;;
    esac
}

# ── Helper: carrier↔transport compatibility matrix ──────────────────────────
# Returns 0 if the transport is supported by olcrtc-launcher (the production
# systemd launcher, server-install/systemd/olcrtc-launcher) for the given
# carrier, otherwise 1.
#
# Note: videochannel is intentionally excluded — the production launcher does
# not pass the required -video-w/-video-h/-video-fps/-video-bitrate/-video-hw
# flags, so the binary aborts with ErrVideoWidthRequired and systemd reports
# the service as failed (looks like a crash). Until the launcher is extended,
# the setup script must not let users land in that state.
is_transport_supported() {
    local carrier="$1" transport="$2"
    case "$transport" in
        vp8channel)
            return 0
            ;;
        datachannel|seichannel)
            [ "$carrier" != "telemost" ] && return 0
            return 1
            ;;
        *)
            return 1
            ;;
    esac
}

# ── Helper: pick a sane default transport for a carrier ─────────────────────
# Used when the previously-saved transport becomes incompatible after a
# carrier change. Mirrors is_transport_supported.
default_transport_for() {
    local carrier="$1"
    case "$carrier" in
        telemost) echo "vp8channel" ;;
        *)        echo "datachannel" ;;
    esac
}

# ── Helper: list supported transports for a carrier (newline-separated) ─────
supported_transports_for() {
    local carrier="$1" t
    for t in vp8channel datachannel seichannel; do
        if is_transport_supported "$carrier" "$t"; then
            echo "$t"
        fi
    done
    return 0
}

# ── Helper: interactive transport selection ──────────────────────────────────
# Usage: select_transport <carrier> [current_transport]
# On success sets REPLY_TRANSPORT (and optionally REPLY_VP8_FPS, REPLY_VP8_BATCH)
select_transport() {
    local carrier="$1"
    local current="${2:-}"
    local i=0
    local -a opts=()

    echo "  Транспорт:"

    # vp8channel — works with all carriers
    if is_transport_supported "$carrier" vp8channel; then
        i=$((i+1)); opts+=("vp8channel")
        local tag=""; [ "$current" = "vp8channel" ] && tag=" ←"
        echo "    $i) vp8channel   — универсальный, работает со всеми${tag}"
    fi

    # datachannel — not with telemost
    if is_transport_supported "$carrier" datachannel; then
        i=$((i+1)); opts+=("datachannel")
        tag=""; [ "$current" = "datachannel" ] && tag=" ←"
        echo "    $i) datachannel  — самый быстрый (~6 МБ/с)${tag}"
    fi

    # seichannel — not with telemost
    if is_transport_supported "$carrier" seichannel; then
        i=$((i+1)); opts+=("seichannel")
        tag=""; [ "$current" = "seichannel" ] && tag=" ←"
        echo "    $i) seichannel   — для wbstream/jazz${tag}"
    fi

    # videochannel — listed but intentionally not selectable: the systemd
    # launcher does not pass -video-* flags, so picking it crashes the
    # service. Keep the line visible so users see the transport exists.
    echo "    -) videochannel — недоступно (пока не поддерживается systemd-лаунчером)"

    echo ""
    local dl=""
    [ -n "$current" ] && dl=", Enter = оставить"
    tty_read -rp "  Выберите [1-$i${dl}]: " tc

    if [ -z "$tc" ] && [ -n "$current" ]; then
        REPLY_TRANSPORT="$current"
        return 0
    fi

    if [ "$tc" -ge 1 ] 2>/dev/null && [ "$tc" -le "$i" ] 2>/dev/null; then
        REPLY_TRANSPORT="${opts[$((tc-1))]}"
    else
        REPLY_TRANSPORT=""
        return 1
    fi

    # Ask vp8 options if vp8channel selected
    REPLY_VP8_FPS=""
    REPLY_VP8_BATCH=""
    if [ "$REPLY_TRANSPORT" = "vp8channel" ]; then
        tty_read -rp "  VP8 FPS [Enter = 60]: " vfps
        REPLY_VP8_FPS="${vfps:-60}"
        tty_read -rp "  VP8 batch size [Enter = 8]: " vbatch
        REPLY_VP8_BATCH="${vbatch:-8}"
    fi
    return 0
}

# ── Multi-instance helpers ───────────────────────────────────────────────────

MAX_EXTRA_INSTANCES=20

list_instances() {
    # Основной
    if [ -f /etc/olcrtc/env ]; then
        echo "0"
    fi
    # Дополнительные: /etc/olcrtc/<N>/env
    for d in /etc/olcrtc/*/env; do
        [ -f "$d" ] || continue
        local n
        n="$(basename "$(dirname "$d")")"
        [[ "$n" =~ ^[0-9]+$ ]] && echo "$n"
    done
}

instance_env_file() {
    local n="$1"
    if [ "$n" = "0" ]; then
        echo "/etc/olcrtc/env"
    else
        echo "/etc/olcrtc/$n/env"
    fi
}

instance_key_file() {
    local n="$1"
    if [ "$n" = "0" ]; then
        echo "/etc/olcrtc/key.hex"
    else
        echo "/etc/olcrtc/$n/key.hex"
    fi
}

instance_service() {
    local n="$1"
    if [ "$n" = "0" ]; then
        echo "olcrtc-server.service"
    else
        echo "olcrtc-server@${n}.service"
    fi
}

instance_label() {
    local n="$1"
    if [ "$n" = "0" ]; then
        echo "основной"
    else
        echo "#$n"
    fi
}

next_instance_id() {
    local max=1
    for d in /etc/olcrtc/*/env; do
        [ -f "$d" ] || continue
        local n
        n="$(basename "$(dirname "$d")")"
        [[ "$n" =~ ^[0-9]+$ ]] && [ "$n" -gt "$max" ] && max="$n"
    done
    echo $((max + 1))
}

instance_count() {
    local count=0
    [ -f /etc/olcrtc/env ] && count=1
    for d in /etc/olcrtc/*/env; do
        [ -f "$d" ] || continue
        local n
        n="$(basename "$(dirname "$d")")"
        [[ "$n" =~ ^[0-9]+$ ]] && count=$((count + 1))
    done
    echo "$count"
}

extra_instance_count() {
    local count=0
    for d in /etc/olcrtc/*/env; do
        [ -f "$d" ] || continue
        local n
        n="$(basename "$(dirname "$d")")"
        [[ "$n" =~ ^[0-9]+$ ]] && count=$((count + 1))
    done
    echo "$count"
}

# Wait for auto-created room ID for a specific service instance
wait_for_room_id_for() {
    local svc="$1" env_f="$2" prov="$3"
    echo "[*] Waiting for $prov to create a room..."
    local detected=""
    local pattern="(WB Stream room created|Jazz room created): "
    for i in $(seq 1 30); do
        sleep 1
        if detected="$(journalctl -u "$svc" --since '1 minute ago' --no-pager 2>/dev/null \
            | grep -oE "$pattern\\S+" \
            | tail -1 \
            | awk '{print $NF}')"; then
            if [ -n "$detected" ]; then
                break
            fi
        fi
    done

    if [ -z "$detected" ]; then
        echo "[!] Failed to detect a generated room ID within 30s." >&2
        journalctl -u "$svc" --no-pager -n 30 >&2 || true
        return 1
    fi

    set_env_value "OLCRTC_ROOM_ID" "$detected" "$env_f"
    echo "[*] Pinned room ID: $detected"
    systemctl restart "$svc"
    sleep 2
    return 0
}

# Install the systemd template unit for extra instances (idempotent)
install_template_unit() {
    cat > /etc/systemd/system/olcrtc-server@.service <<'TMPLUNIT'
[Unit]
Description=olcRTC server instance %i
Documentation=https://github.com/openlibrecommunity/olcrtc
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=10

[Service]
Type=exec
EnvironmentFile=/etc/olcrtc/%i/env
User=olcrtc
Group=olcrtc
StateDirectory=olcrtc-%i
StateDirectoryMode=0750
WorkingDirectory=/var/lib/olcrtc-%i
ExecStart=/usr/local/bin/olcrtc-launcher

ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true

Restart=on-failure
RestartSec=5s

StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
TMPLUNIT
    systemctl daemon-reload
}

# Remove template unit if no extra instances remain
maybe_remove_template_unit() {
    if [ "$(extra_instance_count)" -eq 0 ]; then
        rm -f /etc/systemd/system/olcrtc-server@.service
        systemctl daemon-reload
    fi
}

# ──────────────────────────────────────────────────────────────────────────────
#  CLI argument parsing
# ──────────────────────────────────────────────────────────────────────────────

usage() {
    cat <<EOF
Usage: sudo ./olcrtc-setup.sh [options]

Options:
    --carrier <wbstream|telemost|jazz>
                          Pick a carrier (default: $CARRIER_DEFAULT).
    --provider <name>     Deprecated alias for --carrier.
    --transport <datachannel|vp8channel|seichannel>
                          Pick a transport (default: $TRANSPORT_DEFAULT).
                          Note: videochannel is not yet supported by the
                          systemd launcher and is intentionally rejected
                          here to avoid crashing the service.
    --regenerate          Drop the saved room ID and create a new one.
    --regenerate-key      Drop the encryption key AND room ID, regenerate both.
    --socks-proxy <[user:pass@]host:port>
                          Route carrier signalling through a SOCKS5 proxy.
                          Pass "" to remove an existing setting.
    --debug               Enable verbose -debug logging.
    --no-debug            Disable verbose logging.
    --name <string>       Connection name shown in the client.
    --id <room_id>        Room ID for Telemost.
    -h, --help            Show this help.

Examples:
    sudo ./olcrtc-setup.sh
    sudo ./olcrtc-setup.sh --carrier telemost --transport vp8channel --id 12345
    sudo ./olcrtc-setup.sh --carrier jazz --transport datachannel --regenerate
    sudo ./olcrtc-setup.sh --socks-proxy 1.2.3.4:1080
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --carrier) CARRIER="$(normalize_carrier "$2")"; SET_CARRIER="$CARRIER"; shift 2 ;;
        --carrier=*) CARRIER="$(normalize_carrier "${1#*=}")"; SET_CARRIER="$CARRIER"; shift ;;
        --provider) CARRIER="$(normalize_carrier "$2")"; SET_CARRIER="$CARRIER"; shift 2 ;;
        --provider=*) CARRIER="$(normalize_carrier "${1#*=}")"; SET_CARRIER="$CARRIER"; shift ;;
        --transport) SET_TRANSPORT="$2"; shift 2 ;;
        --transport=*) SET_TRANSPORT="${1#*=}"; shift ;;
        --regenerate) REGENERATE_ROOM=1; shift ;;
        --regenerate-key) REGENERATE_KEY=1; REGENERATE_ROOM=1; shift ;;
        --socks-proxy) SET_SOCKS_PROXY="$2"; shift 2 ;;
        --socks-proxy=*) SET_SOCKS_PROXY="${1#*=}"; shift ;;
        --debug) SET_DEBUG="1"; shift ;;
        --no-debug) SET_DEBUG=""; shift ;;
        --name) SET_NAME="$2"; shift 2 ;;
        --name=*) SET_NAME="${1#*=}"; shift ;;
        --id) SET_TELEMOST_ID="$2"; shift 2 ;;
        --id=*) SET_TELEMOST_ID="${1#*=}"; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

# Normalize carrier (backward compat wb_stream → wbstream)
CARRIER="$(normalize_carrier "$CARRIER")"

# ── Pre-flight checks ────────────────────────────────────────────────────────

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] This script must be run as root (try: sudo $0)" >&2
    exit 1
fi

case "$CARRIER" in
    telemost|jazz|wbstream) ;;
    *) echo "[!] Unsupported carrier: $CARRIER" >&2; exit 1 ;;
esac

if ! command -v systemctl >/dev/null 2>&1; then
    echo "[!] systemd is required but not found." >&2
    exit 1
fi

# ──────────────────────────────────────────────────────────────────────────────
#  Detect mode: install or manage
# ──────────────────────────────────────────────────────────────────────────────

is_installed() {
    [ -f /usr/local/bin/olcrtc ] && \
    [ -f /etc/systemd/system/olcrtc-server.service ] && \
    [ -f "$ENV_FILE" ] && return 0
    return 1
}

# ══════════════════════════════════════════════════════════════════════════════
#  SUBSCRIPTION HELPERS
# ══════════════════════════════════════════════════════════════════════════════

# Generate a random 6-character slug (A-Za-z0-9).
generate_slug() {
    local chars="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
    local slug=""
    local i=0
    while [ "$i" -lt 6 ]; do
        local idx=$(( RANDOM % 62 ))
        slug="${slug}${chars:$idx:1}"
        i=$((i + 1))
    done
    echo "$slug"
}

# Derive the subscription API base URL from the env file. Reads
# OLCRTC_SUB_PORT (default 2096).
sub_api_base() {
    local ef="${1:-$ENV_FILE}"
    local port
    port="$(get_env_value OLCRTC_SUB_PORT "$ef")"
    [ -z "$port" ] && port="2096"
    echo "http://127.0.0.1:${port}"
}

# Check if subscription server is reachable.
sub_is_running() {
    local base
    base="$(sub_api_base "$1")"
    curl -sf --max-time 2 "${base}/api/subscriptions" >/dev/null 2>&1
}

# ══════════════════════════════════════════════════════════════════════════════
#  SUBSCRIPTION MANAGEMENT SUBMENU
# ══════════════════════════════════════════════════════════════════════════════

run_subscription_menu() {
    local BASE
    BASE="$(sub_api_base)"

    if ! sub_is_running; then
        echo ""
        echo "  [!] Сервер подписок не запущен или недоступен."
        echo "      Убедитесь, что OLCRTC_SUB_ENABLED=1 в env и сервис запущен."
        echo ""
        tty_read -rp "[Enter для продолжения]"
        return
    fi

    while true; do
        echo ""
        ui_banner_top 60
        printf '%s   %s  %solcRTC — Управление подписками%s\n' \
            "$(ui_bold)$(ui_cyan)" "$UI_I_SUB" "$(ui_bold)" "$(ui_reset)"
        ui_banner_bottom 60
        echo ""

        # Show existing subscriptions
        ui_section "Подписки"
        echo ""
        local subs_json
        subs_json="$(curl -sf "${BASE}/api/subscriptions" 2>/dev/null)" || subs_json="[]"
        if [ "$subs_json" = "null" ] || [ "$subs_json" = "[]" ]; then
            echo "    (нет подписок)"
        else
            # Parse JSON array with lightweight awk — each subscription on one line.
            echo "$subs_json" | python3 -c "
import sys, json
try:
    subs = json.load(sys.stdin)
    if not subs:
        print('    (нет подписок)')
    else:
        for s in subs:
            slug = s.get('slug','')
            name = s.get('name','')
            print(f'    {slug}  —  {name}')
except:
    print('    (ошибка чтения)')
" 2>/dev/null || echo "    (ошибка чтения JSON)"
        fi
        echo ""

        ui_section "Действия"
        echo ""
        printf '   %s   1) Список подписок\n'                       "$UI_I_SUB"
        printf '   %s   2) Создать подписку\n'                      "$UI_I_NEW"
        printf '   %s   3) Добавить инстанс в подписку\n'           "$UI_I_LINK"
        printf '   %s   4) Убрать инстанс из подписки\n'            "$UI_I_DELETE"
        printf '   %s   5) Открепить все инстансы от подписки\n'    "$UI_I_DELETE"
        printf '   %s   6) Удалить подписку\n'                      "$UI_I_DELETE"
        printf '   %s   7) Экспорт подписок в JSON\n'               "$UI_I_EXPORT"
        printf '   %s   8) Импорт подписок из JSON\n'               "$UI_I_IMPORT"
        echo ""
        printf '   %s   0) Назад\n'                                 "$UI_I_BACK"
        echo ""
        tty_read -rp "$(ui_bold)→$(ui_reset) Выберите пункт: " schoice

        case "$schoice" in
        1)  # Список подписок с инстансами
            echo ""
            ui_section "Все подписки"
            echo ""
            local all_subs
            all_subs="$(curl -sf "${BASE}/api/subscriptions" 2>/dev/null)" || all_subs="[]"
            python3 -c "
import sys, json, urllib.request
base = sys.argv[1]
try:
    subs = json.loads(sys.argv[2])
    if not subs:
        print('    (нет подписок)')
        sys.exit(0)
    for s in subs:
        slug = s['slug']
        name = s['name']
        print(f'  [{slug}] {name}')
        try:
            req = urllib.request.Request(f'{base}/api/subscriptions/{slug}/instances')
            with urllib.request.urlopen(req, timeout=3) as resp:
                insts = json.loads(resp.read())
            if not insts:
                print('      (нет инстансов)')
            else:
                for inst in insts:
                    iid = inst.get('id','')
                    uri = inst.get('raw_uri','')
                    label = inst.get('label','')
                    lbl = f' ({label})' if label else ''
                    # Truncate long URIs for display
                    disp = uri if len(uri) <= 80 else uri[:77] + '...'
                    print(f'      #{iid}{lbl}: {disp}')
        except:
            print('      (ошибка чтения инстансов)')
        print()
except Exception as e:
    print(f'    Ошибка: {e}')
" "$BASE" "$all_subs" 2>/dev/null || echo "    (ошибка)"
            tty_read -rp "[Enter для продолжения]"
            ;;

        2)  # Создать подписку
            echo ""
            tty_read -rp "  Имя подписки: " sub_name
            if [ -z "$sub_name" ]; then
                echo "  [!] Имя не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local sub_slug
            tty_read -rp "  Slug (Enter = сгенерировать автоматически): " sub_slug
            if [ -z "$sub_slug" ]; then
                sub_slug="$(generate_slug)"
            fi
            local create_resp
            create_resp="$(curl -sf -X POST "${BASE}/api/subscriptions" \
                -H 'Content-Type: application/json' \
                -d "{\"name\":\"${sub_name}\",\"slug\":\"${sub_slug}\"}" 2>&1)"
            local create_code=$?
            if [ $create_code -ne 0 ]; then
                echo "  [!] Ошибка создания подписки: $create_resp"
            else
                echo ""
                echo "  Подписка создана:"
                echo "    Slug: $sub_slug"
                echo "    Имя:  $sub_name"
                local pub_ip
                pub_ip="$(get_public_ip)"
                local sub_port
                sub_port="$(get_env_value OLCRTC_SUB_PORT)"
                [ -z "$sub_port" ] && sub_port="2096"
                echo "    URL:  http://${pub_ip}:${sub_port}/sub/${sub_slug}"
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        3)  # Добавить инстанс в подписку
            echo ""
            tty_read -rp "  Slug подписки: " add_slug
            if [ -z "$add_slug" ]; then
                echo "  [!] Slug не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            echo ""
            echo "  Введите полную olcrtc:// URI (можно скопировать из вывода QR-кода):"
            tty_read -rp "  URI: " add_uri
            if [ -z "$add_uri" ]; then
                echo "  [!] URI не может быть пустой"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local add_resp
            add_resp="$(curl -sf -X POST "${BASE}/api/subscriptions/${add_slug}/instances" \
                -H 'Content-Type: application/json' \
                -d "{\"raw_uri\":\"${add_uri}\"}" 2>&1)"
            if [ $? -ne 0 ]; then
                echo "  [!] Ошибка добавления инстанса: $add_resp"
            else
                echo "  Инстанс добавлен в подписку [$add_slug]."
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        4)  # Удалить инстанс из подписки
            echo ""
            tty_read -rp "  Slug подписки: " ri_slug
            if [ -z "$ri_slug" ]; then
                echo "  [!] Slug не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            # Show instances
            local ri_insts
            ri_insts="$(curl -sf "${BASE}/api/subscriptions/${ri_slug}/instances" 2>/dev/null)"
            if [ -z "$ri_insts" ] || [ "$ri_insts" = "null" ] || [ "$ri_insts" = "[]" ]; then
                echo "  (нет инстансов или подписка не найдена)"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            python3 -c "
import sys, json
insts = json.loads(sys.argv[1])
for inst in insts:
    iid = inst.get('id','')
    uri = inst.get('raw_uri','')
    label = inst.get('label','')
    lbl = f' ({label})' if label else ''
    disp = uri if len(uri) <= 80 else uri[:77] + '...'
    print(f'    #{iid}{lbl}: {disp}')
" "$ri_insts" 2>/dev/null || echo "    (ошибка)"
            echo ""
            tty_read -rp "  ID инстанса для удаления: " ri_id
            if [ -z "$ri_id" ]; then
                echo "  [!] ID не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            curl -sf -X DELETE "${BASE}/api/subscriptions/${ri_slug}/instances/${ri_id}" >/dev/null 2>&1
            if [ $? -eq 0 ]; then
                echo "  Инстанс #${ri_id} удалён."
            else
                echo "  [!] Ошибка удаления инстанса"
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        5)  # Открепить все инстансы от подписки
            echo ""
            tty_read -rp "  Slug подписки: " da_slug
            if [ -z "$da_slug" ]; then
                echo "  [!] Slug не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            tty_read -rp "  Открепить все инстансы от подписки [$da_slug]? [y/N] " da_confirm
            if [ "$da_confirm" != "y" ] && [ "$da_confirm" != "Y" ]; then
                echo "  Отменено."
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local da_resp
            da_resp="$(curl -sf -X DELETE "${BASE}/api/subscriptions/${da_slug}?detach=true" 2>/dev/null)"
            if [ $? -eq 0 ]; then
                echo "  Все инстансы откреплены от подписки [$da_slug]."
                echo "  Подписка сохранена (пустая)."
            else
                echo "  [!] Ошибка: $da_resp"
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        6)  # Удалить подписку
            echo ""
            tty_read -rp "  Slug подписки для удаления: " ds_slug
            if [ -z "$ds_slug" ]; then
                echo "  [!] Slug не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            # Check if subscription has instances
            local ds_insts
            ds_insts="$(curl -sf "${BASE}/api/subscriptions/${ds_slug}/instances" 2>/dev/null)" || ds_insts="[]"
            local ds_count
            ds_count="$(echo "$ds_insts" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null)" || ds_count="0"

            if [ "$ds_count" != "0" ] && [ "$ds_count" != "" ]; then
                echo ""
                echo "  В подписке [$ds_slug] есть $ds_count инстанс(ов)."
                echo "    1) Удалить подписку вместе с инстансами"
                echo "    2) Открепить инстансы, затем удалить подписку"
                echo "    0) Отмена"
                echo ""
                tty_read -rp "  Выберите действие: " ds_action
                case "$ds_action" in
                1)
                    curl -sf -X DELETE "${BASE}/api/subscriptions/${ds_slug}" >/dev/null 2>&1
                    if [ $? -eq 0 ]; then
                        echo "  Подписка [$ds_slug] и все инстансы удалены."
                    else
                        echo "  [!] Ошибка удаления подписки"
                    fi
                    ;;
                2)
                    curl -sf -X DELETE "${BASE}/api/subscriptions/${ds_slug}?detach=true" >/dev/null 2>&1
                    curl -sf -X DELETE "${BASE}/api/subscriptions/${ds_slug}" >/dev/null 2>&1
                    if [ $? -eq 0 ]; then
                        echo "  Инстансы откреплены, подписка [$ds_slug] удалена."
                    else
                        echo "  [!] Ошибка удаления подписки"
                    fi
                    ;;
                *)
                    echo "  Отменено."
                    ;;
                esac
            else
                tty_read -rp "  Удалить подписку [$ds_slug]? [y/N] " ds_confirm
                if [ "$ds_confirm" = "y" ] || [ "$ds_confirm" = "Y" ]; then
                    curl -sf -X DELETE "${BASE}/api/subscriptions/${ds_slug}" >/dev/null 2>&1
                    if [ $? -eq 0 ]; then
                        echo "  Подписка [$ds_slug] удалена."
                    else
                        echo "  [!] Ошибка удаления подписки"
                    fi
                else
                    echo "  Отменено."
                fi
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        7)  # Экспорт подписок
            echo ""
            local export_default="/tmp/olcrtc-subscriptions.json"
            tty_read -rp "  Файл для экспорта [Enter = $export_default]: " export_file
            [ -z "$export_file" ] && export_file="$export_default"
            if curl -sf "${BASE}/api/export" -o "$export_file" 2>/dev/null; then
                echo "  Подписки экспортированы в: $export_file"
            else
                echo "  [!] Ошибка экспорта"
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        8)  # Импорт подписок
            echo ""
            tty_read -rp "  Файл для импорта: " import_file
            if [ -z "$import_file" ] || [ ! -f "$import_file" ]; then
                echo "  [!] Файл не найден: $import_file"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            tty_read -rp "  Перезаписать существующие подписки с совпадающим slug? [y/N] " imp_overwrite
            local imp_ow_param=""
            if [ "$imp_overwrite" = "y" ] || [ "$imp_overwrite" = "Y" ]; then
                imp_ow_param="?overwrite=true"
            fi
            local imp_resp
            imp_resp="$(curl -sf -X POST "${BASE}/api/import${imp_ow_param}" \
                -H 'Content-Type: application/json' \
                -d @"$import_file" 2>&1)"
            if [ $? -eq 0 ]; then
                echo "  Результат: $imp_resp"
            else
                echo "  [!] Ошибка импорта: $imp_resp"
            fi
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        0)  return ;;

        *)  echo "  [!] Неверный пункт меню" ;;
        esac
    done
}

# ══════════════════════════════════════════════════════════════════════════════
#  INSTANCE MANAGEMENT SUBMENU
# ══════════════════════════════════════════════════════════════════════════════

run_instance_menu() {
    while true; do
        echo ""
        ui_banner_top 60
        printf '%s   %s  %solcRTC — Управление инстансами%s\n' \
            "$(ui_bold)$(ui_cyan)" "$UI_I_INSTANCES" "$(ui_bold)" "$(ui_reset)"
        ui_banner_bottom 60
        echo ""
        ui_section "Активные инстансы"
        echo ""
        local inst_n inst_ef inst_svc inst_carr inst_trans inst_room inst_name inst_lbl
        for inst_n in $(list_instances); do
            inst_ef="$(instance_env_file "$inst_n")"
            inst_svc="$(instance_service "$inst_n")"
            inst_lbl="$(instance_label "$inst_n")"
            inst_carr="$(get_carrier "$inst_ef")"
            inst_trans="$(get_env_value OLCRTC_TRANSPORT "$inst_ef")"
            [ -z "$inst_trans" ] && inst_trans="datachannel"
            inst_room="$(get_env_value OLCRTC_ROOM_ID "$inst_ef")"
            inst_name="$(get_env_value OLCRTC_NAME "$inst_ef")"
            [ -z "$inst_name" ] && inst_name="${inst_carr}_olcrtc"
            # Build a status indicator block (with colour) plus a plain table row.
            printf "    %s  %s %-10s  %s %-12s  %s Room: %-12s  %s%s[%s]%s\n" \
                "$(ui_status_indicator "$inst_svc")" \
                "$UI_I_CARRIER" "$inst_carr" \
                "$UI_I_TRANSPORT" "$inst_trans" \
                "$UI_I_ROOM" "$inst_room" \
                "$UI_I_INSTANCE" "$(ui_dim)" "$inst_lbl" "$(ui_reset)"
        done
        echo ""
        ui_section "Действия"
        echo ""
        printf '   %s   1) Создать новый инстанс\n'                  "$UI_I_NEW"
        printf '   %s   2) Показать URI / QR-код инстанса\n'         "$UI_I_QR"
        printf '   %s   3) Настроить инстанс  %s(carrier, транспорт, прокси, debug, имя)%s\n' \
            "$UI_I_CONFIGURE" "$(ui_dim)" "$(ui_reset)"
        printf '   %s   4) Перезапустить инстанс\n'                  "$UI_I_RESTART"
        printf '   %s   5) %sУдалить инстанс%s\n'                    "$UI_I_DELETE" "$(ui_red)" "$(ui_reset)"
        printf '   %s   6) Статус всех инстансов\n'                  "$UI_I_STATUS"
        echo ""
        printf '   %s   0) Назад\n' "$UI_I_BACK"
        echo ""
        tty_read -rp "$(ui_bold)→$(ui_reset) Выберите пункт: " ichoice

        case "$ichoice" in
        1)  # ── Создать новый инстанс ──
            local new_id
            new_id="$(next_instance_id)"
            if [ "$new_id" -gt $((MAX_EXTRA_INSTANCES + 1)) ]; then
                echo "  [!] Достигнут лимит дополнительных инстансов ($MAX_EXTRA_INSTANCES)"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi

            echo ""
            echo "  Создание инстанса #$new_id"
            echo ""

            # Carrier
            echo "  Carrier (сервис):"
            echo "    1) wbstream  — Wildberries Stream"
            echo "    2) jazz      — SaluteJazz"
            echo "    3) telemost  — Yandex Telemost"
            echo ""
            local main_carr
            main_carr="$(get_carrier)"
            tty_read -rp "  Carrier [1-3, Enter = как основной ($main_carr)]: " pc
            local new_carr=""
            case "$pc" in
                1) new_carr="wbstream" ;;
                2) new_carr="jazz" ;;
                3) new_carr="telemost" ;;
                "") new_carr="$main_carr" ;;
                *) echo "  [!] Неверный выбор"; tty_read -rp "[Enter для продолжения]"; continue ;;
            esac

            # Transport
            echo ""
            local main_trans
            main_trans="$(get_env_value OLCRTC_TRANSPORT)"
            [ -z "$main_trans" ] && main_trans="$TRANSPORT_DEFAULT"
            if ! select_transport "$new_carr" "$main_trans"; then
                echo "  [!] Неверный выбор транспорта"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local new_trans="$REPLY_TRANSPORT"
            local new_vp8_fps="${REPLY_VP8_FPS:-}"
            local new_vp8_batch="${REPLY_VP8_BATCH:-}"

            # Name
            local new_name_default="${new_carr}_olcrtc_${new_id}"
            tty_read -rp "  Имя соединения [Enter = $new_name_default]: " new_name
            [ -z "$new_name" ] && new_name="$new_name_default"

            # SOCKS proxy
            local main_proxy new_proxy=""
            main_proxy="$(get_env_value OLCRTC_SOCKS_PROXY)"
            if [ -n "$main_proxy" ]; then
                tty_read -rp "  SOCKS5-прокси [Enter = как основной ($main_proxy), n = без прокси]: " proxy_ans
                case "$proxy_ans" in
                    n|N) new_proxy="" ;;
                    "") new_proxy="$main_proxy" ;;
                    *) new_proxy="$proxy_ans" ;;
                esac
            else
                tty_read -rp "  SOCKS5-прокси [user:pass@]host:port (Enter = без прокси): " proxy_ans
                new_proxy="$proxy_ans"
            fi

            # WARP proxy
            local main_warp new_warp=""
            main_warp="$(get_env_value OLCRTC_WARP_PROXY)"
            if [ -n "$main_warp" ]; then
                tty_read -rp "  WARP-прокси [Enter = как основной ($main_warp), n = без WARP]: " warp_ans
                case "$warp_ans" in
                    n|N) new_warp="" ;;
                    "") new_warp="$main_warp" ;;
                    *) new_warp="$warp_ans" ;;
                esac
            fi

            # Create directories
            echo "[*] Создаю директории для инстанса #$new_id..."
            install -d -m 0750 -o root -g olcrtc "/etc/olcrtc/$new_id"
            install -d -m 0750 -o olcrtc -g olcrtc "/var/lib/olcrtc-$new_id"

            # Generate key
            echo "[*] Генерирую ключ шифрования..."
            umask 077
            local new_kf="/etc/olcrtc/$new_id/key.hex"
            openssl rand -hex 32 > "$new_kf"
            chown root:olcrtc "$new_kf"
            chmod 0640 "$new_kf"
            local new_key
            new_key="$(cat "$new_kf")"

            # Room ID
            local new_room="any"
            if [ "$new_carr" = "telemost" ]; then
                tty_read -rp "  Введите Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            # Write env file
            local new_ef="/etc/olcrtc/$new_id/env"
            cat > "$new_ef" <<IEOF
# Managed by olcrtc-setup.sh — instance #$new_id
OLCRTC_CARRIER=$new_carr
OLCRTC_TRANSPORT=$new_trans
OLCRTC_LINK=direct
OLCRTC_ROOM_ID=$new_room
OLCRTC_KEY=$new_key
OLCRTC_DNS=$DNS_DEFAULT
OLCRTC_DEBUG=
OLCRTC_SOCKS_PROXY=$new_proxy
OLCRTC_WARP_PROXY=$new_warp
OLCRTC_NAME=$new_name
OLCRTC_VP8_FPS=$new_vp8_fps
OLCRTC_VP8_BATCH=$new_vp8_batch
IEOF
            chown root:olcrtc "$new_ef"
            chmod 0640 "$new_ef"

            # Ensure template unit
            install_template_unit

            # Enable & start
            local new_svc
            new_svc="$(instance_service "$new_id")"
            echo "[*] Запускаю $new_svc..."
            systemctl enable --quiet "$new_svc"
            systemctl start "$new_svc"

            # Wait for room ID
            if [ "$new_room" = "any" ]; then
                if ! wait_for_room_id_for "$new_svc" "$new_ef" "$new_carr"; then
                    echo "  [!] Сервис запущен, но room ID не определён. Проверьте логи."
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            # Show result
            local final_room final_key
            final_room="$(get_env_value OLCRTC_ROOM_ID "$new_ef")"
            final_key="$(get_env_value OLCRTC_KEY "$new_ef")"
            echo ""
            echo "  Инстанс #$new_id создан."
            show_uri_qr "$new_carr" "$final_room" "$final_key" "$new_name" "$new_ef"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        2)  # ── Показать URI / QR-код инстанса ──
            echo ""
            echo "  Выберите инстанс:"
            for inst_n in $(list_instances); do
                inst_ef="$(instance_env_file "$inst_n")"
                inst_lbl="$(instance_label "$inst_n")"
                inst_carr="$(get_carrier "$inst_ef")"
                inst_room="$(get_env_value OLCRTC_ROOM_ID "$inst_ef")"
                printf "    %s) [%s] %s | Room: %s\n" "$inst_n" "$inst_lbl" "$inst_carr" "$inst_room"
            done
            echo ""
            tty_read -rp "  Номер инстанса (0 = основной): " sel_n
            local sel_ef
            sel_ef="$(instance_env_file "$sel_n")"
            if [ ! -f "$sel_ef" ]; then
                echo "  [!] Инстанс не найден"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local sel_carr sel_room sel_key sel_name
            sel_carr="$(get_carrier "$sel_ef")"
            sel_room="$(get_env_value OLCRTC_ROOM_ID "$sel_ef")"
            sel_key="$(get_env_value OLCRTC_KEY "$sel_ef")"
            sel_name="$(get_env_value OLCRTC_NAME "$sel_ef")"
            [ -z "$sel_name" ] && sel_name="${sel_carr}_olcrtc"
            show_uri_qr "$sel_carr" "$sel_room" "$sel_key" "$sel_name" "$sel_ef"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        3)  # ── Настроить инстанс ──
            echo ""
            echo "  Выберите инстанс для настройки:"
            for inst_n in $(list_instances); do
                inst_ef="$(instance_env_file "$inst_n")"
                inst_lbl="$(instance_label "$inst_n")"
                inst_carr="$(get_carrier "$inst_ef")"
                inst_trans="$(get_env_value OLCRTC_TRANSPORT "$inst_ef")"
                [ -z "$inst_trans" ] && inst_trans="datachannel"
                printf "    %s) [%s] %s / %s\n" "$inst_n" "$inst_lbl" "$inst_carr" "$inst_trans"
            done
            echo ""
            tty_read -rp "  Номер инстанса: " cfg_n
            local cfg_ef
            cfg_ef="$(instance_env_file "$cfg_n")"
            if [ ! -f "$cfg_ef" ]; then
                echo "  [!] Инстанс не найден"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local cfg_svc
            cfg_svc="$(instance_service "$cfg_n")"
            local cfg_lbl
            cfg_lbl="$(instance_label "$cfg_n")"

            echo ""
            ui_section "Настройка инстанса [$cfg_lbl]"
            echo ""
            printf '   %s   1) Сменить carrier\n'             "$UI_I_CARRIER"
            printf '   %s   2) Сменить транспорт\n'           "$UI_I_TRANSPORT"
            printf '   %s   3) Настроить SOCKS5-прокси\n'     "$UI_I_PROXY_ON"
            printf '   %s   4) Убрать SOCKS5-прокси\n'        "$UI_I_PROXY_OFF"
            printf '   %s   5) Включить / выключить debug\n'  "$UI_I_DEBUG"
            printf '   %s   6) Переименовать\n'               "$UI_I_RENAME"
            echo ""
            printf '   %s   0) Назад\n'                       "$UI_I_BACK"
            echo ""
            tty_read -rp "$(ui_bold)→$(ui_reset) Выберите: " cfg_choice

            case "$cfg_choice" in
            1)  # Сменить carrier
                echo ""
                echo "  Carrier (сервис):"
                echo "    1) wbstream  — Wildberries Stream"
                echo "    2) jazz      — SaluteJazz"
                echo "    3) telemost  — Yandex Telemost"
                echo ""
                tty_read -rp "  Carrier [1-3]: " cpc
                local cfg_carr=""
                case "$cpc" in
                    1) cfg_carr="wbstream" ;;
                    2) cfg_carr="jazz" ;;
                    3) cfg_carr="telemost" ;;
                    *) echo "  [!] Неверный выбор"; tty_read -rp "[Enter для продолжения]"; continue ;;
                esac
                set_env_value "OLCRTC_CARRIER" "$cfg_carr" "$cfg_ef"
                # Check transport compatibility
                local cfg_cur_trans cfg_new_trans
                cfg_cur_trans="$(get_env_value OLCRTC_TRANSPORT "$cfg_ef")"
                [ -z "$cfg_cur_trans" ] && cfg_cur_trans="datachannel"
                if ! is_transport_supported "$cfg_carr" "$cfg_cur_trans"; then
                    cfg_new_trans="$(default_transport_for "$cfg_carr")"
                    echo "  [!] Транспорт $cfg_cur_trans не совместим с $cfg_carr, переключаю на $cfg_new_trans"
                    set_env_value "OLCRTC_TRANSPORT" "$cfg_new_trans" "$cfg_ef"
                fi
                if [ "$cfg_carr" = "telemost" ]; then
                    tty_read -rp "  Введите Room ID для Telemost: " cfg_room
                    if [ -z "$cfg_room" ]; then
                        echo "  [!] Room ID не может быть пустым"
                        tty_read -rp "[Enter для продолжения]"
                        continue
                    fi
                    set_env_value "OLCRTC_ROOM_ID" "$cfg_room" "$cfg_ef"
                else
                    set_env_value "OLCRTC_ROOM_ID" "any" "$cfg_ef"
                fi
                systemctl restart "$cfg_svc"
                if [ "$(get_env_value OLCRTC_ROOM_ID "$cfg_ef")" = "any" ]; then
                    if ! wait_for_room_id_for "$cfg_svc" "$cfg_ef" "$cfg_carr"; then
                        tty_read -rp "[Enter для продолжения]"
                        continue
                    fi
                fi
                echo "  Carrier изменён на: $cfg_carr"
                ;;
            2)  # Сменить транспорт
                echo ""
                local cfg_cur_carr cfg_cur_trans
                cfg_cur_carr="$(get_carrier "$cfg_ef")"
                cfg_cur_trans="$(get_env_value OLCRTC_TRANSPORT "$cfg_ef")"
                [ -z "$cfg_cur_trans" ] && cfg_cur_trans="datachannel"
                if ! select_transport "$cfg_cur_carr" "$cfg_cur_trans"; then
                    echo "  [!] Неверный выбор"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_TRANSPORT" "$REPLY_TRANSPORT" "$cfg_ef"
                if [ -n "$REPLY_VP8_FPS" ]; then
                    set_env_value "OLCRTC_VP8_FPS" "$REPLY_VP8_FPS" "$cfg_ef"
                    set_env_value "OLCRTC_VP8_BATCH" "$REPLY_VP8_BATCH" "$cfg_ef"
                fi

                if [ "$REPLY_TRANSPORT" = "$cfg_cur_trans" ]; then
                    systemctl restart "$cfg_svc"
                    echo "  Транспорт уже $REPLY_TRANSPORT — настройки обновлены, room сохранён."
                else
                    # Транспорт сменился — пересоздаём комнату.
                    if [ "$cfg_cur_carr" = "telemost" ]; then
                        tty_read -rp "  Введите новый Room ID для Telemost: " cfg_new_room
                        if [ -z "$cfg_new_room" ]; then
                            echo "  [!] Room ID не может быть пустым"
                            tty_read -rp "[Enter для продолжения]"
                            continue
                        fi
                        set_env_value "OLCRTC_ROOM_ID" "$cfg_new_room" "$cfg_ef"
                        systemctl restart "$cfg_svc"
                    else
                        set_env_value "OLCRTC_ROOM_ID" "any" "$cfg_ef"
                        systemctl restart "$cfg_svc"
                        if ! wait_for_room_id_for "$cfg_svc" "$cfg_ef" "$cfg_cur_carr"; then
                            tty_read -rp "[Enter для продолжения]"
                            continue
                        fi
                    fi
                    local cfg_new_room_done
                    cfg_new_room_done="$(get_env_value OLCRTC_ROOM_ID "$cfg_ef")"
                    echo "  Транспорт изменён на: $REPLY_TRANSPORT"
                    echo "  Room ID пересоздан: $cfg_new_room_done"
                fi
                ;;
            3)  # Настроить SOCKS5-прокси
                tty_read -rp "  Введите адрес прокси [user:pass@]host:port: " cfg_proxy
                if [ -z "$cfg_proxy" ] || [[ "$cfg_proxy" != *":"* ]]; then
                    echo "  [!] Неверный формат"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_SOCKS_PROXY" "$cfg_proxy" "$cfg_ef"
                systemctl restart "$cfg_svc"
                echo "  Прокси установлен: $cfg_proxy"
                ;;
            4)  # Убрать SOCKS5-прокси
                set_env_value "OLCRTC_SOCKS_PROXY" "" "$cfg_ef"
                systemctl restart "$cfg_svc"
                echo "  Прокси удалён"
                ;;
            5)  # Debug
                local cfg_debug
                cfg_debug="$(get_env_value OLCRTC_DEBUG "$cfg_ef")"
                if [ -n "$cfg_debug" ] && [ "$cfg_debug" != "0" ] && [ "$cfg_debug" != "false" ]; then
                    set_env_value "OLCRTC_DEBUG" "" "$cfg_ef"
                    echo "  Debug выключен"
                else
                    set_env_value "OLCRTC_DEBUG" "1" "$cfg_ef"
                    echo "  Debug включён"
                fi
                systemctl restart "$cfg_svc"
                ;;
            6)  # Переименовать
                local cfg_cur_name
                cfg_cur_name="$(get_env_value OLCRTC_NAME "$cfg_ef")"
                [ -z "$cfg_cur_name" ] && cfg_cur_name="$(get_carrier "$cfg_ef")_olcrtc"
                echo "  Текущее имя: $cfg_cur_name"
                tty_read -rp "  Новое имя: " cfg_new_name
                if [ -z "$cfg_new_name" ]; then
                    echo "  [!] Имя не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_NAME" "$cfg_new_name" "$cfg_ef"
                echo "  Имя изменено на: $cfg_new_name"
                ;;
            0) continue ;;
            *) echo "  [!] Неверный выбор" ;;
            esac
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        4)  # ── Перезапустить инстанс ──
            echo ""
            echo "  Выберите инстанс для перезапуска:"
            for inst_n in $(list_instances); do
                inst_lbl="$(instance_label "$inst_n")"
                inst_svc="$(instance_service "$inst_n")"
                printf "    %s) [%s] %s\n" "$inst_n" "$inst_lbl" "$(ui_status_indicator "$inst_svc")"
            done
            echo ""
            tty_read -rp "  Номер инстанса: " rst_n
            local rst_ef
            rst_ef="$(instance_env_file "$rst_n")"
            if [ ! -f "$rst_ef" ]; then
                echo "  [!] Инстанс не найден"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local rst_svc
            rst_svc="$(instance_service "$rst_n")"
            systemctl restart "$rst_svc"
            echo "  Инстанс [$(instance_label "$rst_n")] перезапущен."
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        5)  # ── Удалить инстанс ──
            local del_count
            del_count="$(extra_instance_count)"
            if [ "$del_count" -eq 0 ]; then
                echo "  Нет дополнительных инстансов для удаления."
                echo "  (Основной инстанс удаляется через пункт 11 главного меню)"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            echo ""
            echo "  Выберите дополнительный инстанс для удаления:"
            for inst_n in $(list_instances); do
                [ "$inst_n" = "0" ] && continue
                inst_ef="$(instance_env_file "$inst_n")"
                inst_lbl="$(instance_label "$inst_n")"
                inst_carr="$(get_carrier "$inst_ef")"
                inst_room="$(get_env_value OLCRTC_ROOM_ID "$inst_ef")"
                printf "    %s) [%s] %s | Room: %s\n" "$inst_n" "$inst_lbl" "$inst_carr" "$inst_room"
            done
            echo ""
            tty_read -rp "  Номер инстанса: " del_n
            if [ "$del_n" = "0" ]; then
                echo "  [!] Основной инстанс нельзя удалить из этого меню (используйте пункт 11)"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local del_ef
            del_ef="$(instance_env_file "$del_n")"
            if [ ! -f "$del_ef" ]; then
                echo "  [!] Инстанс не найден"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            tty_read -rp "  Удалить инстанс #$del_n? [y/N] " del_confirm
            if [ "$del_confirm" != "y" ] && [ "$del_confirm" != "Y" ]; then
                echo "  Отменено."
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            local del_svc
            del_svc="$(instance_service "$del_n")"
            echo "[*] Останавливаю $del_svc..."
            systemctl disable --now "$del_svc" 2>/dev/null || true
            systemctl reset-failed "$del_svc" 2>/dev/null || true
            echo "[*] Удаляю файлы инстанса #$del_n..."
            rm -rf "/etc/olcrtc/$del_n" "/var/lib/olcrtc-$del_n"
            maybe_remove_template_unit
            echo "  Инстанс #$del_n удалён."
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        6)  # ── Статус всех инстансов ──
            echo ""
            for inst_n in $(list_instances); do
                inst_svc="$(instance_service "$inst_n")"
                inst_lbl="$(instance_label "$inst_n")"
                ui_section "Инстанс [$inst_lbl] — $inst_svc"
                systemctl --no-pager status "$inst_svc" 2>/dev/null || true
                echo ""
            done
            tty_read -rp "[Enter для продолжения]"
            ;;

        0)  # Назад
            return
            ;;

        *)
            echo "  [!] Неверный пункт меню"
            ;;
        esac
    done
}

# ══════════════════════════════════════════════════════════════════════════════
#  INTERACTIVE MENU MODE
# ══════════════════════════════════════════════════════════════════════════════

run_menu() {
    ensure_qrencode || true

    # Active instance for the main menu. Defaults to 0 (primary) but the user
    # can switch to any other instance via the "Сменить активный инстанс"
    # item. All operations below operate on this instance's env file and
    # systemd service.
    local MENU_INSTANCE_N="${MENU_INSTANCE_N:-0}"

    while true; do
        # If the previously-selected instance was deleted (e.g. via the
        # instance submenu), fall back to the primary so we never operate on
        # a missing env file.
        local MENU_ENV MENU_SVC MENU_KEY_FILE MENU_LBL
        MENU_ENV="$(instance_env_file "$MENU_INSTANCE_N")"
        if [ ! -f "$MENU_ENV" ]; then
            MENU_INSTANCE_N=0
            MENU_ENV="$(instance_env_file 0)"
        fi
        MENU_SVC="$(instance_service "$MENU_INSTANCE_N")"
        MENU_KEY_FILE="$(instance_key_file "$MENU_INSTANCE_N")"
        MENU_LBL="$(instance_label "$MENU_INSTANCE_N")"

        local cur_carrier cur_transport cur_room cur_key cur_name cur_debug cur_proxy cur_ip
        cur_carrier="$(get_carrier "$MENU_ENV")"
        cur_transport="$(get_env_value OLCRTC_TRANSPORT "$MENU_ENV")"
        [ -z "$cur_transport" ] && cur_transport="datachannel"
        cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
        cur_key="$(get_env_value OLCRTC_KEY "$MENU_ENV")"
        cur_name="$(get_env_value OLCRTC_NAME "$MENU_ENV")"
        cur_debug="$(get_env_value OLCRTC_DEBUG "$MENU_ENV")"
        cur_proxy="$(get_env_value OLCRTC_SOCKS_PROXY "$MENU_ENV")"
        cur_warp="$(get_env_value OLCRTC_WARP_PROXY "$MENU_ENV")"
        cur_ip="$(get_public_ip)"

        [ -z "$cur_name" ] && cur_name="${cur_carrier}_olcrtc"

        local inst_total
        inst_total="$(instance_count)"
        local extra_inst
        extra_inst="$(extra_instance_count)"

        # ── Header banner ─────────────────────────────────────────
        echo ""
        ui_banner_top 60
        printf '%s   %s  %solcRTC Server Manager%s\n' \
            "$(ui_bold)$(ui_cyan)" "$UI_I_LOGO" "$(ui_bold)" "$(ui_reset)"
        ui_banner_bottom 60
        # ── Info block ────────────────────────────────────────────
        # Labels are padded with literal spaces because bash printf %-Ns
        # counts bytes (Cyrillic = 2 bytes/char), not display columns.
        local A="$(ui_cyan)$UI_ARROW$(ui_reset)"
        echo "  $A Инстанс:    $(ui_bold)$MENU_LBL$(ui_reset)   $(ui_status_indicator "$MENU_SVC")"
        echo "  $A Имя:        $UI_I_NAME $cur_name"
        echo "  $A Carrier:    $UI_I_CARRIER $cur_carrier  $(ui_dim)$UI_PIPE$(ui_reset)  $UI_I_TRANSPORT Transport: $(ui_bold)$cur_transport$(ui_reset)"
        echo "  $A Room:       $UI_I_ROOM $cur_room  $(ui_dim)$UI_PIPE$(ui_reset)  $UI_I_IP IP: $cur_ip"
        echo "  $A Прокси:     $UI_I_PROXY_ON $(proxy_human "$cur_proxy")"
        if [ -n "$cur_warp" ]; then
            echo "  $A WARP:       $UI_I_PROXY_ON $cur_warp"
        fi
        echo "  $A Debug:      $UI_I_DEBUG $(debug_human "$cur_debug")"
        if [ "$extra_inst" -gt 0 ]; then
            echo "  $A Инстансов:  $UI_I_INSTANCE $inst_total (основной + $extra_inst доп.)"
        fi
        echo ""

        # ── Menu sections ─────────────────────────────────────────
        ui_section "Управление активным инстансом"
        echo ""
        printf '   %s   1) Статус сервиса\n'                                  "$UI_I_STATUS"
        printf '   %s   2) Показать URI / QR-код\n'                           "$UI_I_QR"
        printf '   %s   3) Сменить carrier\n'                                 "$UI_I_CARRIER"
        printf '   %s   4) Сменить транспорт\n'                               "$UI_I_TRANSPORT"
        printf '   %s   5) Пересоздать room ID  %s(--regenerate)%s\n'         "$UI_I_ROOM"     "$(ui_dim)" "$(ui_reset)"
        printf '   %s   6) Ротация ключа + room ID  %s(--regenerate-key)%s\n' "$UI_I_KEY"      "$(ui_dim)" "$(ui_reset)"
        printf '   %s   7) Настроить SOCKS5-прокси\n'                         "$UI_I_PROXY_ON"
        printf '   %s   8) Убрать SOCKS5-прокси\n'                            "$UI_I_PROXY_OFF"
        printf '   %s   9) Включить / выключить debug-логирование\n'          "$UI_I_DEBUG"
        printf '   %s  10) Переименовать соединение (name)\n'                 "$UI_I_RENAME"
        printf '   %s  14) Настроить WARP-прокси  %s(скрытие IP VPS)%s\n'       "$UI_I_PROXY_ON" "$(ui_dim)" "$(ui_reset)"
        printf '   %s  15) Убрать WARP-прокси\n'                               "$UI_I_PROXY_OFF"
        echo ""

        ui_section "Инстансы"
        echo ""
        if [ "$extra_inst" -gt 0 ]; then
            printf '   %s  13) Сменить активный инстанс  %s(сейчас: %s)%s\n' \
                "$UI_I_SWITCH" "$(ui_dim)" "$MENU_LBL" "$(ui_reset)"
        fi
        printf '   %s  20) Управление инстансами  %s%s%s\n' \
            "$UI_I_INSTANCES" "$(ui_dim)" ">>>" "$(ui_reset)"
        echo ""

        ui_section "Подписки"
        echo ""
        printf '   %s  30) Управление подписками  %s%s%s\n' \
            "$UI_I_SUB" "$(ui_dim)" ">>>" "$(ui_reset)"
        echo ""

        ui_section "Глобальные операции"
        echo ""
        printf '   %s  11) Обновить бинарник olcRTC  %s(для всех инстансов)%s\n' \
            "$UI_I_UPDATE" "$(ui_dim)" "$(ui_reset)"
        printf '   %s  12) %sУдалить olcRTC полностью%s\n' \
            "$UI_I_DELETE" "$(ui_red)" "$(ui_reset)"
        echo ""
        printf '   %s   0) Выход\n' "$UI_I_EXIT"
        echo ""
        tty_read -rp "$(ui_bold)→$(ui_reset) Выберите пункт: " choice

        case "$choice" in
        1)  # Статус сервиса
            echo ""
            systemctl --no-pager status "$MENU_SVC" || true
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        2)  # Показать URI / QR-код
            show_uri_qr "$cur_carrier" "$cur_room" "$cur_key" "$cur_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        3)  # Сменить carrier
            echo ""
            echo "  Текущий carrier: $cur_carrier"
            echo ""
            echo "  Carrier (сервис):"
            echo "    1) wbstream  — Wildberries Stream"
            echo "    2) jazz      — SaluteJazz"
            echo "    3) telemost  — Yandex Telemost"
            echo ""
            tty_read -rp "  Выберите carrier [1-3]: " pchoice
            local new_carrier=""
            case "$pchoice" in
                1) new_carrier="wbstream" ;;
                2) new_carrier="jazz" ;;
                3) new_carrier="telemost" ;;
                *) echo "  [!] Неверный выбор"; tty_read -rp "[Enter для продолжения]"; continue ;;
            esac

            set_env_value "OLCRTC_CARRIER" "$new_carrier" "$MENU_ENV"

            # Check transport compatibility
            if ! is_transport_supported "$new_carrier" "$cur_transport"; then
                local new_trans
                new_trans="$(default_transport_for "$new_carrier")"
                echo "  [!] Транспорт $cur_transport не совместим с $new_carrier, переключаю на $new_trans"
                set_env_value "OLCRTC_TRANSPORT" "$new_trans" "$MENU_ENV"
            fi

            if [ "$new_carrier" = "telemost" ]; then
                tty_read -rp "  Введите Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room" "$MENU_ENV"
                ROOM_ID="$new_room"
            else
                set_env_value "OLCRTC_ROOM_ID" "any" "$MENU_ENV"
                ROOM_ID="any"
            fi

            CARRIER="$new_carrier"
            systemctl restart "$MENU_SVC"

            if [ "$ROOM_ID" = "any" ]; then
                if ! wait_for_room_id_for "$MENU_SVC" "$MENU_ENV" "$cur_carrier"; then
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
            cur_name="$(get_env_value OLCRTC_NAME "$MENU_ENV")"
            [ -z "$cur_name" ] && cur_name="${new_carrier}_olcrtc"
            cur_key="$(get_env_value OLCRTC_KEY "$MENU_ENV")"

            echo ""
            echo "  Carrier изменён на: $new_carrier"
            show_uri_qr "$new_carrier" "$cur_room" "$cur_key" "$cur_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        4)  # Сменить транспорт
            echo ""
            if ! select_transport "$cur_carrier" "$cur_transport"; then
                echo "  [!] Неверный выбор"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            if [ "$REPLY_TRANSPORT" = "$cur_transport" ]; then
                set_env_value "OLCRTC_TRANSPORT" "$REPLY_TRANSPORT" "$MENU_ENV"
                if [ -n "$REPLY_VP8_FPS" ]; then
                    set_env_value "OLCRTC_VP8_FPS" "$REPLY_VP8_FPS" "$MENU_ENV"
                    set_env_value "OLCRTC_VP8_BATCH" "$REPLY_VP8_BATCH" "$MENU_ENV"
                fi
                systemctl restart "$MENU_SVC"
                echo "  Транспорт уже $REPLY_TRANSPORT — настройки обновлены, room сохранён."
                echo ""
                tty_read -rp "[Enter для продолжения]"
                continue
            fi

            set_env_value "OLCRTC_TRANSPORT" "$REPLY_TRANSPORT" "$MENU_ENV"
            if [ -n "$REPLY_VP8_FPS" ]; then
                set_env_value "OLCRTC_VP8_FPS" "$REPLY_VP8_FPS" "$MENU_ENV"
                set_env_value "OLCRTC_VP8_BATCH" "$REPLY_VP8_BATCH" "$MENU_ENV"
            fi

            # Транспорт сменился — пересоздаём комнату, иначе старая
            # сигналинг-сессия конфликтует с новым транспортом.
            if [ "$cur_carrier" = "telemost" ]; then
                tty_read -rp "  Введите новый Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room" "$MENU_ENV"
                ROOM_ID="$new_room"
                systemctl restart "$MENU_SVC"
            else
                set_env_value "OLCRTC_ROOM_ID" "any" "$MENU_ENV"
                ROOM_ID="any"
                CARRIER="$cur_carrier"
                systemctl restart "$MENU_SVC"
                if ! wait_for_room_id_for "$MENU_SVC" "$MENU_ENV" "$cur_carrier"; then
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
            cur_key="$(get_env_value OLCRTC_KEY "$MENU_ENV")"
            cur_name="$(get_env_value OLCRTC_NAME "$MENU_ENV")"
            [ -z "$cur_name" ] && cur_name="${cur_carrier}_olcrtc"
            echo ""
            echo "  Транспорт изменён на: $REPLY_TRANSPORT"
            echo "  Room ID пересоздан: $cur_room"
            show_uri_qr "$cur_carrier" "$cur_room" "$cur_key" "$cur_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        5)  # Пересоздать room ID
            set_env_value "OLCRTC_ROOM_ID" "any" "$MENU_ENV"
            ROOM_ID="any"
            CARRIER="$cur_carrier"
            systemctl restart "$MENU_SVC"

            if [ "$cur_carrier" = "telemost" ]; then
                tty_read -rp "  Введите новый Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room" "$MENU_ENV"
                ROOM_ID="$new_room"
                systemctl restart "$MENU_SVC"
            else
                if ! wait_for_room_id_for "$MENU_SVC" "$MENU_ENV" "$cur_carrier"; then
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
            echo ""
            echo "  Room ID обновлён: $cur_room"
            show_uri_qr "$cur_carrier" "$cur_room" "$cur_key" "$cur_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        6)  # Ротация ключа + room ID
            echo ""
            tty_read -rp "  Все существующие клиенты потеряют подключение. Продолжить? [y/N] " confirm
            if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
                echo "  Отменено."
                tty_read -rp "[Enter для продолжения]"
                continue
            fi

            echo "[*] Generating fresh 256-bit encryption key..."
            umask 077
            openssl rand -hex 32 > "$MENU_KEY_FILE"
            chown root:olcrtc "$MENU_KEY_FILE"
            chmod 0640 "$MENU_KEY_FILE"
            local new_key
            new_key="$(cat "$MENU_KEY_FILE")"
            set_env_value "OLCRTC_KEY" "$new_key" "$MENU_ENV"
            set_env_value "OLCRTC_ROOM_ID" "any" "$MENU_ENV"
            ROOM_ID="any"
            CARRIER="$cur_carrier"
            systemctl restart "$MENU_SVC"

            if [ "$cur_carrier" = "telemost" ]; then
                tty_read -rp "  Введите новый Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room" "$MENU_ENV"
                ROOM_ID="$new_room"
                systemctl restart "$MENU_SVC"
            else
                if ! wait_for_room_id_for "$MENU_SVC" "$MENU_ENV" "$cur_carrier"; then
                    tty_read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
            cur_key="$(get_env_value OLCRTC_KEY "$MENU_ENV")"
            echo ""
            echo "  Ключ и Room ID обновлены."
            show_uri_qr "$cur_carrier" "$cur_room" "$cur_key" "$cur_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        7)  # Настроить SOCKS5-прокси
            echo ""
            tty_read -rp "  Введите адрес прокси [user:pass@]host:port: " new_proxy
            if [ -z "$new_proxy" ] || [[ "$new_proxy" != *":"* ]]; then
                echo "  [!] Неверный формат. Ожидается [user:pass@]host:port"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            set_env_value "OLCRTC_SOCKS_PROXY" "$new_proxy" "$MENU_ENV"
            systemctl restart "$MENU_SVC"
            echo "  Прокси установлен: $new_proxy"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        8)  # Убрать SOCKS5-прокси
            set_env_value "OLCRTC_SOCKS_PROXY" "" "$MENU_ENV"
            systemctl restart "$MENU_SVC"
            echo "  Прокси удалён"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        9)  # Включить / выключить debug
            if [ -n "$cur_debug" ] && [ "$cur_debug" != "0" ] && [ "$cur_debug" != "false" ]; then
                set_env_value "OLCRTC_DEBUG" "" "$MENU_ENV"
                echo "  Debug выключен"
            else
                set_env_value "OLCRTC_DEBUG" "1" "$MENU_ENV"
                echo "  Debug включён"
            fi
            systemctl restart "$MENU_SVC"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        10) # Переименовать соединение
            echo ""
            echo "  Текущее имя: $cur_name"
            tty_read -rp "  Введите новое имя: " new_name
            if [ -z "$new_name" ]; then
                echo "  [!] Имя не может быть пустым"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            set_env_value "OLCRTC_NAME" "$new_name" "$MENU_ENV"
            cur_room="$(get_env_value OLCRTC_ROOM_ID "$MENU_ENV")"
            cur_key="$(get_env_value OLCRTC_KEY "$MENU_ENV")"
            show_uri_qr "$cur_carrier" "$cur_room" "$cur_key" "$new_name" "$MENU_ENV"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        11) # Обновить бинарник
            local up_arch up_bin up_tag up_url
            up_arch="$(uname -m)"
            case "$up_arch" in
                x86_64|amd64)  up_bin="olcrtc-linux-amd64" ;;
                aarch64|arm64) up_bin="olcrtc-linux-arm64" ;;
                *) echo "  [!] Неподдерживаемая архитектура: $up_arch"; tty_read -rp "[Enter для продолжения]"; continue ;;
            esac
            up_tag="server-v$INSTALLER_VERSION"
            up_url="https://github.com/Oleglog/Olcrtc_manager/releases/download/$up_tag/$up_bin"
            echo ""
            echo "  Скачиваю $up_bin из релиза $up_tag..."
            if ! curl -fsSL "$up_url" -o /usr/local/bin/olcrtc.tmp; then
                echo "  [!] Не удалось скачать: $up_url"
                rm -f /usr/local/bin/olcrtc.tmp
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            mv /usr/local/bin/olcrtc.tmp /usr/local/bin/olcrtc
            chmod 0755 /usr/local/bin/olcrtc
            # Бинарник общий — перезапускаем все инстансы, чтобы они подхватили новую версию.
            local up_n up_svc
            for up_n in $(list_instances); do
                up_svc="$(instance_service "$up_n")"
                if systemctl is-enabled --quiet "$up_svc" 2>/dev/null \
                   || systemctl is-active --quiet "$up_svc" 2>/dev/null; then
                    systemctl restart "$up_svc"
                    echo "  Перезапущен: $up_svc"
                fi
            done
            echo "  Бинарник обновлён, все активные инстансы перезапущены."
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        13) # Сменить активный инстанс
            if [ "$extra_inst" -eq 0 ]; then
                echo "  [!] Дополнительных инстансов нет — переключать не на что."
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            echo ""
            ui_section "Выберите активный инстанс"
            echo ""
            local sw_n sw_ef sw_lbl sw_carr sw_room sw_svc
            for sw_n in $(list_instances); do
                sw_ef="$(instance_env_file "$sw_n")"
                sw_lbl="$(instance_label "$sw_n")"
                sw_svc="$(instance_service "$sw_n")"
                sw_carr="$(get_carrier "$sw_ef")"
                sw_room="$(get_env_value OLCRTC_ROOM_ID "$sw_ef")"
                local marker="  "
                if [ "$sw_n" = "$MENU_INSTANCE_N" ]; then
                    marker="$(ui_green)$UI_BULLET$(ui_reset) "
                fi
                printf "    %s %s) %s %s%-12s%s  %s %-10s  %s Room: %-12s  %s\n" \
                    "$marker" "$sw_n" "$UI_I_INSTANCE" \
                    "$(ui_dim)" "[$sw_lbl]" "$(ui_reset)" \
                    "$UI_I_CARRIER" "$sw_carr" \
                    "$UI_I_ROOM" "$sw_room" \
                    "$(ui_status_indicator "$sw_svc")"
            done
            echo ""
            tty_read -rp "  Номер инстанса (Enter = оставить $MENU_LBL): " sw_pick
            if [ -z "$sw_pick" ]; then
                continue
            fi
            local sw_pick_ef
            sw_pick_ef="$(instance_env_file "$sw_pick")"
            if [ ! -f "$sw_pick_ef" ]; then
                echo "  [!] Инстанс не найден"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            MENU_INSTANCE_N="$sw_pick"
            echo "  $(ui_green)→$(ui_reset) Активный инстанс: $UI_I_INSTANCE $(ui_bold)$(instance_label "$sw_pick")$(ui_reset)"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        12) # Удалить olcRTC полностью
            echo ""
            tty_read -rp "  Удалить olcRTC сервер, все конфиги и ключи? Это необратимо! [y/N] " confirm
            if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
                echo "  Отменено."
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            echo "[*] Останавливаю и удаляю основной сервис..."
            systemctl disable --now olcrtc-server 2>/dev/null || true
            systemctl reset-failed olcrtc-server 2>/dev/null || true
            rm -f /etc/systemd/system/olcrtc-server.service
            echo "[*] Останавливаю и удаляю все дополнительные инстансы..."
            for inst_n in $(list_instances); do
                [ "$inst_n" = "0" ] && continue
                local inst_svc
                inst_svc="$(instance_service "$inst_n")"
                systemctl disable --now "$inst_svc" 2>/dev/null || true
                systemctl reset-failed "$inst_svc" 2>/dev/null || true
            done
            rm -f /etc/systemd/system/olcrtc-server@.service
            systemctl daemon-reload
            # Subscription database
            local sub_db_found=0
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
                    # Copy out before dirs are deleted
                    if [ -f /var/lib/olcrtc/subscriptions.db ]; then
                        cp /var/lib/olcrtc/subscriptions.db /tmp/olcrtc-subscriptions.db 2>/dev/null || true
                        echo "  Копия: /tmp/olcrtc-subscriptions.db"
                    fi
                fi
            fi

            echo "[*] Удаляю файлы..."
            rm -rf /etc/olcrtc /var/lib/olcrtc /var/lib/olcrtc-* /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher
            echo "[*] Удаляю пользователя olcrtc..."
            userdel olcrtc 2>/dev/null || true
            echo ""
            echo "  olcRTC полностью удалён."
            exit 0
            ;;

        14) # Настроить WARP-прокси
            echo ""
            echo "  WARP-прокси направляет клиентский tunnel-трафик через Cloudflare WARP,"
            echo "  скрывая реальный IP VPS от посещаемых сайтов."
            echo ""
            local cur_warp_val
            cur_warp_val="$(get_env_value OLCRTC_WARP_PROXY "$MENU_ENV")"
            if [ -n "$cur_warp_val" ]; then
                echo "  Текущее значение: $cur_warp_val"
            fi
            tty_read -rp "  Введите адрес WARP-прокси host:port [Enter = 127.0.0.1:40000]: " new_warp
            [ -z "$new_warp" ] && new_warp="127.0.0.1:40000"
            if [[ "$new_warp" != *":"* ]]; then
                echo "  [!] Неверный формат. Ожидается host:port"
                tty_read -rp "[Enter для продолжения]"
                continue
            fi
            set_env_value "OLCRTC_WARP_PROXY" "$new_warp" "$MENU_ENV"
            systemctl restart "$MENU_SVC"
            echo "  WARP-прокси установлен: $new_warp"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        15) # Убрать WARP-прокси
            set_env_value "OLCRTC_WARP_PROXY" "" "$MENU_ENV"
            systemctl restart "$MENU_SVC"
            echo "  WARP-прокси удалён"
            echo ""
            tty_read -rp "[Enter для продолжения]"
            ;;

        20) # Управление инстансами
            run_instance_menu
            ;;

        30) # Управление подписками
            run_subscription_menu
            ;;

        0)  # Выход
            echo "Выход."
            exit 0
            ;;

        *)
            echo "  [!] Неверный пункт меню"
            ;;
        esac
    done
}

# ══════════════════════════════════════════════════════════════════════════════
#  INSTALLATION MODE
# ══════════════════════════════════════════════════════════════════════════════

run_install() {
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64)  BIN_NAME="olcrtc-linux-amd64" ;;
        aarch64|arm64) BIN_NAME="olcrtc-linux-arm64" ;;
        *)
            echo "[!] Unsupported CPU architecture: $ARCH" >&2
            echo "    Supported: x86_64 (amd64), aarch64 (arm64)." >&2
            exit 1
            ;;
    esac

    RELEASE_TAG="server-v$INSTALLER_VERSION"
    RELEASE_URL_BASE="https://github.com/Oleglog/Olcrtc_manager/releases/download/$RELEASE_TAG"

    # Try bundled binary first (git checkout / tarball), then download
    SCRIPT_DIR=""
    if [ -n "${BASH_SOURCE[0]:-}" ] && [ "${BASH_SOURCE[0]}" != "bash" ]; then
        SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)" || true
    fi
    SRC_BIN="${SCRIPT_DIR:+$SCRIPT_DIR/bin/$BIN_NAME}"

    echo "[*] olcRTC server installer v$INSTALLER_VERSION"
    echo "[*] Detected architecture: $ARCH ($BIN_NAME)"

    # Create system user
    if ! id olcrtc >/dev/null 2>&1; then
        echo "[*] Creating olcrtc system user..."
        useradd --system --no-create-home --shell /usr/sbin/nologin --home-dir "$STATE_DIR" olcrtc
    fi

    # Prepare directories
    echo "[*] Preparing $CONFIG_DIR and $STATE_DIR..."
    install -d -m 0750 -o root -g olcrtc "$CONFIG_DIR"
    install -d -m 0750 -o olcrtc -g olcrtc "$STATE_DIR"

    # Install binary: bundled → existing → download
    if [ -n "$SRC_BIN" ] && [ -f "$SRC_BIN" ]; then
        echo "[*] Installing bundled binary..."
        install -m 0755 -o root -g root "$SRC_BIN" /usr/local/bin/olcrtc
    elif [ -f /usr/local/bin/olcrtc ]; then
        echo "[*] Keeping existing /usr/local/bin/olcrtc"
    else
        if ! command -v curl >/dev/null 2>&1; then
            echo "[!] curl is required to download the binary." >&2
            exit 1
        fi
        echo "[*] Downloading $BIN_NAME from $RELEASE_TAG release..."
        if ! curl -fsSL "$RELEASE_URL_BASE/$BIN_NAME" -o /usr/local/bin/olcrtc.tmp; then
            echo "[!] Failed to download $BIN_NAME from $RELEASE_URL_BASE/" >&2
            echo "[!] Check that release '$RELEASE_TAG' exists, or build from source." >&2
            rm -f /usr/local/bin/olcrtc.tmp
            exit 1
        fi
        mv /usr/local/bin/olcrtc.tmp /usr/local/bin/olcrtc
        echo "[*] Downloaded to /usr/local/bin/olcrtc"
    fi
    chmod 0755 /usr/local/bin/olcrtc

    # Install embedded launcher script
    echo "[*] Installing olcrtc-launcher..."
    cat > /usr/local/bin/olcrtc-launcher <<'LAUNCHER'
#!/usr/bin/env bash
# olcRTC server launcher — reads env and translates to CLI flags.
set -euo pipefail

# Carrier: prefer OLCRTC_CARRIER, fall back to legacy OLCRTC_PROVIDER
carrier="${OLCRTC_CARRIER:-${OLCRTC_PROVIDER:-}}"
# Backward compat: wb_stream → wbstream
[ "$carrier" = "wb_stream" ] && carrier="wbstream"

if [ -z "$carrier" ] || [ -z "${OLCRTC_ROOM_ID:-}" ] || [ -z "${OLCRTC_KEY:-}" ]; then
    echo "olcrtc-launcher: missing required env (OLCRTC_CARRIER / OLCRTC_ROOM_ID / OLCRTC_KEY)" >&2
    exit 64
fi

transport="${OLCRTC_TRANSPORT:-datachannel}"
link="${OLCRTC_LINK:-direct}"

ARGS=(
    -mode srv
    -carrier "$carrier"
    -transport "$transport"
    -link "$link"
    -data data
    -id "$OLCRTC_ROOM_ID"
    -key "$OLCRTC_KEY"
    -dns "${OLCRTC_DNS:-1.1.1.1:53}"
)

if [ -n "${OLCRTC_DEBUG:-}" ] && [ "$OLCRTC_DEBUG" != "0" ] && [ "$OLCRTC_DEBUG" != "false" ]; then
    ARGS+=(-debug)
fi

# VP8 channel options
if [ "$transport" = "vp8channel" ]; then
    ARGS+=(-vp8-fps "${OLCRTC_VP8_FPS:-60}" -vp8-batch "${OLCRTC_VP8_BATCH:-8}")
fi

if [ -n "${OLCRTC_SOCKS_PROXY:-}" ]; then
    proxy="$OLCRTC_SOCKS_PROXY"
    proxy="${proxy#socks5://}"
    proxy="${proxy#socks5h://}"

    proxy_user=""
    proxy_pass=""
    if [[ "$proxy" == *"@"* ]]; then
        creds="${proxy%@*}"
        proxy="${proxy##*@}"
        if [[ "$creds" == *":"* ]]; then
            proxy_user="${creds%%:*}"
            proxy_pass="${creds#*:}"
        else
            proxy_user="$creds"
        fi
    fi

    if [[ "$proxy" != *":"* ]]; then
        echo "olcrtc-launcher: OLCRTC_SOCKS_PROXY must contain host:port (got '$proxy')" >&2
        exit 66
    fi
    proxy_host="${proxy%:*}"
    proxy_port="${proxy##*:}"
    if ! [[ "$proxy_port" =~ ^[0-9]+$ ]]; then
        echo "olcrtc-launcher: OLCRTC_SOCKS_PROXY port is not numeric ('$proxy_port')" >&2
        exit 67
    fi

    ARGS+=(-socks-proxy "$proxy_host" -socks-proxy-port "$proxy_port")
    if [ -n "$proxy_user" ]; then
        ARGS+=(-socks-proxy-user "$proxy_user")
    fi
    if [ -n "$proxy_pass" ]; then
        ARGS+=(-socks-proxy-pass "$proxy_pass")
    fi
fi

if [ -n "${OLCRTC_WARP_PROXY:-}" ]; then
    warp="$OLCRTC_WARP_PROXY"
    if [[ "$warp" != *":"* ]]; then
        echo "olcrtc-launcher: OLCRTC_WARP_PROXY must be host:port (got '$warp')" >&2
        exit 68
    fi
    warp_host="${warp%:*}"
    warp_port="${warp##*:}"
    ARGS+=(-warp-proxy "$warp_host" -warp-proxy-port "$warp_port")
fi

# Subscription server
if [ -n "${OLCRTC_SUB_ENABLED:-}" ] && [ "$OLCRTC_SUB_ENABLED" = "1" ]; then
    ARGS+=(-sub-enabled)
    sub_port="${OLCRTC_SUB_PORT:-2096}"
    ARGS+=(-sub-port "$sub_port")
    ARGS+=(-sub-db "data/subscriptions.db")
fi

exec /usr/local/bin/olcrtc "${ARGS[@]}"
LAUNCHER
    chmod 0755 /usr/local/bin/olcrtc-launcher
    chown root:root /usr/local/bin/olcrtc-launcher

    # Install embedded systemd unit
    echo "[*] Installing systemd unit..."
    cat > /etc/systemd/system/olcrtc-server.service <<'UNIT'
[Unit]
Description=olcRTC server (WebRTC tunnel through whitelisted services)
Documentation=https://github.com/openlibrecommunity/olcrtc
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=10

[Service]
Type=exec
EnvironmentFile=/etc/olcrtc/env
User=olcrtc
Group=olcrtc
StateDirectory=olcrtc
StateDirectoryMode=0750
WorkingDirectory=/var/lib/olcrtc
ExecStart=/usr/local/bin/olcrtc-launcher

# Hardening
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
RestrictSUIDSGID=true
LockPersonality=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
SystemCallArchitectures=native
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true

# Restart policy
Restart=on-failure
RestartSec=5s

# Logging
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
UNIT
    systemctl daemon-reload

    # Generate or reuse the encryption key
    if [ "$REGENERATE_KEY" -eq 1 ] || [ ! -s "$KEY_FILE" ]; then
        echo "[*] Generating fresh 256-bit encryption key..."
        umask 077
        openssl rand -hex 32 > "$KEY_FILE"
        chown root:olcrtc "$KEY_FILE"
        chmod 0640 "$KEY_FILE"
    fi
    KEY="$(cat "$KEY_FILE")"

    # Read previous env to preserve untouched fields
    EXISTING_ROOM=""
    EXISTING_SOCKS_PROXY=""
    EXISTING_WARP_PROXY=""
    EXISTING_DEBUG=""
    EXISTING_NAME=""
    EXISTING_TRANSPORT=""
    EXISTING_VP8_FPS=""
    EXISTING_VP8_BATCH=""
    EXISTING_CARRIER=""
    if [ -f "$ENV_FILE" ]; then
        EXISTING_ROOM="$(get_env_value OLCRTC_ROOM_ID)"
        EXISTING_SOCKS_PROXY="$(get_env_value OLCRTC_SOCKS_PROXY)"
        EXISTING_WARP_PROXY="$(get_env_value OLCRTC_WARP_PROXY)"
        EXISTING_DEBUG="$(get_env_value OLCRTC_DEBUG)"
        EXISTING_NAME="$(get_env_value OLCRTC_NAME)"
        EXISTING_TRANSPORT="$(get_env_value OLCRTC_TRANSPORT)"
        EXISTING_VP8_FPS="$(get_env_value OLCRTC_VP8_FPS)"
        EXISTING_VP8_BATCH="$(get_env_value OLCRTC_VP8_BATCH)"
        EXISTING_CARRIER="$(get_carrier)"
    fi
    if [ "$REGENERATE_ROOM" -eq 1 ]; then
        EXISTING_ROOM=""
    fi
    # Если --carrier не указан явно, сохраняем текущий carrier из env
    # (без этого --debug/--regenerate без --carrier молча сбросил бы
    # carrier на дефолтный wbstream).
    if [ "$SET_CARRIER" = "__keep__" ] && [ -n "$EXISTING_CARRIER" ]; then
        CARRIER="$EXISTING_CARRIER"
    fi
    # Если carrier явно сменился — старая комната принадлежит другому провайдеру.
    if [ "$SET_CARRIER" != "__keep__" ] && [ -n "$EXISTING_CARRIER" ] \
       && [ "$SET_CARRIER" != "$EXISTING_CARRIER" ]; then
        echo "[*] Carrier changed: $EXISTING_CARRIER → $SET_CARRIER, regenerating room ID."
        EXISTING_ROOM=""
    fi
    # Если транспорт явно указан и отличается — комната конфликтует с новым транспортом.
    if [ "$SET_TRANSPORT" != "__keep__" ] && [ -n "$EXISTING_TRANSPORT" ] \
       && [ "$SET_TRANSPORT" != "$EXISTING_TRANSPORT" ]; then
        echo "[*] Transport changed: $EXISTING_TRANSPORT → $SET_TRANSPORT, regenerating room ID."
        EXISTING_ROOM=""
    fi

    # Decide final SOCKS proxy / debug / transport values
    if [ "$SET_SOCKS_PROXY" = "__keep__" ]; then
        SOCKS_PROXY="$EXISTING_SOCKS_PROXY"
    else
        SOCKS_PROXY="$SET_SOCKS_PROXY"
    fi
    if [ "$SET_DEBUG" = "__keep__" ]; then
        DEBUG_FLAG="$EXISTING_DEBUG"
    else
        DEBUG_FLAG="$SET_DEBUG"
    fi
    if [ "$SET_TRANSPORT" = "__keep__" ]; then
        if [ -n "$EXISTING_TRANSPORT" ]; then
            TRANSPORT="$EXISTING_TRANSPORT"
        fi
        # else keep TRANSPORT from env/default
    else
        TRANSPORT="$SET_TRANSPORT"
    fi

    # Validate that the resolved transport is supported by the systemd
    # launcher for the chosen carrier. This prevents the service from
    # starting in a state where the binary aborts because of missing flags
    # (most notably -video-* for videochannel).
    if ! is_transport_supported "$CARRIER" "$TRANSPORT"; then
        if [ "$SET_TRANSPORT" != "__keep__" ]; then
            echo "[!] Transport '$TRANSPORT' is not supported with carrier '$CARRIER'." >&2
            echo "    Supported transports for $CARRIER:" >&2
            supported_transports_for "$CARRIER" | sed 's/^/      - /' >&2
            if [ "$TRANSPORT" = "videochannel" ]; then
                echo "    videochannel currently requires manual launcher configuration." >&2
            fi
            exit 1
        fi
        # SET_TRANSPORT was __keep__ but the existing env contains an
        # unsupported transport (e.g. videochannel from an older install).
        # Fall back to a safe default and force room regeneration.
        local fallback_transport
        fallback_transport="$(default_transport_for "$CARRIER")"
        echo "[!] Saved transport '$TRANSPORT' is not supported with carrier '$CARRIER'."
        echo "    Falling back to '$fallback_transport' and regenerating room ID."
        TRANSPORT="$fallback_transport"
        EXISTING_ROOM=""
    fi

    VP8_FPS="${EXISTING_VP8_FPS:-}"
    VP8_BATCH="${EXISTING_VP8_BATCH:-}"

    # Decide subscription settings
    EXISTING_SUB_ENABLED=""
    EXISTING_SUB_PORT=""
    if [ -f "$ENV_FILE" ]; then
        EXISTING_SUB_ENABLED="$(get_env_value OLCRTC_SUB_ENABLED)"
        EXISTING_SUB_PORT="$(get_env_value OLCRTC_SUB_PORT)"
    fi
    if [ -n "$EXISTING_SUB_ENABLED" ]; then
        SUB_ENABLED="$EXISTING_SUB_ENABLED"
        SUB_PORT="${EXISTING_SUB_PORT:-2096}"
        echo "[*] Subscription server: $([ "$SUB_ENABLED" = "1" ] && echo "enabled (port $SUB_PORT)" || echo "disabled")"
    else
        echo ""
        tty_read -rp "[?] Включить сервер подписок? (y/N): " sub_ans
        if [ "$sub_ans" = "y" ] || [ "$sub_ans" = "Y" ]; then
            SUB_ENABLED="1"
            tty_read -rp "[?] Порт сервера подписок [Enter = 2096]: " sub_port_ans
            SUB_PORT="${sub_port_ans:-2096}"
            echo "[*] Subscription server enabled on port $SUB_PORT"
        else
            SUB_ENABLED=""
            SUB_PORT=""
        fi
    fi

    # Decide name
    if [ -n "$SET_NAME" ]; then
        NAME="$SET_NAME"
    elif [ -n "$EXISTING_NAME" ]; then
        NAME="$EXISTING_NAME"
    else
        NAME="${CARRIER}_olcrtc"
    fi

    # Decide initial room ID
    if [ -n "$EXISTING_ROOM" ] && [ "$EXISTING_ROOM" != "any" ]; then
        ROOM_ID="$EXISTING_ROOM"
        echo "[*] Reusing existing room ID: $ROOM_ID"
    elif [ "$CARRIER" = "telemost" ]; then
        if [ -n "$SET_TELEMOST_ID" ]; then
            ROOM_ID="$SET_TELEMOST_ID"
        else
            tty_read -rp "[?] Введите Room ID для Telemost: " ROOM_ID
            if [ -z "$ROOM_ID" ]; then
                echo "[!] Room ID не может быть пустым для Telemost" >&2
                exit 1
            fi
        fi
        echo "[*] Telemost room ID: $ROOM_ID"
    else
        ROOM_ID="any"
    fi

    # Write env file
    cat > "$ENV_FILE" <<EOF
# Managed by olcrtc-setup.sh
OLCRTC_CARRIER=$CARRIER
OLCRTC_TRANSPORT=$TRANSPORT
OLCRTC_LINK=direct
OLCRTC_ROOM_ID=$ROOM_ID
OLCRTC_KEY=$KEY
OLCRTC_DNS=$DNS_DEFAULT
OLCRTC_DEBUG=$DEBUG_FLAG
OLCRTC_SOCKS_PROXY=$SOCKS_PROXY
OLCRTC_WARP_PROXY=$EXISTING_WARP_PROXY
OLCRTC_NAME=$NAME
OLCRTC_VP8_FPS=$VP8_FPS
OLCRTC_VP8_BATCH=$VP8_BATCH
OLCRTC_SUB_ENABLED=$SUB_ENABLED
OLCRTC_SUB_PORT=$SUB_PORT
EOF
    chown root:olcrtc "$ENV_FILE"
    chmod 0640 "$ENV_FILE"

    # Start service
    echo "[*] Restarting olcrtc-server..."
    systemctl enable --quiet olcrtc-server.service
    systemctl restart olcrtc-server.service

    # Auto-detect room ID for wbstream / jazz
    if [ "$ROOM_ID" = "any" ]; then
        if ! wait_for_room_id; then
            exit 1
        fi
    fi

    # Install qrencode
    ensure_qrencode || true

    # Final output
    PUBLIC_IP="$(get_public_ip)"
    DEBUG_HUMAN="$(debug_human "$DEBUG_FLAG")"
    PROXY_HUMAN="$(proxy_human "$SOCKS_PROXY")"
    TRANSPORT_HUMAN="$(transport_human "$TRANSPORT")"

    cat <<EOF

==========================================================
        olcRTC server is up.
==========================================================

  Carrier:         $CARRIER
  Transport:       $TRANSPORT_HUMAN
  Room ID:         $ROOM_ID
  Key (hex):       $KEY
  DNS:             $DNS_DEFAULT
  Debug:           $DEBUG_HUMAN
  Proxy:           $PROXY_HUMAN
  Public IP:       $PUBLIC_IP
  Name:            $NAME

  URI для импорта в приложение:
EOF
    show_uri_qr "$CARRIER" "$ROOM_ID" "$KEY" "$NAME"

    cat <<EOF

  --- Управление сервисом ---
  Статус:   systemctl status olcrtc-server
  Логи:     journalctl -u olcrtc-server -f
  Меню:     curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
==========================================================
EOF
}

# ══════════════════════════════════════════════════════════════════════════════
#  MAIN: decide mode
# ══════════════════════════════════════════════════════════════════════════════

if is_installed && [ "$REGENERATE_ROOM" -eq 0 ] && [ "$REGENERATE_KEY" -eq 0 ] \
    && [ "$SET_SOCKS_PROXY" = "__keep__" ] && [ "$SET_DEBUG" = "__keep__" ] \
    && [ "$SET_TRANSPORT" = "__keep__" ] && [ "$SET_CARRIER" = "__keep__" ] \
    && [ -z "$SET_NAME" ] && [ -z "$SET_TELEMOST_ID" ]; then
    # No CLI flags that imply install/update → interactive menu
    run_menu
else
    run_install
fi
