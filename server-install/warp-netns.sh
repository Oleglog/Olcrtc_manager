#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────────────────────
# warp-netns.sh — Управление WARP network namespace для olcrtc.
#
# Создаёт изолированный сетевой namespace с WireGuard-туннелем через
# Cloudflare WARP.  Только olcrtc запускается внутри — весь его трафик
# (TCP + UDP/ICE) идёт через WARP.  Остальные сервисы VPS не затрагиваются.
#
# Установка одной командой:
#   curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/warp-netns.sh | sudo bash -s install
#
# Использование:
#   warp-netns  up                  — поднять namespace + WireGuard
#   warp-netns  down                — удалить namespace
#   warp-netns  status              — показать состояние
#   warp-netns  test                — проверить IP изнутри namespace
#   warp-netns  install             — установить скрипт + systemd + подключить olcrtc
#   warp-netns  uninstall           — отключить WARP от olcrtc и удалить всё
#
# Конфиг:  /etc/olcrtc/warp-wg.conf  (стандартный WireGuard INI-формат)
# ──────────────────────────────────────────────────────────────────────────────

set -euo pipefail

CONF="${WG_CONFIG_FILE:-/etc/olcrtc/warp-wg.conf}"
NS="warp_olcrtc"
WG="wg-warp0"
VH="veth-olcrtc0"
VN="veth-warp0"
HOST_IP="10.200.1.1"
NS_IP="10.200.1.2"

RAW_BASE="https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install"

# ── Helpers ───────────────────────────────────────────────────────────────────

tty_read() {
    if [ -t 0 ]; then read "$@"; else read "$@" < /dev/tty; fi
}

die() { echo "[!] $*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "Запустите от root:  sudo $0 $*"
}

require_cmds() {
    local missing=0
    for c in ip wg iptables curl; do
        if ! command -v "$c" >/dev/null 2>&1; then
            echo "[!] Не найдена команда: $c" >&2
            missing=1
        fi
    done
    [ "$missing" -eq 0 ] || die "Установите:  apt install wireguard-tools iptables curl"
}

main_iface() {
    ip route show default | awk '{for(i=1;i<=NF;i++) if($i=="dev") print $(i+1)}' | head -1
}

# ── Parse WireGuard config ────────────────────────────────────────────────────

PK="" ADDR="" MTU="1420" PUB="" EP=""

parse_conf() {
    [ -f "$CONF" ] || die "Конфиг не найден: $CONF\nСоздайте его — см. warp-netns install или WARP.md"
    local s=""
    while IFS= read -r l; do
        l="${l%%#*}"; l="$(echo "$l" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
        [ -z "$l" ] && continue
        case "$l" in
            \[Interface\]*) s=i; continue ;; \[Peer\]*) s=p; continue ;; esac
        local k v
        k="$(echo "$l" | cut -d= -f1 | sed 's/[[:space:]]*$//')"
        v="$(echo "$l" | cut -d= -f2- | sed 's/^[[:space:]]*//')"
        case "$s" in
            i) case "$k" in PrivateKey) PK="$v";; Address) ADDR="$v";; MTU) MTU="$v";; esac ;;
            p) case "$k" in PublicKey) PUB="$v";; Endpoint) EP="$v";; esac ;;
        esac
    done < "$CONF"
    [ -n "$PK"  ] || die "PrivateKey не найден в $CONF"
    [ -n "$PUB" ] || die "PublicKey не найден в $CONF"
    [ -n "$EP"  ] || die "Endpoint не найден в $CONF"
}

# ══════════════════════════════════════════════════════════════════════════════
#  UP
# ══════════════════════════════════════════════════════════════════════════════

do_up() {
    require_cmds
    parse_conf

    if ip netns list 2>/dev/null | grep -q "^${NS} \|^${NS}$"; then
        echo "[*] Namespace '$NS' уже существует. Сначала: $0 down"
        return 0
    fi

    local mi
    mi="$(main_iface)"
    [ -n "$mi" ] || die "Не удалось определить основной сетевой интерфейс"

    # IPv4 из Address (может быть через запятую с IPv6)
    local ipv4
    ipv4="$(echo "$ADDR" | tr ',' '\n' | grep -v ':' | sed 's/^[[:space:]]*//' | head -1)"
    [ -z "$ipv4" ] && ipv4="172.16.0.2/32"

    local ephost="${EP%:*}"

    echo "[1/7] Namespace..."
    ip netns add "$NS"

    echo "[2/7] Veth-мост ($VH <-> $VN)..."
    ip link add "$VH" type veth peer name "$VN"
    ip link set "$VN" netns "$NS"
    ip addr add "$HOST_IP/30" dev "$VH"
    ip link set "$VH" up
    ip netns exec "$NS" ip link set lo up
    ip netns exec "$NS" ip addr add "$NS_IP/30" dev "$VN"
    ip netns exec "$NS" ip link set "$VN" up

    echo "[3/7] WireGuard..."
    ip link add "$WG" type wireguard
    ip link set "$WG" netns "$NS"
    local kf; kf="$(mktemp)"; echo "$PK" > "$kf"; chmod 600 "$kf"
    ip netns exec "$NS" wg set "$WG" \
        private-key "$kf" \
        peer "$PUB" \
            endpoint "$EP" \
            allowed-ips "0.0.0.0/0,::/0" \
            persistent-keepalive 25
    rm -f "$kf"
    ip netns exec "$NS" ip addr add "$ipv4" dev "$WG"
    ip netns exec "$NS" ip link set "$WG" mtu "$MTU"
    ip netns exec "$NS" ip link set "$WG" up

    echo "[4/7] Маршруты внутри namespace..."
    ip netns exec "$NS" ip route add "$ephost/32" via "$HOST_IP" dev "$VN"
    ip netns exec "$NS" ip route add default dev "$WG"

    echo "[5/7] NAT и форвардинг..."
    sysctl -qw net.ipv4.ip_forward=1
    iptables -t nat -C POSTROUTING -s 10.200.1.0/30 -o "$mi" -j MASQUERADE 2>/dev/null \
        || iptables -t nat -A POSTROUTING -s 10.200.1.0/30 -o "$mi" -j MASQUERADE
    iptables -C FORWARD -i "$VH" -o "$mi" -j ACCEPT 2>/dev/null \
        || iptables -A FORWARD -i "$VH" -o "$mi" -j ACCEPT
    iptables -C FORWARD -i "$mi" -o "$VH" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null \
        || iptables -A FORWARD -i "$mi" -o "$VH" -m state --state RELATED,ESTABLISHED -j ACCEPT

    echo "[6/7] DNS..."
    mkdir -p "/etc/netns/$NS"
    echo "nameserver 1.1.1.1" > "/etc/netns/$NS/resolv.conf"

    echo "[7/7] Проверяю туннель..."
    local warp_ip
    warp_ip="$(ip netns exec "$NS" curl -s --max-time 10 https://ifconfig.me 2>/dev/null || true)"

    echo ""
    echo "══════════════════════════════════════════════════════"
    echo "  WARP namespace '$NS' — UP"
    if [ -n "$warp_ip" ]; then
        echo "  WARP IP: $warp_ip"
    else
        echo "  WARP IP: (проверка не удалась, туннелю нужно время)"
    fi
    echo "══════════════════════════════════════════════════════"
}

# ══════════════════════════════════════════════════════════════════════════════
#  DOWN
# ══════════════════════════════════════════════════════════════════════════════

do_down() {
    local mi; mi="$(main_iface)"
    echo "[*] Удаляю namespace '$NS'..."
    if [ -n "$mi" ]; then
        iptables -t nat -D POSTROUTING -s 10.200.1.0/30 -o "$mi" -j MASQUERADE 2>/dev/null || true
        iptables -D FORWARD -i "$VH" -o "$mi" -j ACCEPT 2>/dev/null || true
        iptables -D FORWARD -i "$mi" -o "$VH" -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true
    fi
    ip netns del "$NS" 2>/dev/null || true
    ip link del "$VH" 2>/dev/null || true
    rm -rf "/etc/netns/$NS"
    echo "[+] DOWN"
}

# ══════════════════════════════════════════════════════════════════════════════
#  STATUS
# ══════════════════════════════════════════════════════════════════════════════

do_status() {
    if ! ip netns list 2>/dev/null | grep -q "^${NS}"; then
        echo "[-] Namespace '$NS' не существует."
        return
    fi
    echo "[+] Namespace '$NS' активен."
    echo ""
    echo "  Интерфейсы:"
    ip netns exec "$NS" ip -br addr show 2>/dev/null | sed 's/^/    /'
    echo ""
    echo "  Маршруты:"
    ip netns exec "$NS" ip route show 2>/dev/null | sed 's/^/    /'
    echo ""
    echo "  WireGuard:"
    ip netns exec "$NS" wg show 2>/dev/null | sed 's/^/    /' || echo "    (нет)"
}

# ══════════════════════════════════════════════════════════════════════════════
#  TEST
# ══════════════════════════════════════════════════════════════════════════════

do_test() {
    if ! ip netns list 2>/dev/null | grep -q "^${NS}"; then
        die "Namespace '$NS' не существует. Сначала: $0 up"
    fi
    echo "[*] Тест из namespace '$NS':"
    echo ""
    local warp_ip vps_ip
    warp_ip="$(ip netns exec "$NS" curl -s --max-time 10 https://ifconfig.me 2>/dev/null || echo "ОШИБКА")"
    vps_ip="$(curl -s --max-time 5 https://ifconfig.me 2>/dev/null || echo "?")"
    echo "  WARP IP (виден клиенту):  $warp_ip"
    echo "  VPS IP  (настоящий):      $vps_ip"
    echo ""
    if [ "$warp_ip" != "$vps_ip" ] && [ "$warp_ip" != "ОШИБКА" ]; then
        echo "  ✓ IP отличаются — VPS IP скрыт."
    else
        echo "  ✗ Что-то не так. Проверьте: $0 status"
    fi
}

# ══════════════════════════════════════════════════════════════════════════════
#  INSTALL  — полная установка: скрипт + конфиг + systemd + подключение olcrtc
# ══════════════════════════════════════════════════════════════════════════════

do_install() {
    echo ""
    echo "════════════════════════════════════════════════════════════"
    echo "  WARP для olcrtc — установка"
    echo "════════════════════════════════════════════════════════════"
    echo ""

    # 0. Проверки
    if [ ! -f /usr/local/bin/olcrtc ]; then
        die "olcrtc не установлен. Сначала:\n  curl -fsSL $RAW_BASE/olcrtc-setup.sh | sudo bash"
    fi

    for cmd in ip iptables curl; do
        command -v "$cmd" >/dev/null 2>&1 || die "Не найдено: $cmd"
    done

    # wireguard-tools
    if ! command -v wg >/dev/null 2>&1; then
        echo "[*] Устанавливаю wireguard-tools..."
        if command -v apt-get >/dev/null 2>&1; then
            apt-get update -qq && apt-get install -y -qq wireguard-tools >/dev/null 2>&1
        elif command -v dnf >/dev/null 2>&1; then
            dnf install -y -q wireguard-tools >/dev/null 2>&1
        elif command -v yum >/dev/null 2>&1; then
            yum install -y -q wireguard-tools >/dev/null 2>&1
        fi
        command -v wg >/dev/null 2>&1 || die "Не удалось установить wireguard-tools. Установите вручную."
    fi

    # 1. Конфиг WireGuard
    echo ""
    echo "  Откуда взять ключи WARP?"
    echo ""
    echo "  1) Ввести вручную (из 3x-ui → Исходящие подключения → warp)"
    echo "  2) Сгенерировать новые через wgcf"
    echo ""
    tty_read -rp "  Выберите [1/2]: " key_choice

    case "$key_choice" in
    2)
        echo "[*] Устанавливаю wgcf..."
        local arch; arch="$(uname -m)"
        local wgcf_arch="amd64"
        { [ "$arch" = "aarch64" ] || [ "$arch" = "arm64" ]; } && wgcf_arch="arm64"
        # Получаем последнюю версию через GitHub API
        local wgcf_ver
        wgcf_ver="$(curl -fsSL https://api.github.com/repos/ViRb3/wgcf/releases/latest | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4)"
        wgcf_ver="${wgcf_ver#v}"  # убираем "v" из "v2.2.30"
        if [ -z "$wgcf_ver" ]; then
            die "Не удалось определить версию wgcf. Проверьте доступ к api.github.com"
        fi
        local wgcf_url="https://github.com/ViRb3/wgcf/releases/download/v${wgcf_ver}/wgcf_${wgcf_ver}_linux_${wgcf_arch}"
        echo "    Версия: $wgcf_ver  Архитектура: $wgcf_arch"
        curl -fsSL "$wgcf_url" -o /tmp/wgcf || die "Не удалось скачать wgcf: $wgcf_url"
        chmod +x /tmp/wgcf
        echo "[*] Регистрирую WARP-аккаунт..."
        (cd /tmp && ./wgcf register --accept-tos && ./wgcf generate) || die "wgcf не удался"
        mkdir -p /etc/olcrtc
        cp /tmp/wgcf-profile.conf "$CONF"
        chmod 600 "$CONF"
        echo "[+] Конфиг сохранён: $CONF"
        rm -f /tmp/wgcf /tmp/wgcf-account.toml /tmp/wgcf-profile.conf
        ;;
    *)
        echo ""
        echo "  Возьмите данные из 3x-ui → Исходящие подключения → warp (JSON)."
        echo "  Или из любого другого WireGuard WARP-конфига."
        echo ""
        tty_read -rp "  PrivateKey (secretKey из 3x-ui): " input_pk
        [ -z "$input_pk" ] && die "PrivateKey не может быть пустым"

        local input_pub input_ep input_addr
        tty_read -rp "  PublicKey пира [Enter = bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=]: " input_pub
        [ -z "$input_pub" ] && input_pub="bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo="

        tty_read -rp "  Endpoint [Enter = 162.159.192.1:2408]: " input_ep
        [ -z "$input_ep" ] && input_ep="162.159.192.1:2408"

        tty_read -rp "  Address (IP в туннеле) [Enter = 172.16.0.2/32]: " input_addr
        [ -z "$input_addr" ] && input_addr="172.16.0.2/32"

        mkdir -p /etc/olcrtc
        cat > "$CONF" <<WGEOF
[Interface]
PrivateKey = $input_pk
Address = $input_addr
MTU = 1420

[Peer]
PublicKey = $input_pub
Endpoint = $input_ep
AllowedIPs = 0.0.0.0/0, ::/0
WGEOF
        chmod 600 "$CONF"
        echo "[+] Конфиг сохранён: $CONF"
        ;;
    esac

    # 2. Устанавливаю скрипт
    echo "[*] Устанавливаю /usr/local/bin/warp-netns..."
    local self_path
    self_path="$(realpath "${BASH_SOURCE[0]}" 2>/dev/null || echo "")"
    if [ -n "$self_path" ] && [ -f "$self_path" ]; then
        install -m 0755 "$self_path" /usr/local/bin/warp-netns
    else
        # Запущен через curl | bash — скачиваем
        curl -fsSL "$RAW_BASE/warp-netns.sh" -o /usr/local/bin/warp-netns
        chmod 0755 /usr/local/bin/warp-netns
    fi

    # 3. systemd unit для namespace
    echo "[*] Создаю warp-netns.service..."
    cat > /etc/systemd/system/warp-netns.service <<'UNIT'
[Unit]
Description=WARP WireGuard namespace for olcrtc
Before=olcrtc-server.service
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/warp-netns up
ExecStop=/usr/local/bin/warp-netns down

[Install]
WantedBy=multi-user.target
UNIT

    # 4. Drop-in для olcrtc-server — запуск внутри namespace
    echo "[*] Подключаю olcrtc к WARP namespace..."
    mkdir -p /etc/systemd/system/olcrtc-server.service.d
    cat > /etc/systemd/system/olcrtc-server.service.d/warp.conf <<DROPIN
[Unit]
After=warp-netns.service
Requires=warp-netns.service

[Service]
NetworkNamespacePath=/run/netns/$NS
DROPIN

    # Drop-in для шаблонных инстансов (если есть)
    if [ -f /etc/systemd/system/olcrtc-server@.service ]; then
        mkdir -p /etc/systemd/system/olcrtc-server@.service.d
        cat > /etc/systemd/system/olcrtc-server@.service.d/warp.conf <<DROPIN2
[Unit]
After=warp-netns.service
Requires=warp-netns.service

[Service]
NetworkNamespacePath=/run/netns/$NS
DROPIN2
    fi

    systemctl daemon-reload

    # 5. Проверяем SOCKS-прокси
    if [ -f /etc/olcrtc/env ]; then
        local cur_proxy
        cur_proxy="$(grep -E '^OLCRTC_SOCKS_PROXY=' /etc/olcrtc/env 2>/dev/null | tail -1 | cut -d= -f2-)"
        if [ -n "$cur_proxy" ] && [[ "$cur_proxy" == *"127.0.0.1"* || "$cur_proxy" == *"localhost"* ]]; then
            echo ""
            echo "  ╔══════════════════════════════════════════════════════╗"
            echo "  ║  ВНИМАНИЕ: ваш SOCKS-прокси на localhost!           ║"
            echo "  ║  Изнутри namespace 127.0.0.1 недоступен.            ║"
            echo "  ║                                                      ║"
            echo "  ║  Нужно заменить адрес прокси на: $HOST_IP            ║"
            echo "  ║  И убедиться что прокси слушает на 0.0.0.0           ║"
            echo "  ╚══════════════════════════════════════════════════════╝"
            echo ""
            local new_proxy
            new_proxy="$(echo "$cur_proxy" | sed "s/127\.0\.0\.1/$HOST_IP/g; s/localhost/$HOST_IP/g")"
            tty_read -rp "  Заменить '$cur_proxy' → '$new_proxy'? [Y/n] " fix_proxy
            if [ "$fix_proxy" != "n" ] && [ "$fix_proxy" != "N" ]; then
                # Update using the same approach as olcrtc-setup.sh set_env_value
                local tmpf; tmpf="$(mktemp)"
                while IFS= read -r line || [ -n "$line" ]; do
                    case "$line" in
                        OLCRTC_SOCKS_PROXY=*) echo "OLCRTC_SOCKS_PROXY=$new_proxy" ;;
                        *) echo "$line" ;;
                    esac
                done < /etc/olcrtc/env > "$tmpf"
                cat "$tmpf" > /etc/olcrtc/env
                rm -f "$tmpf"
                echo "  [+] Прокси обновлён: $new_proxy"
            fi
        fi
    fi

    # 6. Запуск
    echo ""
    echo "[*] Запускаю WARP namespace..."
    systemctl enable --quiet warp-netns.service
    systemctl start warp-netns.service

    echo "[*] Перезапускаю olcrtc-server..."
    systemctl restart olcrtc-server.service 2>/dev/null || true

    # 7. Тест
    sleep 2
    local warp_ip vps_ip
    warp_ip="$(ip netns exec "$NS" curl -s --max-time 10 https://ifconfig.me 2>/dev/null || echo "?")"
    vps_ip="$(curl -s --max-time 5 https://ifconfig.me 2>/dev/null || echo "?")"

    echo ""
    echo "════════════════════════════════════════════════════════════"
    echo "  WARP для olcrtc — установлено!"
    echo "════════════════════════════════════════════════════════════"
    echo ""
    echo "  WARP IP (виден клиенту):  $warp_ip"
    echo "  VPS IP  (настоящий):      $vps_ip"
    echo ""
    echo "  Управление:"
    echo "    warp-netns status     — состояние"
    echo "    warp-netns test       — проверить IP"
    echo "    warp-netns down       — выключить (временно)"
    echo "    warp-netns up         — включить обратно"
    echo "    warp-netns uninstall  — полностью удалить WARP"
    echo ""
    echo "  Автозапуск при перезагрузке: включён (warp-netns.service)"
    echo "════════════════════════════════════════════════════════════"
}

# ══════════════════════════════════════════════════════════════════════════════
#  UNINSTALL
# ══════════════════════════════════════════════════════════════════════════════

do_uninstall() {
    echo "[*] Удаляю WARP namespace для olcrtc..."

    # Остановить
    systemctl disable --now warp-netns.service 2>/dev/null || true
    systemctl reset-failed warp-netns.service 2>/dev/null || true

    # Удалить drop-in
    rm -f /etc/systemd/system/olcrtc-server.service.d/warp.conf
    rmdir /etc/systemd/system/olcrtc-server.service.d 2>/dev/null || true
    rm -f /etc/systemd/system/olcrtc-server@.service.d/warp.conf
    rmdir /etc/systemd/system/olcrtc-server@.service.d 2>/dev/null || true

    # Удалить unit
    rm -f /etc/systemd/system/warp-netns.service
    systemctl daemon-reload

    # Убрать namespace (если ещё жив)
    do_down 2>/dev/null || true

    # Удалить скрипт и конфиг
    rm -f /usr/local/bin/warp-netns
    # Конфиг оставляем — пользователь может захотеть переиспользовать ключи
    # rm -f /etc/olcrtc/warp-wg.conf

    echo ""
    echo "[*] Перезапускаю olcrtc-server (уже без WARP)..."
    systemctl restart olcrtc-server.service 2>/dev/null || true

    echo ""
    echo "[+] WARP удалён. olcrtc работает напрямую (как раньше)."
    echo "    Конфиг $CONF оставлен (удалите вручную если не нужен)."

    # Проверить прокси — может нужно вернуть localhost
    if [ -f /etc/olcrtc/env ]; then
        local cur_proxy
        cur_proxy="$(grep -E '^OLCRTC_SOCKS_PROXY=' /etc/olcrtc/env 2>/dev/null | tail -1 | cut -d= -f2-)"
        if [ -n "$cur_proxy" ] && [[ "$cur_proxy" == *"$HOST_IP"* ]]; then
            echo ""
            echo "  [!] Ваш прокси настроен на $HOST_IP (адрес veth-моста)."
            echo "      Возможно нужно вернуть 127.0.0.1:"
            echo "      sudo sed -i 's/$HOST_IP/127.0.0.1/g' /etc/olcrtc/env"
            echo "      sudo systemctl restart olcrtc-server"
        fi
    fi
}

# ══════════════════════════════════════════════════════════════════════════════
#  MAIN
# ══════════════════════════════════════════════════════════════════════════════

require_root "${1:-}"

case "${1:-}" in
    up)        do_up ;;
    down)      do_down ;;
    status)    do_status ;;
    test)      do_test ;;
    install)   do_install ;;
    uninstall) do_uninstall ;;
    *)
        cat <<EOF
Использование:  $(basename "$0") <команда>

Команды:
    install     Полная установка (конфиг + namespace + systemd)
    uninstall   Удалить WARP (olcrtc вернётся к работе напрямую)
    up          Поднять namespace
    down        Удалить namespace
    status      Показать состояние
    test        Проверить IP из namespace

Установка одной командой:
    curl -fsSL $RAW_BASE/warp-netns.sh | sudo bash -s install
EOF
        ;;
esac
