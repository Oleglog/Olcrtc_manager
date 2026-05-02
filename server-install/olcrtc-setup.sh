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
#   - /etc/olcrtc/env                          OLCRTC_PROVIDER / ROOM_ID / KEY / DNS / DEBUG / SOCKS_PROXY / NAME
#   - /etc/olcrtc/key.hex                      32-byte encryption key (hex)
#
# Idempotent. Re-run with --regenerate to get a fresh room ID; re-run with
# --regenerate-key to also rotate the encryption key.

set -euo pipefail

INSTALLER_VERSION="0.2.0"
PROVIDER_DEFAULT="wb_stream"
DNS_DEFAULT="1.1.1.1:53"

REGENERATE_ROOM=0
REGENERATE_KEY=0
PROVIDER="${OLCRTC_PROVIDER:-$PROVIDER_DEFAULT}"
SET_SOCKS_PROXY="__keep__"
SET_DEBUG="__keep__"
SET_NAME=""
SET_TELEMOST_ID=""

CONFIG_DIR=/etc/olcrtc
STATE_DIR=/var/lib/olcrtc
ENV_FILE=$CONFIG_DIR/env
KEY_FILE=$CONFIG_DIR/key.hex

# ── Helper: update a single key in the env file ──────────────────────────────
set_env_value() {
    local key="$1" value="$2"
    if grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
        sed -i -E "s|^${key}=.*|${key}=${value}|" "$ENV_FILE"
    else
        echo "${key}=${value}" >> "$ENV_FILE"
    fi
}

# ── Helper: read a value from the env file ───────────────────────────────────
get_env_value() {
    local key="$1"
    grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d= -f2-
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
show_uri_qr() {
    local provider="$1" room_id="$2" key_hex="$3" name="$4"
    local uri="olcrtc://${provider}@room/${room_id}?key=${key_hex}#${name}"
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
    echo "[*] Waiting for $PROVIDER to create a room..."
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

# ──────────────────────────────────────────────────────────────────────────────
#  CLI argument parsing
# ──────────────────────────────────────────────────────────────────────────────

usage() {
    cat <<EOF
Usage: sudo ./olcrtc-setup.sh [options]

Options:
    --provider <wb_stream|telemost|jazz>
                          Pick a provider (default: $PROVIDER_DEFAULT).
    --regenerate          Drop the saved room ID and create a new one.
    --regenerate-key      Drop the encryption key AND room ID, regenerate both.
    --socks-proxy <[user:pass@]host:port>
                          Route provider signalling through a SOCKS5 proxy.
                          Pass "" to remove an existing setting.
    --debug               Enable verbose -debug logging.
    --no-debug            Disable verbose logging.
    --name <string>       Connection name shown in the client (default: <provider>_olcrtc).
    --id <room_id>        Room ID for Telemost (required with --provider telemost in CLI).
    -h, --help            Show this help.

Examples:
    sudo ./olcrtc-setup.sh
    sudo ./olcrtc-setup.sh --provider telemost --id 12345
    sudo ./olcrtc-setup.sh --provider jazz --regenerate
    sudo ./olcrtc-setup.sh --socks-proxy 1.2.3.4:1080
    sudo ./olcrtc-setup.sh --name my_server
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --provider) PROVIDER="$2"; shift 2 ;;
        --provider=*) PROVIDER="${1#*=}"; shift ;;
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

# ── Pre-flight checks ────────────────────────────────────────────────────────

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] This script must be run as root (try: sudo $0)" >&2
    exit 1
fi

case "$PROVIDER" in
    telemost|jazz|wb_stream) ;;
    *) echo "[!] Unsupported provider: $PROVIDER" >&2; exit 1 ;;
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
    [ -f "$ENV_FILE" ]
}

# ══════════════════════════════════════════════════════════════════════════════
#  INTERACTIVE MENU MODE
# ══════════════════════════════════════════════════════════════════════════════

run_menu() {
    ensure_qrencode || true

    while true; do
        local cur_provider cur_room cur_key cur_name cur_debug cur_proxy cur_ip
        cur_provider="$(get_env_value OLCRTC_PROVIDER)"
        cur_room="$(get_env_value OLCRTC_ROOM_ID)"
        cur_key="$(get_env_value OLCRTC_KEY)"
        cur_name="$(get_env_value OLCRTC_NAME)"
        cur_debug="$(get_env_value OLCRTC_DEBUG)"
        cur_proxy="$(get_env_value OLCRTC_SOCKS_PROXY)"
        cur_ip="$(get_public_ip)"

        [ -z "$cur_name" ] && cur_name="${cur_provider}_olcrtc"

        echo ""
        echo "============================================================"
        echo "  olcRTC Server Manager"
        echo "  Provider: $cur_provider | Room: $cur_room | IP: $cur_ip"
        echo "============================================================"
        echo ""
        echo "  1) Статус сервиса"
        echo "  2) Показать URI / QR-код"
        echo "  3) Сменить провайдера"
        echo "  4) Пересоздать room ID  (--regenerate)"
        echo "  5) Ротация ключа + room ID  (--regenerate-key)"
        echo "  6) Настроить SOCKS5-прокси"
        echo "  7) Убрать SOCKS5-прокси"
        echo "  8) Включить / выключить debug-логирование"
        echo "  9) Переименовать соединение (name)"
        echo "  0) Выход"
        echo ""
        read -rp "Выберите пункт: " choice

        case "$choice" in
        1)  # Статус сервиса
            echo ""
            systemctl --no-pager status olcrtc-server || true
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        2)  # Показать URI / QR-код
            show_uri_qr "$cur_provider" "$cur_room" "$cur_key" "$cur_name"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        3)  # Сменить провайдера
            echo ""
            echo "  Текущий провайдер: $cur_provider"
            echo ""
            echo "  1) wb_stream"
            echo "  2) jazz"
            echo "  3) telemost"
            echo ""
            read -rp "  Выберите провайдера [1-3]: " pchoice
            local new_provider=""
            case "$pchoice" in
                1) new_provider="wb_stream" ;;
                2) new_provider="jazz" ;;
                3) new_provider="telemost" ;;
                *) echo "  [!] Неверный выбор"; read -rp "[Enter для продолжения]"; continue ;;
            esac

            set_env_value "OLCRTC_PROVIDER" "$new_provider"

            if [ "$new_provider" = "telemost" ]; then
                read -rp "  Введите Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room"
                ROOM_ID="$new_room"
            else
                set_env_value "OLCRTC_ROOM_ID" "any"
                ROOM_ID="any"
            fi

            PROVIDER="$new_provider"
            systemctl restart olcrtc-server.service

            if [ "$ROOM_ID" = "any" ]; then
                if ! wait_for_room_id; then
                    read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            # Re-read updated values
            cur_room="$(get_env_value OLCRTC_ROOM_ID)"
            cur_name="$(get_env_value OLCRTC_NAME)"
            [ -z "$cur_name" ] && cur_name="${new_provider}_olcrtc"
            cur_key="$(get_env_value OLCRTC_KEY)"

            echo ""
            echo "  Провайдер изменён на: $new_provider"
            show_uri_qr "$new_provider" "$cur_room" "$cur_key" "$cur_name"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        4)  # Пересоздать room ID
            set_env_value "OLCRTC_ROOM_ID" "any"
            ROOM_ID="any"
            PROVIDER="$cur_provider"
            systemctl restart olcrtc-server.service

            if [ "$cur_provider" = "telemost" ]; then
                read -rp "  Введите новый Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room"
                ROOM_ID="$new_room"
                systemctl restart olcrtc-server.service
            else
                if ! wait_for_room_id; then
                    read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID)"
            echo ""
            echo "  Room ID обновлён: $cur_room"
            show_uri_qr "$cur_provider" "$cur_room" "$cur_key" "$cur_name"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        5)  # Ротация ключа + room ID
            echo ""
            read -rp "  Все существующие клиенты потеряют подключение. Продолжить? [y/N] " confirm
            if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
                echo "  Отменено."
                read -rp "[Enter для продолжения]"
                continue
            fi

            echo "[*] Generating fresh 256-bit encryption key..."
            umask 077
            openssl rand -hex 32 > "$KEY_FILE"
            chown root:olcrtc "$KEY_FILE"
            chmod 0640 "$KEY_FILE"
            local new_key
            new_key="$(cat "$KEY_FILE")"
            set_env_value "OLCRTC_KEY" "$new_key"
            set_env_value "OLCRTC_ROOM_ID" "any"
            ROOM_ID="any"
            PROVIDER="$cur_provider"
            systemctl restart olcrtc-server.service

            if [ "$cur_provider" = "telemost" ]; then
                read -rp "  Введите новый Room ID для Telemost: " new_room
                if [ -z "$new_room" ]; then
                    echo "  [!] Room ID не может быть пустым"
                    read -rp "[Enter для продолжения]"
                    continue
                fi
                set_env_value "OLCRTC_ROOM_ID" "$new_room"
                ROOM_ID="$new_room"
                systemctl restart olcrtc-server.service
            else
                if ! wait_for_room_id; then
                    read -rp "[Enter для продолжения]"
                    continue
                fi
            fi

            cur_room="$(get_env_value OLCRTC_ROOM_ID)"
            cur_key="$(get_env_value OLCRTC_KEY)"
            echo ""
            echo "  Ключ и Room ID обновлены."
            show_uri_qr "$cur_provider" "$cur_room" "$cur_key" "$cur_name"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        6)  # Настроить SOCKS5-прокси
            echo ""
            read -rp "  Введите адрес прокси [user:pass@]host:port: " new_proxy
            if [ -z "$new_proxy" ] || [[ "$new_proxy" != *":"* ]]; then
                echo "  [!] Неверный формат. Ожидается [user:pass@]host:port"
                read -rp "[Enter для продолжения]"
                continue
            fi
            set_env_value "OLCRTC_SOCKS_PROXY" "$new_proxy"
            systemctl restart olcrtc-server.service
            echo "  Прокси установлен: $new_proxy"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        7)  # Убрать SOCKS5-прокси
            set_env_value "OLCRTC_SOCKS_PROXY" ""
            systemctl restart olcrtc-server.service
            echo "  Прокси удалён"
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        8)  # Включить / выключить debug
            if [ -n "$cur_debug" ] && [ "$cur_debug" != "0" ] && [ "$cur_debug" != "false" ]; then
                set_env_value "OLCRTC_DEBUG" ""
                echo "  Debug выключен"
            else
                set_env_value "OLCRTC_DEBUG" "1"
                echo "  Debug включён"
            fi
            systemctl restart olcrtc-server.service
            echo ""
            read -rp "[Enter для продолжения]"
            ;;

        9)  # Переименовать соединение
            echo ""
            echo "  Текущее имя: $cur_name"
            read -rp "  Введите новое имя: " new_name
            if [ -z "$new_name" ]; then
                echo "  [!] Имя не может быть пустым"
                read -rp "[Enter для продолжения]"
                continue
            fi
            set_env_value "OLCRTC_NAME" "$new_name"
            cur_room="$(get_env_value OLCRTC_ROOM_ID)"
            cur_key="$(get_env_value OLCRTC_KEY)"
            cur_provider="$(get_env_value OLCRTC_PROVIDER)"
            show_uri_qr "$cur_provider" "$cur_room" "$cur_key" "$new_name"
            echo ""
            read -rp "[Enter для продолжения]"
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

    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    SRC_BIN="$SCRIPT_DIR/bin/$BIN_NAME"
    SRC_UNIT="$SCRIPT_DIR/systemd/olcrtc-server.service"
    SRC_LAUNCHER="$SCRIPT_DIR/systemd/olcrtc-launcher"

    RELEASE_TAG="server-v$INSTALLER_VERSION"
    RELEASE_URL_BASE="https://github.com/Oleglog/olcrtc_FORK/releases/download/$RELEASE_TAG"

    # Download binary if not bundled
    if [ ! -f "$SRC_BIN" ]; then
        if ! command -v curl >/dev/null 2>&1; then
            echo "[!] Binary not bundled and curl is not installed. Install curl or download" >&2
            echo "    $BIN_NAME from $RELEASE_URL_BASE/$BIN_NAME and place it in $SCRIPT_DIR/bin/" >&2
            exit 1
        fi
        install -d -m 0755 "$SCRIPT_DIR/bin"
        echo "[*] Binary not bundled. Downloading $BIN_NAME from $RELEASE_TAG release..."
        if ! curl -fsSL "$RELEASE_URL_BASE/$BIN_NAME" -o "$SRC_BIN.tmp"; then
            echo "[!] Failed to download $BIN_NAME from $RELEASE_URL_BASE/" >&2
            echo "[!] Check that release '$RELEASE_TAG' exists, or build from source: see server-install/build-from-source.sh" >&2
            rm -f "$SRC_BIN.tmp"
            exit 1
        fi
        mv "$SRC_BIN.tmp" "$SRC_BIN"
        echo "[*] Downloaded to $SRC_BIN"
    fi

    if [ ! -x "$SRC_BIN" ]; then
        chmod +x "$SRC_BIN" 2>/dev/null || true
    fi

    if [ ! -f "$SRC_BIN" ]; then
        echo "[!] Missing binary: $SRC_BIN" >&2; exit 1
    fi
    if [ ! -f "$SRC_UNIT" ]; then
        echo "[!] Missing systemd unit: $SRC_UNIT" >&2; exit 1
    fi
    if [ ! -f "$SRC_LAUNCHER" ]; then
        echo "[!] Missing launcher script: $SRC_LAUNCHER" >&2; exit 1
    fi

    echo "[*] olcRTC server installer v$INSTALLER_VERSION"
    echo "[*] Detected architecture: $ARCH ($BIN_NAME)"

    # Create system user
    if ! id olcrtc >/dev/null 2>&1; then
        echo "[*] Creating olcrtc system user..."
        useradd --system --no-create-home --shell /usr/sbin/nologin --home-dir "$STATE_DIR" olcrtc
    fi

    # Install binaries
    echo "[*] Installing binaries..."
    install -m 0755 -o root -g root "$SRC_BIN" /usr/local/bin/olcrtc
    install -m 0755 -o root -g root "$SRC_LAUNCHER" /usr/local/bin/olcrtc-launcher

    # Prepare directories
    echo "[*] Preparing $CONFIG_DIR and $STATE_DIR..."
    install -d -m 0750 -o root -g olcrtc "$CONFIG_DIR"
    install -d -m 0750 -o olcrtc -g olcrtc "$STATE_DIR"

    # Generate or reuse the encryption key
    if [ "$REGENERATE_KEY" -eq 1 ] || [ ! -s "$KEY_FILE" ]; then
        echo "[*] Generating fresh 256-bit encryption key..."
        umask 077
        openssl rand -hex 32 > "$KEY_FILE"
        chown root:olcrtc "$KEY_FILE"
        chmod 0640 "$KEY_FILE"
    fi
    KEY="$(cat "$KEY_FILE")"

    # Install systemd unit
    echo "[*] Installing systemd unit..."
    install -m 0644 "$SRC_UNIT" /etc/systemd/system/olcrtc-server.service
    systemctl daemon-reload

    # Read previous env to preserve untouched fields
    EXISTING_ROOM=""
    EXISTING_SOCKS_PROXY=""
    EXISTING_DEBUG=""
    EXISTING_NAME=""
    if [ -f "$ENV_FILE" ]; then
        EXISTING_ROOM="$(get_env_value OLCRTC_ROOM_ID)"
        EXISTING_SOCKS_PROXY="$(get_env_value OLCRTC_SOCKS_PROXY)"
        EXISTING_DEBUG="$(get_env_value OLCRTC_DEBUG)"
        EXISTING_NAME="$(get_env_value OLCRTC_NAME)"
    fi
    if [ "$REGENERATE_ROOM" -eq 1 ]; then
        EXISTING_ROOM=""
    fi

    # Decide final SOCKS proxy / debug values
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

    # Decide name
    if [ -n "$SET_NAME" ]; then
        NAME="$SET_NAME"
    elif [ -n "$EXISTING_NAME" ]; then
        NAME="$EXISTING_NAME"
    else
        NAME="${PROVIDER}_olcrtc"
    fi

    # Decide initial room ID
    if [ -n "$EXISTING_ROOM" ] && [ "$EXISTING_ROOM" != "any" ]; then
        ROOM_ID="$EXISTING_ROOM"
        echo "[*] Reusing existing room ID: $ROOM_ID"
    elif [ "$PROVIDER" = "telemost" ]; then
        if [ -n "$SET_TELEMOST_ID" ]; then
            ROOM_ID="$SET_TELEMOST_ID"
        else
            # Interactive mode: ask user for room ID
            read -rp "[?] Введите Room ID для Telemost: " ROOM_ID
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
OLCRTC_PROVIDER=$PROVIDER
OLCRTC_ROOM_ID=$ROOM_ID
OLCRTC_KEY=$KEY
OLCRTC_DNS=$DNS_DEFAULT
OLCRTC_DEBUG=$DEBUG_FLAG
OLCRTC_SOCKS_PROXY=$SOCKS_PROXY
OLCRTC_NAME=$NAME
EOF
    chown root:olcrtc "$ENV_FILE"
    chmod 0640 "$ENV_FILE"

    # Start service
    echo "[*] Restarting olcrtc-server..."
    systemctl enable --quiet olcrtc-server.service
    systemctl restart olcrtc-server.service

    # Auto-detect room ID for wb_stream / jazz
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

    cat <<EOF

==========================================================
        olcRTC server is up.
==========================================================

  Provider:        $PROVIDER
  Room ID:         $ROOM_ID
  Key (hex):       $KEY
  DNS:             $DNS_DEFAULT
  Debug:           $DEBUG_HUMAN
  Proxy:           $PROXY_HUMAN
  Public IP:       $PUBLIC_IP
  Name:            $NAME

  URI для импорта в приложение:
EOF
    show_uri_qr "$PROVIDER" "$ROOM_ID" "$KEY" "$NAME"

    cat <<EOF

  --- Управление сервисом ---
  Статус:   systemctl status olcrtc-server
  Логи:     journalctl -u olcrtc-server -f
  Меню:     sudo $(realpath "$0")
==========================================================
EOF
}

# ══════════════════════════════════════════════════════════════════════════════
#  MAIN: decide mode
# ══════════════════════════════════════════════════════════════════════════════

if is_installed && [ "$REGENERATE_ROOM" -eq 0 ] && [ "$REGENERATE_KEY" -eq 0 ] \
    && [ "$SET_SOCKS_PROXY" = "__keep__" ] && [ "$SET_DEBUG" = "__keep__" ] \
    && [ -z "$SET_NAME" ] && [ -z "$SET_TELEMOST_ID" ]; then
    # No CLI flags that imply install/update → interactive menu
    run_menu
else
    run_install
fi
