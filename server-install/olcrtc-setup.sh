#!/usr/bin/env bash
# olcRTC Installer v1.0.0 — install-only, no management menu.
# Installs olcrtc + olcrtc-admin binaries, systemd units, generates keys/env.
# Re-run without flags shows status and Admin UI URL.
#
# Usage:
#   curl -fsSL .../olcrtc-setup.sh | sudo bash
#   sudo bash olcrtc-setup.sh --update
#   sudo bash olcrtc-setup.sh --uninstall
#   sudo bash olcrtc-setup.sh --show-token

set -euo pipefail

# Some sudo configurations strip /usr/sbin and /usr/local/sbin from PATH,
# which makes `command -v nginx` fail even when nginx is installed (e.g. by
# 3x-ui). Always ensure system sbin dirs are searched.
export PATH="${PATH}:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/snap/bin:/usr/local/nginx/sbin:/opt/nginx/sbin"

INSTALLER_VERSION="1.0.7"
CARRIER_DEFAULT="wbstream"
TRANSPORT_DEFAULT="datachannel"
DNS_DEFAULT="1.1.1.1:53"

CONFIG_DIR=/etc/olcrtc
STATE_DIR=/var/lib/olcrtc
ADMIN_ENV=$CONFIG_DIR/admin.env
ENV_FILE=$CONFIG_DIR/env
KEY_FILE=$CONFIG_DIR/key.hex

# ── Flags ────────────────────────────────────────────────────────────────────
DO_UPDATE=0
DO_UNINSTALL=0
DO_SHOW_TOKEN=0
DO_REGENERATE=0
DO_REGENERATE_KEY=0
DO_STATUS=0
DO_SETUP_DOMAIN=0
DO_REMOVE_DOMAIN=0

CARRIER=""
TRANSPORT=""
SET_NAME=""
SET_ID=""
SETUP_DOMAIN=""

# ── Helpers ──────────────────────────────────────────────────────────────────
tty_read() {
    if [ -t 0 ]; then read "$@"; else read "$@" < /dev/tty; fi
}

get_env_value() {
    local key="$1" file="${2:-$ENV_FILE}"
    grep -E "^${key}=" "$file" 2>/dev/null | tail -1 | cut -d= -f2- || true
}

set_env_value() {
    local key="$1" value="$2" file="${3:-$ENV_FILE}"
    if [ ! -f "$file" ]; then
        install -d -m 0750 "$(dirname "$file")"
        echo "${key}=${value}" > "$file"
        return
    fi
    if grep -qE "^${key}=" "$file" 2>/dev/null; then
        local tmp
        tmp="$(mktemp)"
        while IFS= read -r line || [ -n "$line" ]; do
            case "$line" in
                "${key}="*) echo "${key}=${value}" ;;
                *) echo "$line" ;;
            esac
        done < "$file" > "$tmp"
        mv "$tmp" "$file"
    else
        echo "${key}=${value}" >> "$file"
    fi
}

normalize_carrier() {
    case "$1" in
        wb_stream) echo "wbstream" ;;
        *) echo "$1" ;;
    esac
}

get_public_ip() {
    curl -fsS --max-time 3 https://api.ipify.org 2>/dev/null || echo "unknown"
}

is_installed() {
    [ -f /usr/local/bin/olcrtc ] && [ -f /etc/systemd/system/olcrtc-server.service ] && [ -f "$ENV_FILE" ]
}

# nginx_present — returns 0 if nginx is installed in any common way, even if
# `command -v nginx` doesn't find it. Critical for hosts where nginx was
# installed by 3x-ui / a control panel into a non-standard prefix, or where
# sudo's secure_path hides /usr/sbin.
nginx_present() {
    command -v nginx >/dev/null 2>&1 && return 0
    for p in /usr/sbin/nginx /usr/local/sbin/nginx /usr/local/nginx/sbin/nginx \
             /opt/nginx/sbin/nginx /snap/bin/nginx; do
        [ -x "$p" ] && return 0
    done
    # Config-dir presence is also a strong signal (3x-ui / custom builds).
    [ -f /etc/nginx/nginx.conf ] && return 0
    return 1
}

# external_https_listener — returns 0 if something other than our own nginx
# server block is listening on :443 (e.g. xray/3x-ui standalone, caddy).
# Used to refuse certbot --standalone fallbacks that would `systemctl stop
# nginx` and break those services.
external_https_listener() {
    pgrep -x xray >/dev/null 2>&1 && return 0
    systemctl is-active --quiet x-ui 2>/dev/null && return 0
    systemctl is-active --quiet sing-box 2>/dev/null && return 0
    systemctl is-active --quiet caddy 2>/dev/null && return 0
    return 1
}

# caddy_present — returns 0 if caddy is installed.
caddy_present() {
    command -v caddy >/dev/null 2>&1 && return 0
    for p in /usr/bin/caddy /usr/local/bin/caddy /usr/sbin/caddy; do
        [ -x "$p" ] && return 0
    done
    return 1
}

# caddy_config_path — prints the active Caddyfile path on stdout. Tries the
# Debian package default first, then parses --config from the systemd unit's
# ExecStart. Returns non-zero if no Caddyfile is found.
caddy_config_path() {
    if [ -f /etc/caddy/Caddyfile ]; then
        echo /etc/caddy/Caddyfile
        return 0
    fi
    local exec_start config_path
    exec_start="$(systemctl show caddy --property=ExecStart 2>/dev/null | head -1)"
    if [ -n "$exec_start" ]; then
        config_path="$(echo "$exec_start" | sed -n 's/.*--config \([^ ;]*\).*/\1/p' | head -1)"
        if [ -n "$config_path" ] && [ -f "$config_path" ]; then
            echo "$config_path"
            return 0
        fi
    fi
    return 1
}

# port_listener — prints the program name listening on the given port (best
# effort). E.g. `port_listener 443` -> "caddy" or "nginx" or empty.
#
# Uses a single awk invocation so the function never returns non-zero on "no
# match" (which under `set -euo pipefail` killed the whole script silently
# when grep -oP / sed couldn't find anything).
port_listener() {
    local port="$1"
    ss -tlnp 2>/dev/null | awk -v port="$port" '
        # ss columns: State Recv-Q Send-Q Local-Addr:Port Peer-Addr:Port Users
        # Match e.g. *:443, 0.0.0.0:443, [::]:443, 127.0.0.1:443 — anchored to
        # end-of-field so :2096 does not match :2.
        $4 ~ ":" port "$" {
            if (match($0, /users:\(\("[^"]+"/)) {
                s = substr($0, RSTART, RLENGTH)
                sub(/^users:\(\("/, "", s)
                sub(/"$/, "", s)
                print s
                exit
            }
        }
    '
}

# ── Download helpers ─────────────────────────────────────────────────────────
detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) echo "unsupported:$arch" ;;
    esac
}

download_release() {
    local name="$1" dest="$2"
    local repo="Oleglog/Olcrtc_manager"
    local tag="server-v${INSTALLER_VERSION}"
    local url="https://github.com/${repo}/releases/download/${tag}/${name}"

    # Refuse Windows binaries on Linux installs
    if [[ "$name" == *.exe ]] || [[ "$name" == *-windows-* ]]; then
        echo "[!] Refusing to download Windows binary on Linux: $name" >&2
        return 1
    fi

    if [ -f "$dest" ]; then rm -f "$dest"; fi
    if curl -fsSL --max-time 30 "$url" -o "$dest.tmp"; then
        # Verify downloaded file is a Linux ELF binary
        if command -v file >/dev/null 2>&1; then
            local ftype
            ftype="$(file -b "$dest.tmp")"
            case "$ftype" in
                ELF*)
                    ;;
                *)
                    echo "[!] Downloaded file is not a Linux binary (type: $ftype): $name" >&2
                    rm -f "$dest.tmp"
                    return 1
                    ;;
            esac
        fi
        # Sanity check: file must not be empty and should have reasonable size
        local size
        size="$(stat -c%s "$dest.tmp" 2>/dev/null || stat -f%z "$dest.tmp" 2>/dev/null || echo 0)"
        if [ "$size" -lt 1024 ]; then
            echo "[!] Downloaded file is too small ($size bytes), likely not a binary: $name" >&2
            rm -f "$dest.tmp"
            return 1
        fi
        mv "$dest.tmp" "$dest"
        chmod +x "$dest"
        return 0
    fi
    rm -f "$dest.tmp"
    return 1
}

# ── Domain helpers ────────────────────────────────────────────────────────────
validate_domain() {
    local d="$1"
    # Reject empty, IP addresses, wildcards, and invalid chars.
    [ -z "$d" ] && return 1
    echo "$d" | grep -qE '^\*' && return 1
    echo "$d" | grep -qP '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' && return 1
    echo "$d" | grep -qP '^[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]*[a-zA-Z0-9])?)+$'
}

dns_points_here() {
    local d="$1"
    local my_ip
    my_ip="$(get_public_ip)"
    [ "$my_ip" = "unknown" ] && return 0  # skip check if we can't determine IP
    local resolved
    resolved="$(dig +short "$d" A 2>/dev/null || host -t A "$d" 2>/dev/null | awk '/has address/{print $NF}')"
    echo "$resolved" | grep -qF "$my_ip"
}

ensure_sites_dirs() {
    mkdir -p /etc/nginx/sites-available /etc/nginx/sites-enabled
    # Make sure nginx includes sites-enabled.
    if ! grep -q 'sites-enabled' /etc/nginx/nginx.conf 2>/dev/null; then
        if grep -q 'http {' /etc/nginx/nginx.conf 2>/dev/null; then
            sed -i '/http {/a\    include /etc/nginx/sites-enabled/*;' /etc/nginx/nginx.conf
        fi
    fi
}

find_certbot() {
    # 1. Already in PATH — best case.
    if command -v certbot >/dev/null 2>&1; then return 0; fi
    # 2. Probe every place certbot can land depending on how it was installed
    #    (apt, snap, pip, certbot-auto, control panels like 3x-ui).
    for p in /snap/bin/certbot /usr/local/bin/certbot /usr/bin/certbot \
             /opt/certbot/bin/certbot /root/.local/bin/certbot; do
        if [ -x "$p" ]; then
            # Make it accessible without rebuilding PATH each call.
            ln -sf "$p" /usr/local/bin/certbot 2>/dev/null || true
            return 0
        fi
    done
    return 1
}

# apt_install — wrapper around apt-get that:
#   1. forces non-interactive frontend (no debconf prompts that hang forever),
#   2. shows package-manager output to the user instead of silently swallowing
#      it (so a broken DNS / locked dpkg / 404 mirror is visible),
#   3. returns the apt exit code so callers can react.
apt_install() {
    if ! command -v apt-get >/dev/null 2>&1; then
        return 127
    fi
    DEBIAN_FRONTEND=noninteractive apt-get update -qq -o=Dpkg::Use-Pty=0 || return $?
    DEBIAN_FRONTEND=noninteractive apt-get install -y -q -o=Dpkg::Use-Pty=0 \
        -o Dpkg::Options::="--force-confdef" \
        -o Dpkg::Options::="--force-confold" \
        "$@"
}

# snap_install_certbot — best-effort certbot install via snap when apt fails or
# isn't available (e.g. CentOS/Rocky/Fedora hosts running 3x-ui). Returns 0 on
# success, non-zero otherwise.
snap_install_certbot() {
    command -v snap >/dev/null 2>&1 || return 1
    snap install --classic certbot >/dev/null 2>&1 || return 1
    [ -x /snap/bin/certbot ] || return 1
    ln -sf /snap/bin/certbot /usr/local/bin/certbot 2>/dev/null || true
    return 0
}

# ── Caddy domain helpers ─────────────────────────────────────────────────────
# Marker strings used to delimit our managed block inside the Caddyfile so we
# can idempotently replace it on re-runs and surgically remove it on
# --remove-domain without touching the rest of the user's caddy config.
CADDY_MARK_START="# olcrtc:sub:start"
CADDY_MARK_END="# olcrtc:sub:end"

# do_caddy_setup <domain> <sub_port> <caddyfile> — append or replace the
# olcrtc:sub managed block in the user's Caddyfile, validate, reload.
do_caddy_setup() {
    local domain="$1" sub_port="$2" caddyfile="$3"

    [ -z "$caddyfile" ] && { echo "  [!] do_caddy_setup: Caddyfile не задан" >&2; return 1; }
    [ -f "$caddyfile" ] || { echo "  [!] do_caddy_setup: $caddyfile не существует" >&2; return 1; }

    # Backup before modification.
    local backup_path
    backup_path="${caddyfile}.bak.$(date +%s)"
    cp "$caddyfile" "$backup_path"
    echo "         Бэкап Caddyfile: $backup_path"

    # Strip any previous olcrtc:sub block so re-runs are idempotent.
    if grep -q "$CADDY_MARK_START" "$caddyfile"; then
        sed -i "/$CADDY_MARK_START/,/$CADDY_MARK_END/d" "$caddyfile"
        # Collapse double-blank lines left over from the deletion.
        sed -i '/^$/N;/^\n$/d' "$caddyfile"
    fi

    # Append the new block at EOF.
    {
        echo ""
        echo "$CADDY_MARK_START $domain"
        echo "$domain {"
        echo "    reverse_proxy 127.0.0.1:${sub_port}"
        echo "}"
        echo "$CADDY_MARK_END"
    } >> "$caddyfile"

    # Validate. `caddy validate` returns non-zero on syntax errors.
    if ! caddy validate --config "$caddyfile" --adapter caddyfile >/dev/null 2>&1; then
        echo "  [!] caddy validate не прошёл после правки." >&2
        echo "      Откатываю Caddyfile из бэкапа: $backup_path" >&2
        cp "$backup_path" "$caddyfile"
        return 1
    fi

    # Reload via systemd if caddy is managed by systemd, else use caddy CLI.
    if systemctl is-active --quiet caddy 2>/dev/null; then
        if ! systemctl reload caddy 2>&1; then
            echo "  [!] systemctl reload caddy завершился с ошибкой." >&2
            return 1
        fi
    else
        if ! caddy reload --config "$caddyfile" --adapter caddyfile >/dev/null 2>&1; then
            echo "  [!] caddy reload завершился с ошибкой." >&2
            return 1
        fi
    fi
    echo "         caddy перезагружен ✓"
}

# do_caddy_remove <domain?> — strip the olcrtc:sub block from the Caddyfile and
# reload. Safe to call when no block exists.
do_caddy_remove() {
    local caddyfile
    if ! caddyfile="$(caddy_config_path)"; then
        return 0  # no caddy config — nothing to remove
    fi
    grep -q "$CADDY_MARK_START" "$caddyfile" 2>/dev/null || return 0

    local backup_path
    backup_path="${caddyfile}.bak.$(date +%s)"
    cp "$caddyfile" "$backup_path"
    echo "  Бэкап Caddyfile: $backup_path"

    sed -i "/$CADDY_MARK_START/,/$CADDY_MARK_END/d" "$caddyfile"
    sed -i '/^$/N;/^\n$/d' "$caddyfile"

    if ! caddy validate --config "$caddyfile" --adapter caddyfile >/dev/null 2>&1; then
        echo "  [!] caddy validate не прошёл после удаления блока, откатываю." >&2
        cp "$backup_path" "$caddyfile"
        return 1
    fi
    if systemctl is-active --quiet caddy 2>/dev/null; then
        systemctl reload caddy 2>/dev/null || true
    else
        caddy reload --config "$caddyfile" --adapter caddyfile >/dev/null 2>&1 || true
    fi
    echo "  Удалён блок olcrtc:sub из $caddyfile ✓"
}

# ── Domain setup ─────────────────────────────────────────────────────────────
do_setup_domain() {
    local domain="$1"

    # ── 0. Validate domain ──
    if ! validate_domain "$domain"; then
        echo "  [!] Некорректный домен: $domain" >&2
        echo "      Домен должен быть вида sub.example.com" >&2
        echo "      Wildcards (*.example.com) и IP-адреса не поддерживаются." >&2
        return 1
    fi

    # Read subscription port from env.
    local sub_port
    sub_port="$(get_env_value OLCRTC_SUB_PORT "$ENV_FILE" 2>/dev/null)"
    sub_port="${sub_port:-$(get_env_value OLCRTC_SUB_PORT "$ADMIN_ENV" 2>/dev/null)}"
    sub_port="${sub_port:-2096}"

    echo "[*] Настройка домена $domain для подписок (порт подписок: $sub_port)..."
    echo ""

    # ── 1. DNS pre-check ──
    echo "  [1/6] Проверка DNS..."
    if command -v dig >/dev/null 2>&1 || command -v host >/dev/null 2>&1; then
        if ! dns_points_here "$domain"; then
            local my_ip
            my_ip="$(get_public_ip)"
            echo "  [!] ВНИМАНИЕ: A-запись $domain не указывает на IP этого сервера ($my_ip)." >&2
            echo "      Certbot не сможет получить сертификат, пока DNS не будет настроен." >&2
            echo "      Добавьте A-запись: $domain → $my_ip" >&2
            echo "" >&2
            local dns_continue=""
            tty_read -rp "      Продолжить всё равно? [y/N]: " dns_continue
            if [ "$dns_continue" != "y" ] && [ "$dns_continue" != "Y" ]; then
                echo "  Отменено." >&2
                return 1
            fi
        else
            echo "         DNS OK ✓"
        fi
    else
        echo "         (dig/host не найден, пропускаем проверку DNS)"
    fi

    # ── 2. Install nginx if missing ──
    # Use nginx_present() (not just `command -v`) so we never run apt-get on
    # systems where nginx was installed by 3x-ui, snap, or built from source.
    if nginx_present; then
        echo "  [2/6] nginx уже установлен ✓"
    else
        echo "  [2/6] Установка nginx..."
        if ! command -v apt-get >/dev/null 2>&1; then
            echo "  [!] nginx не установлен и apt-get недоступен." >&2
            echo "      Установите nginx вручную и запустите скрипт снова." >&2
            return 1
        fi
        if ! apt_install nginx; then
            echo "  [!] apt-get install nginx завершился с ошибкой." >&2
            return 1
        fi
        if ! nginx_present; then
            echo "  [!] Не удалось установить nginx." >&2
            return 1
        fi
    fi
    ensure_sites_dirs

    # ── 3. Install certbot if missing ──
    if find_certbot; then
        echo "  [3/6] certbot уже установлен ✓"
    else
        echo "  [3/6] Установка certbot..."
        # Try apt first — most Debian/Ubuntu hosts. Show output so the user
        # can see what went wrong if it fails (locked dpkg, 404 mirror, etc).
        # On hosts without nginx-plugin support (e.g. snap-only) we still get
        # core certbot via the fallback below.
        apt_ok=0
        if apt_install certbot python3-certbot-nginx; then
            apt_ok=1
        elif apt_install certbot; then
            apt_ok=1
        fi
        if [ "$apt_ok" -ne 1 ] || ! find_certbot; then
            echo "  [3/6] apt не справился, пробуем snap..."
            if ! snap_install_certbot; then
                echo "  [!] Не удалось установить certbot ни через apt, ни через snap." >&2
                echo "      Установите certbot вручную и повторите:" >&2
                echo "        Debian/Ubuntu:  sudo apt install -y certbot python3-certbot-nginx" >&2
                echo "        snap (универс.): sudo snap install --classic certbot && \\" >&2
                echo "                          sudo ln -sf /snap/bin/certbot /usr/local/bin/certbot" >&2
                return 1
            fi
            if ! find_certbot; then
                echo "  [!] certbot установлен, но не найден в PATH." >&2
                return 1
            fi
        fi
    fi

    # ── 4. Detect front-door on :443 ──
    # We support three layouts:
    #   sni_mode=1   — nginx with a stream { ssl_preread } block (3x-ui+nginx).
    #   caddy_mode=1 — caddy is the :443 listener (3x-ui+caddy or pure caddy).
    #   neither     — standalone nginx on :443 (we'll add a normal server{}).
    local sni_mode=0
    local caddy_mode=0
    local stream_conf=""
    local has_proxy_protocol=0
    local stream_conf_count=0
    local caddyfile=""

    if grep -rq 'ssl_preread' /etc/nginx/ 2>/dev/null; then
        sni_mode=1
        stream_conf_count="$(grep -rl 'ssl_preread' /etc/nginx/ 2>/dev/null | wc -l)"
        stream_conf="$(grep -rl 'ssl_preread' /etc/nginx/ 2>/dev/null | head -1)"

        if [ "$stream_conf_count" -gt 1 ]; then
            echo "  [4/6] ВНИМАНИЕ: найдено $stream_conf_count файлов с ssl_preread:"
            grep -rl 'ssl_preread' /etc/nginx/ 2>/dev/null | while read -r f; do
                echo "           $f"
            done
            echo "         Используем первый: $stream_conf"
        else
            echo "  [4/6] Обнаружен SNI-мультиплексор nginx (3x-ui / xray)"
        fi
        echo "         Stream config: $stream_conf"

        # Detect proxy_protocol specifically in the server block that listens on 443.
        # Look for proxy_protocol within the same server{} block that has ssl_preread.
        if awk '/server\s*\{/{found=1; buf=""} found{buf=buf"\n"$0} /\}/{if(found && buf ~ /ssl_preread/ && buf ~ /proxy_protocol\s+on/) print "YES"; found=0}' "$stream_conf" 2>/dev/null | grep -q YES; then
            has_proxy_protocol=1
        elif grep -q 'proxy_protocol on' "$stream_conf" 2>/dev/null; then
            # Fallback: simple grep (less precise but handles common configs).
            has_proxy_protocol=1
        fi
        [ "$has_proxy_protocol" -eq 1 ] && echo "         proxy_protocol: да" || echo "         proxy_protocol: нет"
    else
        # nginx doesn't have a stream block. Inspect who actually owns :443.
        local p443
        p443="$(port_listener 443)"

        if [ "$p443" = "caddy" ] || { [ -z "$p443" ] && caddy_present && systemctl is-active --quiet caddy 2>/dev/null; }; then
            # Caddy is the front-door. We add an HTTPS reverse_proxy block to
            # the Caddyfile — caddy auto-acquires the LE cert.
            if ! caddy_present; then
                echo "  [!] caddy слушает :443, но бинарник не найден в PATH — странно." >&2
                return 1
            fi
            if ! caddyfile="$(caddy_config_path)"; then
                echo "  [4/6] Обнаружен caddy на :443, но Caddyfile не найден."
                echo "  [!] Не удалось определить активный Caddyfile." >&2
                echo "      Проверьте /etc/caddy/Caddyfile или флаг --config в systemd-юните caddy." >&2
                return 1
            fi
            caddy_mode=1
            echo "  [4/6] Обнаружен caddy на :443"
            echo "         Caddyfile: $caddyfile"
            echo "         certbot не нужен — caddy сам выпустит LE-сертификат."
        elif [ "$p443" = "nginx" ] || [ -z "$p443" ]; then
            # Either nginx already on :443 (we'll add a server block) or
            # nothing is listening yet (we'll bring up our own server block
            # after writing the config).
            echo "  [4/6] SNI-мультиплексор не обнаружен (стандартный nginx)"
        else
            # Some other process (xray/sing-box/haproxy) holds :443 directly
            # — we don't know how to safely inject our route.
            echo "  [4/6] Порт 443 занят процессом: $p443"
            echo "  [!] Автоматическая настройка не поддерживает эту схему." >&2
            echo "      Настройте reverse-proxy для $domain → 127.0.0.1:${sub_port} вручную." >&2
            echo "      Инструкция: https://github.com/Oleglog/Olcrtc_manager/blob/master/server-install/README.md" >&2
            return 1
        fi
    fi

    # ── 5. Obtain TLS certificate ──
    # Caddy handles ACME internally — don't run certbot at all.
    if [ "$caddy_mode" -eq 1 ]; then
        echo "  [5/6] Пропуск (caddy сам получит сертификат после reload) ✓"
    else
    echo "  [5/6] Получение TLS-сертификата..."
    local cert_path="/etc/letsencrypt/live/$domain/fullchain.pem"
    if [ -f "$cert_path" ]; then
        echo "         Сертификат уже существует ✓"
    else
        # Ensure /var/www/html exists for webroot challenges.
        mkdir -p /var/www/html

        if [ "$sni_mode" -eq 1 ]; then
            # In SNI mode the certbot --nginx plugin cannot work because
            # port 443 is handled by the stream block.  Use webroot via the
            # existing nginx http server (port 80) when available, or fall
            # back to standalone (temporarily stopping nginx if needed).
            if nginx -t >/dev/null 2>&1 && systemctl is-active --quiet nginx; then
                # Ensure a minimal server_name block exists for the domain
                # on port 80 so that webroot challenge can be served.
                if ! grep -rq "server_name.*${domain}" /etc/nginx/sites-enabled/ 2>/dev/null && \
                   ! grep -rq "server_name.*${domain}" /etc/nginx/conf.d/ 2>/dev/null; then
                    cat > /etc/nginx/sites-available/olcrtc-acme <<ACME
server {
    listen 80;
    server_name ${domain};
    root /var/www/html;
    location /.well-known/acme-challenge/ {
        allow all;
    }
}
ACME
                    ln -sf /etc/nginx/sites-available/olcrtc-acme /etc/nginx/sites-enabled/
                    nginx -t >/dev/null 2>&1 && systemctl reload nginx
                fi
                certbot certonly --webroot -w /var/www/html -d "$domain" \
                    --non-interactive --agree-tos --register-unsafely-without-email 2>&1 || {
                    # In SNI mode we MUST NOT `systemctl stop nginx` — that
                    # would tear down the stream block that 3x-ui/xray rely on
                    # for TLS routing. Fall back to standalone only if no
                    # external service is actively using nginx.
                    if external_https_listener; then
                        echo "  [!] webroot не сработал, а останавливать nginx нельзя" >&2
                        echo "      (активен xray/3x-ui/caddy через stream block)." >&2
                        echo "      Получите сертификат вручную:" >&2
                        echo "        sudo certbot certonly --webroot -w /var/www/html \\" >&2
                        echo "          -d $domain --agree-tos -m you@example.com" >&2
                        echo "      Затем повторите --setup-domain $domain." >&2
                        return 1
                    fi
                    echo "  [!] webroot не сработал, пробуем standalone..." >&2
                    systemctl stop nginx
                    certbot certonly --standalone -d "$domain" \
                        --non-interactive --agree-tos --register-unsafely-without-email 2>&1
                    systemctl start nginx
                }
            else
                # nginx not running or broken — try standalone.
                # Check if port 80 is free.
                if ss -tlnp 2>/dev/null | grep -q ':80 '; then
                    echo "  [!] Порт 80 занят, но nginx не запущен." >&2
                    echo "      Остановите сервис на порту 80 и повторите." >&2
                    return 1
                fi
                certbot certonly --standalone -d "$domain" \
                    --non-interactive --agree-tos --register-unsafely-without-email 2>&1
            fi
        else
            # Standard nginx — use the nginx plugin.
            certbot --nginx -d "$domain" \
                --non-interactive --agree-tos --register-unsafely-without-email 2>&1 || {
                echo "  [!] certbot --nginx не сработал, пробуем standalone..." >&2
                systemctl stop nginx
                certbot certonly --standalone -d "$domain" \
                    --non-interactive --agree-tos --register-unsafely-without-email 2>&1
                systemctl start nginx
            }
        fi
        if [ ! -f "$cert_path" ]; then
            echo "  [!] Не удалось получить сертификат для $domain" >&2
            echo "      Убедитесь, что:" >&2
            echo "        1. A-запись $domain → $(get_public_ip) настроена" >&2
            echo "        2. Порт 80 доступен извне (не заблокирован firewall)" >&2
            echo "        3. DNS изменения успели распространиться (подождите 1-5 минут)" >&2
            return 1
        fi
        echo "         Сертификат получен ✓"
    fi
    fi  # end of caddy_mode skip / else branch

    # ── 6. Configure front-door (nginx or caddy) ──
    if [ "$caddy_mode" -eq 1 ]; then
        echo "  [6/6] Настройка caddy..."
        do_caddy_setup "$domain" "$sub_port" "$caddyfile" || return 1
        echo ""
        echo "[✓] Домен $domain привязан к подпискам через caddy"
        echo "    URL подписок: https://$domain/sub/<UUID>?type=plain"
        echo "    Используется caddy на :443 (3x-ui / xray не затронуты)"
        echo "    Caddy сам выпустит/обновит Let's Encrypt сертификат."
        # Persist domain/port in admin env so admin UI knows about it.
        if [ -f "$ADMIN_ENV" ]; then
            set_env_value OLCRTC_SUB_DOMAIN "$domain" "$ADMIN_ENV"
        fi
        if [ -f "$ENV_FILE" ]; then
            set_env_value OLCRTC_SUB_DOMAIN "$domain" "$ENV_FILE"
        fi
        return 0
    fi
    echo "  [6/6] Настройка nginx..."

    if [ "$sni_mode" -eq 1 ]; then
        # ── SNI mode (3x-ui / xray) ──

        # Backup stream config before ANY modifications.
        local backup_path
        backup_path="${stream_conf}.bak.$(date +%s)"
        cp "$stream_conf" "$backup_path"
        echo "         Бэкап stream config: $backup_path"

        # Pick a free internal port for the HTTPS server block.
        local internal_port=9443
        # If olcrtc_sub upstream already exists, read the port from it.
        if grep -q 'olcrtc_sub' "$stream_conf" 2>/dev/null; then
            local existing_port
            existing_port="$(grep -A1 'upstream olcrtc_sub' "$stream_conf" | grep -oP '127\.0\.0\.1:\K[0-9]+')"
            [ -n "$existing_port" ] && internal_port="$existing_port"
        else
            for p in 9443 9444 9445 9446 9447; do
                if ! timeout 1 bash -c "</dev/tcp/127.0.0.1/${p}" 2>/dev/null; then
                    internal_port=$p; break
                fi
            done
        fi

        # Add upstream to stream config if not already present.
        if ! grep -q 'olcrtc_sub' "$stream_conf" 2>/dev/null; then
            # Insert upstream block before the first existing 'upstream' line.
            local upstream_block="upstream olcrtc_sub { server 127.0.0.1:${internal_port}; }"
            sed -i "0,/^upstream /s||${upstream_block}\n\n&|" "$stream_conf"

            # Insert SNI map entry before the 'default' line.
            # Escape dots in domain for safety.
            local sni_entry="    ${domain}    olcrtc_sub;"
            sed -i "/default/i\\${sni_entry}" "$stream_conf"

            # Validate stream config — rollback on failure.
            if ! nginx -t >/dev/null 2>&1; then
                echo "  [!] nginx -t не прошёл после модификации stream config!" >&2
                echo "      Откатываю изменения из бэкапа: $backup_path" >&2
                cp "$backup_path" "$stream_conf"
                echo "      Stream config восстановлен." >&2
                echo "" >&2
                echo "  Возможные причины:" >&2
                echo "    - Нестандартный формат stream config" >&2
                echo "    - map/upstream в отдельных файлах" >&2
                echo "  Настройте stream config вручную (см. README)." >&2
                return 1
            fi
            echo "         Добавлен upstream olcrtc_sub + SNI-запись в $stream_conf ✓"
        else
            echo "         upstream olcrtc_sub уже существует в $stream_conf"
        fi

        # Build server block depending on proxy_protocol.
        {
            cat <<NGINX_HTTP
server {
    listen 80;
    server_name ${domain};
    return 301 https://\$host\$request_uri;
}

NGINX_HTTP
            if [ "$has_proxy_protocol" -eq 1 ]; then
                cat <<NGINX_PP
server {
    listen 127.0.0.1:${internal_port} ssl http2 proxy_protocol;
    server_name ${domain};
    real_ip_header proxy_protocol;
    set_real_ip_from 127.0.0.1;

    ssl_certificate     /etc/letsencrypt/live/${domain}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${domain}/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:${sub_port};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
    }
}
NGINX_PP
            else
                cat <<NGINX_NOPP
server {
    listen 127.0.0.1:${internal_port} ssl http2;
    server_name ${domain};

    ssl_certificate     /etc/letsencrypt/live/${domain}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${domain}/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:${sub_port};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
    }
}
NGINX_NOPP
            fi
        } > /etc/nginx/sites-available/olcrtc-sub
    else
        # ── Standard nginx (no SNI multiplexer) ──
        cat > /etc/nginx/sites-available/olcrtc-sub <<NGINX_EOF
server {
    listen 80;
    server_name ${domain};
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name ${domain};

    ssl_certificate     /etc/letsencrypt/live/${domain}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/${domain}/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:${sub_port};
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
    }
}
NGINX_EOF
    fi

    ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
    # Remove temporary ACME config if it was created.
    rm -f /etc/nginx/sites-available/olcrtc-acme /etc/nginx/sites-enabled/olcrtc-acme

    if ! nginx -t 2>&1; then
        echo "  [!] nginx -t не прошёл после создания server block." >&2
        # Cleanup: remove our server block.
        rm -f /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/olcrtc-sub
        # Rollback stream config if in SNI mode.
        if [ "$sni_mode" -eq 1 ] && [ -n "${backup_path:-}" ] && [ -f "${backup_path:-}" ]; then
            cp "$backup_path" "$stream_conf"
            echo "      Stream config восстановлен из бэкапа." >&2
        fi
        echo "      Проверьте конфигурацию вручную." >&2
        return 1
    fi
    systemctl reload nginx
    echo "         nginx настроен ✓"

    # ── Save domain to env ──
    set_env_value "OLCRTC_SUB_DOMAIN" "$domain" "$ENV_FILE"

    # ── Summary ──
    echo ""
    echo "  ═══════════════════════════════════════════"
    echo "  Домен настроен!"
    echo "  ═══════════════════════════════════════════"
    echo ""
    echo "  Подписки доступны по адресу:"
    echo "    https://${domain}/sub/{slug}"
    echo ""
    if [ "$sni_mode" -eq 1 ]; then
        echo "  Режим: SNI-мультиплексор (3x-ui / xray)"
        echo "  Внутренний порт: $internal_port"
        [ "$has_proxy_protocol" -eq 1 ] && echo "  proxy_protocol: да"
        echo "  Бэкап stream config: $backup_path"
        echo ""
    fi
    echo "  (опционально) Закрыть прямой доступ к порту $sub_port:"
    echo "    sudo ufw deny ${sub_port}/tcp"
    echo "  или:"
    echo "    sudo iptables -A INPUT -p tcp --dport ${sub_port} -j DROP"
    echo ""
}

# ── Domain removal ───────────────────────────────────────────────────────────
do_remove_domain() {
    echo "[*] Отвязка домена подписок..."

    local current_domain
    current_domain="$(get_env_value OLCRTC_SUB_DOMAIN "$ENV_FILE" 2>/dev/null)"

    # ── 1. Remove nginx server block ──
    if [ -f /etc/nginx/sites-available/olcrtc-sub ]; then
        rm -f /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/olcrtc-sub
        echo "  Удалён server block: /etc/nginx/sites-available/olcrtc-sub ✓"
    else
        echo "  Server block не найден (уже удалён)"
    fi

    # ── 2. Remove upstream/SNI from stream config (if SNI mode) ──
    if grep -rq 'ssl_preread' /etc/nginx/ 2>/dev/null; then
        local stream_conf
        stream_conf="$(grep -rl 'ssl_preread' /etc/nginx/ 2>/dev/null | head -1)"
        if grep -q 'olcrtc_sub' "$stream_conf" 2>/dev/null; then
            # Backup before modification.
            local backup_path
            backup_path="${stream_conf}.bak.$(date +%s)"
            cp "$stream_conf" "$backup_path"
            echo "  Бэкап stream config: $backup_path"

            # Remove upstream block.
            sed -i '/upstream olcrtc_sub {/,/}/d' "$stream_conf"
            # Remove SNI map entry (match by olcrtc_sub).
            sed -i '/olcrtc_sub;/d' "$stream_conf"
            # Remove blank lines left by deletions (collapse double+ blanks).
            sed -i '/^$/N;/^\n$/d' "$stream_conf"

            if ! nginx -t >/dev/null 2>&1; then
                echo "  [!] nginx -t не прошёл после удаления из stream config!" >&2
                echo "      Откатываю из бэкапа: $backup_path" >&2
                cp "$backup_path" "$stream_conf"
                return 1
            fi
            echo "  Удалены upstream olcrtc_sub + SNI-запись из $stream_conf ✓"
        fi
    fi

    # ── 3. Reload nginx ──
    if nginx_present; then
        if nginx -t >/dev/null 2>&1; then
            systemctl reload nginx 2>/dev/null || true
            echo "  nginx перезагружен ✓"
        fi
    fi

    # ── 3b. Remove caddy block (if any) ──
    if caddy_present; then
        do_caddy_remove || true
    fi

    # ── 4. Clear domain from env ──
    set_env_value "OLCRTC_SUB_DOMAIN" "" "$ENV_FILE"

    echo ""
    echo "  ═══════════════════════════════════════════"
    echo "  Домен отвязан."
    echo "  ═══════════════════════════════════════════"
    if [ -n "$current_domain" ]; then
        echo "  Был: $current_domain"
    fi
    echo "  Подписки доступны по: http://$(get_public_ip):$(get_env_value OLCRTC_SUB_PORT "$ADMIN_ENV" 2>/dev/null || echo 2096)/sub/{slug}"
    echo ""
    echo "  Сертификат Let's Encrypt НЕ удалён (можно переиспользовать)."
    echo "  Для удаления: sudo certbot delete --cert-name $current_domain"
    echo ""
}

# ── Uninstall ────────────────────────────────────────────────────────────────
do_uninstall() {
    echo "[*] Stopping services..."
    systemctl stop olcrtc-server.service 2>/dev/null || true
    systemctl stop olcrtc-admin.service 2>/dev/null || true

    echo "[*] Disabling services..."
    systemctl disable olcrtc-server.service 2>/dev/null || true
    systemctl disable olcrtc-admin.service 2>/dev/null || true

    echo "[*] Removing systemd units..."
    rm -f /etc/systemd/system/olcrtc-server.service
    rm -f /etc/systemd/system/olcrtc-server@.service
    rm -f /etc/systemd/system/olcrtc-admin.service
    systemctl daemon-reload

    echo "[*] Removing binaries..."
    rm -f /usr/local/bin/olcrtc
    rm -f /usr/local/bin/olcrtc-admin
    rm -f /usr/local/bin/olcrtc-launcher

    echo "[*] Removing config..."
    rm -rf "$CONFIG_DIR"
    rm -rf "$STATE_DIR"
    rm -rf /var/lib/olcrtc/admin-tls

    echo "[*] olcRTC полностью удалён."
}

# ── Update ───────────────────────────────────────────────────────────────────
do_update() {
    echo "[*] Updating binaries..."
    local arch
    arch="$(detect_arch)"
    if [[ "$arch" == unsupported* ]]; then
        echo "[!] Unsupported architecture: $arch" >&2; exit 1
    fi
    echo "    Detected architecture: $arch"

    local tmpdir
    tmpdir="$(mktemp -d)"
    if download_release "olcrtc-linux-${arch}" "$tmpdir/olcrtc"; then
        install -m 0755 "$tmpdir/olcrtc" /usr/local/bin/olcrtc
        echo "  olcrtc updated"
    else
        echo "[!] Failed to download olcrtc binary" >&2
    fi
    if download_release "olcrtc-admin-linux-${arch}" "$tmpdir/olcrtc-admin"; then
        install -m 0755 "$tmpdir/olcrtc-admin" /usr/local/bin/olcrtc-admin
        echo "  olcrtc-admin updated"
    else
        echo "[!] Failed to download olcrtc-admin binary (may not exist yet)" >&2
    fi
    rm -rf "$tmpdir"

    systemctl daemon-reload
    systemctl restart olcrtc-server.service 2>/dev/null || true
    systemctl restart olcrtc-admin.service 2>/dev/null || true
    echo "[*] Update complete."
}

# ── Argument parsing ─────────────────────────────────────────────────────────
usage() {
    cat <<EOF
Usage: sudo ./olcrtc-setup.sh [options]

Options:
    --carrier <wbstream|telemost|jazz>   Carrier (default: $CARRIER_DEFAULT)
    --transport <datachannel|vp8channel|seichannel>  Transport (default: $TRANSPORT_DEFAULT)
    --name <string>                      Connection name
    --id <room_id>                       Room ID for telemost
    --regenerate                         Regenerate Room ID
    --regenerate-key                     Regenerate key + Room ID
    --update                             Update binaries
    --uninstall                          Full uninstall
    --show-token                         Show admin token
    --status                             Show status
    --setup-domain <domain>              Setup custom domain for subscriptions
    --remove-domain                      Remove custom domain for subscriptions
    -h, --help                           Show this help
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --carrier) CARRIER="$(normalize_carrier "$2")"; shift 2 ;;
        --carrier=*) CARRIER="$(normalize_carrier "${1#*=}")"; shift ;;
        --transport) TRANSPORT="$2"; shift 2 ;;
        --transport=*) TRANSPORT="${1#*=}"; shift ;;
        --name) SET_NAME="$2"; shift 2 ;;
        --name=*) SET_NAME="${1#*=}"; shift ;;
        --id) SET_ID="$2"; shift 2 ;;
        --id=*) SET_ID="${1#*=}"; shift ;;
        --regenerate) DO_REGENERATE=1; shift ;;
        --regenerate-key) DO_REGENERATE_KEY=1; DO_REGENERATE=1; shift ;;
        --update) DO_UPDATE=1; shift ;;
        --uninstall) DO_UNINSTALL=1; shift ;;
        --show-token) DO_SHOW_TOKEN=1; shift ;;
        --status) DO_STATUS=1; shift ;;
        --setup-domain) SETUP_DOMAIN="$2"; DO_SETUP_DOMAIN=1; shift 2 ;;
        --setup-domain=*) SETUP_DOMAIN="${1#*=}"; DO_SETUP_DOMAIN=1; shift ;;
        --remove-domain) DO_REMOVE_DOMAIN=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown option: $1" >&2; usage; exit 1 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "[!] Must run as root (try: sudo $0)" >&2
    exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
    echo "[!] systemd required" >&2
    exit 1
fi

# Handle simple actions first.
if [ "$DO_UNINSTALL" -eq 1 ]; then do_uninstall; exit 0; fi
if [ "$DO_UPDATE" -eq 1 ]; then do_update; exit 0; fi
if [ "$DO_SHOW_TOKEN" -eq 1 ]; then
    if [ -f "$ADMIN_ENV" ]; then
        grep "^OLCRTC_ADMIN_TOKEN=" "$ADMIN_ENV" | cut -d= -f2-
    else
        echo "[!] Admin env not found" >&2; exit 1
    fi
    exit 0
fi
if [ "$DO_STATUS" -eq 1 ]; then
    systemctl status olcrtc-server.service --no-pager 2>/dev/null || true
    systemctl status olcrtc-admin.service --no-pager 2>/dev/null || true
    exit 0
fi
if [ "$DO_SETUP_DOMAIN" -eq 1 ]; then
    if [ -z "$SETUP_DOMAIN" ]; then
        echo "[!] Укажите домен: --setup-domain sub.example.com" >&2; exit 1
    fi
    do_setup_domain "$SETUP_DOMAIN"
    exit 0
fi
if [ "$DO_REMOVE_DOMAIN" -eq 1 ]; then
    do_remove_domain
    exit 0
fi

# ── Re-run on already-installed system ───────────────────────────────────────
if is_installed; then
    echo "  olcRTC уже установлен."
    echo ""
    ADMIN_PORT="$(get_env_value OLCRTC_ADMIN_PORT "$ADMIN_ENV" 2>/dev/null || echo "8443")"
    PUBLIC_IP="$(get_public_ip)"
    echo "  Admin UI:  https://${PUBLIC_IP}:${ADMIN_PORT}"
    systemctl is-active olcrtc-server.service >/dev/null 2>&1 && echo "  olcrtc-server: running" || echo "  olcrtc-server: not running"
    systemctl is-active olcrtc-admin.service >/dev/null 2>&1 && echo "  olcrtc-admin:  running" || echo "  olcrtc-admin:  not running"
    echo ""
    echo "  Дополнительные действия:"
    echo "    --update                   Обновить бинарники"
    echo "    --regenerate               Пересоздать Room ID"
    echo "    --regenerate-key           Пересоздать ключ + Room ID"
    echo "    --setup-domain <domain>    Привязать домен для подписок"
    echo "    --remove-domain            Отвязать домен подписок"
    echo "    --uninstall                Полное удаление"
    echo "    --show-token               Показать токен"
    echo ""
    exit 0
fi

# ══════════════════════════════════════════════════════════════════════════════
#  Fresh installation
# ══════════════════════════════════════════════════════════════════════════════

echo ""
echo "  ╔═══════════════════════════════════════════╗"
echo "  ║  olcRTC Installer v${INSTALLER_VERSION}                  ║"
echo "  ╚═══════════════════════════════════════════╝"
echo ""

ARCH="$(detect_arch)"
if [[ "$ARCH" == unsupported* ]]; then
    echo "[!] Unsupported architecture: ${ARCH#unsupported:}" >&2
    exit 1
fi
echo "    Architecture: $ARCH"

# ── 1. Check system ──────────────────────────────────────────────────────────
echo "  [1/7] Проверка системы..."
for pkg in curl systemctl; do
    if ! command -v "$pkg" >/dev/null 2>&1; then
        echo "  [!] Missing: $pkg" >&2; exit 1
    fi
done
echo "              ✓"

# ── 2. Download olcrtc ───────────────────────────────────────────────────────
echo "  [2/7] Скачивание olcrtc binary..."
TMPDIR="$(mktemp -d)"
if ! download_release "olcrtc-linux-${ARCH}" "$TMPDIR/olcrtc"; then
    echo "  [!] Не удалось скачать olcrtc binary. Проверьте соединение." >&2
    rm -rf "$TMPDIR"
    exit 1
fi
echo "              ✓"

# ── 3. Download olcrtc-admin ─────────────────────────────────────────────────
echo "  [3/7] Скачивание olcrtc-admin..."
if ! download_release "olcrtc-admin-linux-${ARCH}" "$TMPDIR/olcrtc-admin"; then
    echo "  [!] Не удалось скачать olcrtc-admin. Установка olcrtc-admin пропущена." >&2
    touch "$TMPDIR/olcrtc-admin-missing"
fi
echo "              ✓"

# ── 4. Interactive config ────────────────────────────────────────────────────
echo "  [4/7] Настройка:"
echo ""
echo "        Доступные carrier:"
echo "          wbstream  — Wildberries Stream (рекомендуется)"
echo "          jazz      — SaluteJazz"
echo "          telemost  — Yandex Telemost"
if [ -z "$CARRIER" ]; then
    tty_read -rp "        Carrier [wbstream]: " CARRIER
    CARRIER="${CARRIER:-wbstream}"
fi
CARRIER="$(normalize_carrier "$CARRIER")"

echo ""
echo "        Доступные transport:"
echo "          datachannel  — самый быстрый (~6 МБ/с)"
echo "          vp8channel   — универсальный, работает со всеми"
echo "          seichannel   — для wbstream/jazz"
if [ -z "$TRANSPORT" ]; then
    tty_read -rp "        Transport [datachannel]: " TRANSPORT
    TRANSPORT="${TRANSPORT:-datachannel}"
fi

echo ""
echo "        Подписки — публичные ссылки с URIs для клиентов."
echo "        URL: https://IP:PORT/sub/XXXXXX"
SUB_ENABLED=""
while [ "$SUB_ENABLED" != "y" ] && [ "$SUB_ENABLED" != "n" ] && [ "$SUB_ENABLED" != "Y" ] && [ "$SUB_ENABLED" != "N" ]; do
    tty_read -rp "        Подписки [Y/n]: " SUB_ENABLED
    SUB_ENABLED="${SUB_ENABLED:-y}"
done

SETUP_DOMAIN_INSTALL=""
if [ "$SUB_ENABLED" = "y" ] || [ "$SUB_ENABLED" = "Y" ]; then
    echo ""
    echo "        Привязка домена для подписок (HTTPS)."
    echo "        Если есть домен — подписки будут по HTTPS:"
    echo "          https://sub.example.com/sub/XXXXXX"
    echo "        Если нет — подписки доступны по IP:"
    echo "          http://IP:2096/sub/XXXXXX"
    echo ""
    WANT_DOMAIN=""
    tty_read -rp "        Привязать домен? [y/N]: " WANT_DOMAIN
    if [ "$WANT_DOMAIN" = "y" ] || [ "$WANT_DOMAIN" = "Y" ]; then
        tty_read -rp "        Домен (напр. sub.example.com): " SETUP_DOMAIN_INSTALL
        if [ -n "$SETUP_DOMAIN_INSTALL" ] && ! validate_domain "$SETUP_DOMAIN_INSTALL"; then
            echo "        [!] Некорректный домен. Пропускаем привязку." >&2
            SETUP_DOMAIN_INSTALL=""
        fi
    fi
fi

echo ""
if [ -z "$SET_NAME" ]; then
    DEFAULT_NAME="${CARRIER}_olcrtc"
    tty_read -rp "        Имя инстанса [${DEFAULT_NAME}]: " SET_NAME
    SET_NAME="${SET_NAME:-$DEFAULT_NAME}"
fi

# ── 5. Install binaries ──────────────────────────────────────────────────────
echo "  [5/7] Установка бинарников..."
install -m 0755 -o root -g root "$TMPDIR/olcrtc" /usr/local/bin/olcrtc

if [ ! -f "$TMPDIR/olcrtc-admin-missing" ]; then
    install -m 0755 -o root -g root "$TMPDIR/olcrtc-admin" /usr/local/bin/olcrtc-admin
fi

# Install launcher from bundled file or create inline.
SCRIPT_DIR=""
if [ -n "${BASH_SOURCE:-}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
fi
LAUNCHER_SRC=""
[ -n "$SCRIPT_DIR" ] && LAUNCHER_SRC="$SCRIPT_DIR/systemd/olcrtc-launcher"
if [ -n "$LAUNCHER_SRC" ] && [ -f "$LAUNCHER_SRC" ]; then
    install -m 0755 -o root -g root "$LAUNCHER_SRC" /usr/local/bin/olcrtc-launcher
else
    cat > /usr/local/bin/olcrtc-launcher <<'LAUNCHER_EOF'
#!/usr/bin/env bash
set -euo pipefail
carrier="${OLCRTC_CARRIER:-${OLCRTC_PROVIDER:-}}"
[ "$carrier" = "wb_stream" ] && carrier="wbstream"
if [ -z "$carrier" ] || [ -z "${OLCRTC_ROOM_ID:-}" ] || [ -z "${OLCRTC_KEY:-}" ]; then
    echo "missing env" >&2; exit 64
fi
transport="${OLCRTC_TRANSPORT:-datachannel}"
ARGS=(-mode srv -carrier "$carrier" -transport "$transport" -link direct -data data -id "$OLCRTC_ROOM_ID" -key "$OLCRTC_KEY" -dns "${OLCRTC_DNS:-1.1.1.1:53}")
if [ -n "${OLCRTC_DEBUG:-}" ] && [ "$OLCRTC_DEBUG" != "0" ] && [ "$OLCRTC_DEBUG" != "false" ]; then ARGS+=(-debug); fi
if [ "$transport" = "vp8channel" ]; then ARGS+=(-vp8-fps "${OLCRTC_VP8_FPS:-60}" -vp8-batch "${OLCRTC_VP8_BATCH:-8}"); fi
if [ "$transport" = "seichannel" ]; then
    [ -n "${OLCRTC_SEI_FPS:-}" ] && ARGS+=(-sei-fps "$OLCRTC_SEI_FPS")
    [ -n "${OLCRTC_SEI_BATCH:-}" ] && ARGS+=(-sei-batch "$OLCRTC_SEI_BATCH")
    [ -n "${OLCRTC_SEI_FRAG:-}" ] && ARGS+=(-sei-frag "$OLCRTC_SEI_FRAG")
    [ -n "${OLCRTC_SEI_ACK:-}" ] && ARGS+=(-sei-ack-ms "$OLCRTC_SEI_ACK")
fi
if [ -n "${OLCRTC_SOCKS_PROXY:-}" ]; then
    proxy="${OLCRTC_SOCKS_PROXY#socks5://}"
    proxy="${proxy#socks5h://}"
    proxy_user=""; proxy_pass=""
    if [[ "$proxy" == *"@"* ]]; then
        creds="${proxy%@*}"; proxy="${proxy##*@}"
        if [[ "$creds" == *":"* ]]; then proxy_user="${creds%%:*}"; proxy_pass="${creds#*:}"; else proxy_user="$creds"; fi
    fi
    proxy_host="${proxy%:*}"; proxy_port="${proxy##*:}"
    ARGS+=(-socks-proxy "$proxy_host" -socks-proxy-port "$proxy_port")
    [ -n "$proxy_user" ] && ARGS+=(-socks-proxy-user "$proxy_user")
    [ -n "$proxy_pass" ] && ARGS+=(-socks-proxy-pass "$proxy_pass")
fi
if [ -n "${OLCRTC_WARP_PROXY:-}" ]; then
    warp="$OLCRTC_WARP_PROXY"
    warp_host="${warp%:*}"; warp_port="${warp##*:}"
    ARGS+=(-warp-proxy "$warp_host" -warp-proxy-port "$warp_port")
fi
if [ -n "${OLCRTC_SUB_ENABLED:-}" ] && [ "$OLCRTC_SUB_ENABLED" != "0" ] && [ "$OLCRTC_SUB_ENABLED" != "false" ]; then
    ARGS+=(-sub-enabled -sub-db /var/lib/olcrtc/subscriptions.db)
fi
ARGS+=(-sub-port "${OLCRTC_SUB_PORT:-2096}")
exec /usr/local/bin/olcrtc "${ARGS[@]}"
LAUNCHER_EOF
    chmod +x /usr/local/bin/olcrtc-launcher
fi
rm -rf "$TMPDIR"
echo "              ✓"

# ── 6. Generate keys, env, admin.env ─────────────────────────────────────────
echo "  [6/7] Генерация ключей и конфигурации..."

# Create user.
if ! id olcrtc >/dev/null 2>&1; then
    useradd --system --no-create-home --shell /usr/sbin/nologin --home-dir "$STATE_DIR" olcrtc
fi

install -d -m 0750 -o root -g olcrtc "$CONFIG_DIR"
install -d -m 0750 -o olcrtc -g olcrtc "$STATE_DIR"
install -d -m 0750 -o root -g root /var/lib/olcrtc/admin-tls

# Encryption key.
if [ "$DO_REGENERATE_KEY" -eq 1 ] || [ ! -s "$KEY_FILE" ]; then
    openssl rand -hex 32 > "$KEY_FILE"
    chown root:olcrtc "$KEY_FILE"
    chmod 0640 "$KEY_FILE"
fi
KEY="$(cat "$KEY_FILE")"

# Room ID.
ROOM_ID="any"
if [ "$CARRIER" = "telemost" ]; then
    ROOM_ID="${SET_ID:-olcrtc-$(openssl rand -hex 4)}"
elif [ "$DO_REGENERATE" -eq 0 ] && [ -f "$ENV_FILE" ]; then
    EXISTING_ROOM="$(get_env_value OLCRTC_ROOM_ID)"
    [ -n "$EXISTING_ROOM" ] && ROOM_ID="$EXISTING_ROOM"
fi

# Main env file.
SUB_ENABLED_VAL=""
if [ "$SUB_ENABLED" = "y" ] || [ "$SUB_ENABLED" = "Y" ]; then SUB_ENABLED_VAL="1"; fi
cat > "$ENV_FILE" <<EOF
OLCRTC_CARRIER=$CARRIER
OLCRTC_TRANSPORT=$TRANSPORT
OLCRTC_ROOM_ID=$ROOM_ID
OLCRTC_KEY=$KEY
OLCRTC_DNS=$DNS_DEFAULT
OLCRTC_NAME=$SET_NAME
OLCRTC_SUB_ENABLED=$SUB_ENABLED_VAL
EOF
chown root:olcrtc "$ENV_FILE"
chmod 0640 "$ENV_FILE"

# Admin env.
ADMIN_PORT=8443
# Check if 8443 is free, else auto-pick.
if ! timeout 1 bash -c "</dev/tcp/127.0.0.1/${ADMIN_PORT}" 2>/dev/null; then
    : # free
else
    for p in 9443 8080 3000 4443; do
        if ! timeout 1 bash -c "</dev/tcp/127.0.0.1/${p}" 2>/dev/null; then
            ADMIN_PORT=$p; break
        fi
    done
fi
ADMIN_TOKEN="$(openssl rand -hex 32)"
OLCRTC_ADMIN_DOMAIN=""
OLCRTC_SUB_PORT=2096
cat > "$ADMIN_ENV" <<EOF
OLCRTC_ADMIN_PORT=${ADMIN_PORT}
OLCRTC_ADMIN_TOKEN=${ADMIN_TOKEN}
OLCRTC_ADMIN_DOMAIN=
OLCRTC_SUB_PORT=2096
EOF
chmod 0600 "$ADMIN_ENV"

# ── 7. Systemd units ─────────────────────────────────────────────────────────
echo "  [7/7] Создание systemd-юнитов..."

cat > /etc/systemd/system/olcrtc-server.service <<'UNIT'
[Unit]
Description=olcRTC server
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
UNIT

cat > /etc/systemd/system/olcrtc-server@.service <<'UNIT'
[Unit]
Description=olcRTC server instance %i
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
UNIT

cat > /etc/systemd/system/olcrtc-admin.service <<UNIT
[Unit]
Description=olcRTC Admin Web UI
After=network-online.target olcrtc-server.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/olcrtc/admin.env
ExecStart=/usr/local/bin/olcrtc-admin \\
    -port \${OLCRTC_ADMIN_PORT} \\
    -token \${OLCRTC_ADMIN_TOKEN} \\
    -domain \"${OLCRTC_ADMIN_DOMAIN}\" \\
    -sub-port \${OLCRTC_SUB_PORT} \\
    -tls-dir /var/lib/olcrtc/admin-tls
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --quiet olcrtc-server.service
systemctl enable --quiet olcrtc-admin.service 2>/dev/null || true
systemctl restart olcrtc-server.service

# Wait for room ID if auto.
if [ "$ROOM_ID" = "any" ]; then
    echo "  [*] Waiting for carrier to create room..."
    DETECTED=""
    for i in $(seq 1 30); do
        sleep 1
        DETECTED="$(journalctl -u olcrtc-server.service --since '1 minute ago' --no-pager 2>/dev/null | grep -oE '(WB Stream room created|Jazz room created): \S+' | tail -1 | awk '{print $NF}')" || true
        [ -n "$DETECTED" ] && break
    done
    if [ -n "$DETECTED" ]; then
        set_env_value "OLCRTC_ROOM_ID" "$DETECTED" "$ENV_FILE"
        systemctl restart olcrtc-server.service
        ROOM_ID="$DETECTED"
        sleep 2
    fi
fi

systemctl start olcrtc-admin.service 2>/dev/null || true

PUBLIC_IP="$(get_public_ip)"

# ── Done ─────────────────────────────────────────────────────────────────────
echo ""
echo "  ═══════════════════════════════════════════"
echo "  Установка завершена!"
echo "  ═══════════════════════════════════════════"
echo ""
echo "  Admin UI:  https://${PUBLIC_IP}:${ADMIN_PORT}"
echo "  Токен:     ${ADMIN_TOKEN}"
echo ""
echo "  ⚠  Сертификат самоподписанный."
echo "     В браузере нажмите 'Дополнительно' → 'Перейти'."
echo ""
echo "  Дальнейшее управление — через Web UI."
echo "  ═══════════════════════════════════════════"
echo ""

# ── Domain setup (if requested during install) ───────────────────────────────
if [ -n "$SETUP_DOMAIN_INSTALL" ]; then
    echo ""
    do_setup_domain "$SETUP_DOMAIN_INSTALL"
fi
