# ТЗ: Скрытие IP VPS через WARP (wireproxy)

## Проблема

Клиент, подключённый через olcrtc, видит реальный IP VPS при посещении
сайтов (например 2ip.io). Причина: функция `Server.dial()` в
`internal/server/server.go` открывает TCP-соединения напрямую от VPS.

Существующий SOCKS5-прокси (`-socks-proxy`) используется **только** для
signaling-трафика (создание комнаты, получение токена) и должен сохранять
RU-IP. Клиентский трафик через него идти не должен.

## Решение

Добавить **второй** SOCKS5-прокси (`-warp-proxy`), через который пойдёт
**только** клиентский tunnel-трафик (`Server.dial()`). В качестве этого
прокси используется [wireproxy](https://github.com/pufferffish/wireproxy) —
userspace WireGuard, поднимающий локальный SOCKS5 из `.conf`-файла WARP.

### Архитектура

```
┌──────────────────────────────────────────────────────────────┐
│  VPS                                                         │
│                                                              │
│  ┌─────────────┐    signaling     ┌──────────────────────┐   │
│  │ olcrtc-srv  │ ────────────────→│ SOCKS5 RU-прокси     │   │
│  │             │    (wb_stream    │ 127.0.0.1:11080      │   │
│  │             │     API/WS)      └──────────────────────┘   │
│  │             │                                             │
│  │             │    client TCP    ┌──────────────────────┐   │
│  │             │ ────────────────→│ wireproxy (WARP)     │   │
│  │             │    (dial())      │ 127.0.0.1:40000      │   │
│  └─────────────┘                  └───────┬──────────────┘   │
│                                           │ WireGuard        │
│                                           ▼                  │
│                                   ┌──────────────────────┐   │
│                                   │ Cloudflare WARP      │   │
│                                   │ 162.159.192.1:2408   │   │
│                                   └──────────────────────┘   │
└──────────────────────────────────────────────────────────────┘
```

**Клиент видит:** Cloudflare WARP IP (напр. 104.28.x.x)
**wb_stream видит:** RU residential IP (через SOCKS5 RU-прокси)
**Остальной VPS трафик:** не затронут (без namespace, без глобальных маршрутов)

## Изменения в коде

### 1. `cmd/olcrtc/main.go` — новый флаг

Добавить в `config`:

```go
warpProxyAddr string
warpProxyPort int
```

Добавить в `parseFlags()`:

```go
flag.StringVar(&cfg.warpProxyAddr, "warp-proxy", "", "WARP SOCKS5 proxy for client tunnel traffic (server only)")
flag.IntVar(&cfg.warpProxyPort, "warp-proxy-port", 40000, "WARP SOCKS5 proxy port (server only)")
```

Передать в `server.Run()` (добавить два аргумента):

```go
server.Run(ctx, cfg.provider, cfg.roomID, cfg.keyHex,
    cfg.dnsServer,
    cfg.socksProxyAddr, cfg.socksProxyPort,
    cfg.socksProxyUser, cfg.socksProxyPass,
    cfg.warpProxyAddr, cfg.warpProxyPort,   // <-- новое
)
```

### 2. `internal/server/server.go` — маршрутизация dial() через WARP

#### 2a. Добавить поля в `Server`:

```go
warpProxyAddr string
warpProxyPort int
```

#### 2b. Обновить `Run()` — принять и сохранить новые параметры:

```go
func Run(
    ctx context.Context,
    providerName, roomURL, keyHex string,
    dnsServer, socksProxyAddr string,
    socksProxyPort int,
    socksProxyUser, socksProxyPass string,
    warpProxyAddr string,    // <-- новое
    warpProxyPort int,       // <-- новое
) error {
```

Сохранить в `Server`:

```go
s := &Server{
    // ... существующие поля ...
    warpProxyAddr: warpProxyAddr,
    warpProxyPort: warpProxyPort,
}
```

#### 2c. Изменить `dial()` — если WARP-прокси настроен, идти через него:

```go
func (s *Server) dial(req ConnectRequest) (net.Conn, error) {
    addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))

    // Если WARP-прокси настроен — весь клиентский трафик через него
    if s.warpProxyAddr != "" {
        proxyAddr := net.JoinHostPort(s.warpProxyAddr, strconv.Itoa(s.warpProxyPort))
        dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{
            Timeout:  10 * time.Second,
            Resolver: s.resolver,
        })
        if err != nil {
            return nil, fmt.Errorf("warp proxy setup failed: %w", err)
        }
        conn, err := dialer.Dial("tcp4", addr)
        if err != nil {
            return nil, fmt.Errorf("dial via warp failed: %w", err)
        }
        return conn, nil
    }

    // Без WARP — прямое соединение (как сейчас)
    dialer := &net.Dialer{
        Timeout:   10 * time.Second,
        KeepAlive: 30 * time.Second,
        Resolver:  s.resolver,
    }
    conn, err := dialer.Dial("tcp4", addr)
    if err != nil {
        return nil, fmt.Errorf("dial failed: %w", err)
    }
    return conn, nil
}
```

Импорт: `"golang.org/x/net/proxy"` (уже используется в проекте через
`internal/protect`).

### 3. `server-install/systemd/olcrtc-launcher` — env → флаг

Добавить после блока `OLCRTC_SOCKS_PROXY`:

```bash
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
```

### 4. Env-файл `/etc/olcrtc/env`

Новая переменная (опциональная):

```
OLCRTC_WARP_PROXY=127.0.0.1:40000
```

### 5. `server-install/olcrtc-setup.sh` — меню

Добавить пункт в интерактивное меню:

- **Настроить WARP-прокси** — задать/изменить/убрать `OLCRTC_WARP_PROXY`
  в env-файле. Логика аналогична существующему пункту для `OLCRTC_SOCKS_PROXY`.

## Установка wireproxy на VPS

Не требует изменений в коде olcrtc. Выполняется один раз вручную:

```bash
# 1. Скачать wireproxy
WPVER=1.0.9
curl -fsSL "https://github.com/pufferffish/wireproxy/releases/download/v${WPVER}/wireproxy_linux_amd64.tar.gz" \
  | tar xz -C /usr/local/bin/ wireproxy
chmod +x /usr/local/bin/wireproxy

# 2. Создать конфиг из уже сгенерированного WARP-профиля
cat > /etc/olcrtc/wireproxy.conf << 'EOF'
[Interface]
PrivateKey = <PrivateKey из warp-wg.conf>
DNS = 1.1.1.1
Address = 172.16.0.2/32
MTU = 1280

[Peer]
PublicKey = bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=
Endpoint = 162.159.192.1:2408
AllowedIPs = 0.0.0.0/0

[Socks5]
BindAddress = 127.0.0.1:40000
EOF
chmod 600 /etc/olcrtc/wireproxy.conf

# 3. Создать systemd unit
cat > /etc/systemd/system/wireproxy-warp.service << 'EOF'
[Unit]
Description=wireproxy WARP SOCKS5
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/wireproxy -c /etc/olcrtc/wireproxy.conf
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now wireproxy-warp

# 4. Проверить
curl --proxy socks5://127.0.0.1:40000 https://ifconfig.me
# Должен показать Cloudflare WARP IP

# 5. Добавить в env
echo 'OLCRTC_WARP_PROXY=127.0.0.1:40000' >> /etc/olcrtc/env
systemctl restart olcrtc-server
```

## Итого: файлы для изменения

| Файл | Что менять |
|------|-----------|
| `cmd/olcrtc/main.go` | +2 поля в config, +2 flag, передача в Run() |
| `internal/server/server.go` | +2 поля в Server, обновить Run(), изменить dial() |
| `server-install/systemd/olcrtc-launcher` | +блок OLCRTC_WARP_PROXY |
| `server-install/olcrtc-setup.sh` | +пункт меню WARP-прокси (опционально) |

**Объём изменений:** ~30 строк Go, ~15 строк bash.
**Обратная совместимость:** полная. Без `-warp-proxy` поведение не меняется.
