# Скрытие IP VPS через WARP (Cloudflare)

## Проблема

Клиенту виден IP вашего VPS через WebRTC ICE-кандидаты (UDP).  
SOCKS5-прокси с RU IP решает только одну задачу — обход гео-бана при создании комнаты.  
Прокси работает по TCP, а ICE — по UDP, поэтому IP VPS всё равно утекает.

## Решение

**Network namespace** — изолированная сетевая среда на Linux.  
Создаём namespace с WireGuard-туннелем через Cloudflare WARP.  
Запускаем olcrtc **внутри** этого namespace — весь его трафик (TCP + UDP) идёт через WARP.  
Остальные сервисы VPS (3x-ui, SSH, и т.д.) **не затрагиваются**.

```
┌─────────────── VPS ───────────────────────────┐
│                                               │
│  [3x-ui, SSH, прочее]  → интернет напрямую    │
│                                               │
│  ┌──── namespace warp_olcrtc ────┐            │
│  │                               │            │
│  │  olcrtc-server                │            │
│  │    ├─ сигнализация ──→ WARP ──┼──→ SOCKS5  │
│  │    │                  (прокси)│    (RU IP)  │
│  │    └─ ICE (UDP) ─────→ WARP ──┼──→ SFU     │
│  │       клиент видит            │            │
│  │       IP WARP, не VPS         │            │
│  └───────────────────────────────┘            │
└───────────────────────────────────────────────┘
```

## Нужно ли переустанавливать olcrtc?

**Нет.**  Существующий сервер остаётся как есть.  WARP добавляется поверх — через systemd drop-in.  Ничего не удаляется и не пересоздаётся.  Ключи, room ID, прокси — всё сохраняется.

## Установка одной командой

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/warp-netns.sh | sudo bash -s install
```

Скрипт:
1. Установит `wireguard-tools` (если нет)
2. Спросит ключи WARP (можно ввести из 3x-ui или сгенерировать новые)
3. Создаст конфиг `/etc/olcrtc/warp-wg.conf`
4. Создаст namespace с WireGuard-туннелем
5. Подключит olcrtc к namespace через systemd drop-in
6. Автоматически исправит адрес SOCKS-прокси (если на localhost)
7. Включит автозапуск при перезагрузке
8. Покажет WARP IP для проверки

## Ручная установка (пошагово)

### 1. Установить wireguard-tools

```bash
apt install wireguard-tools -y
```

### 2. Создать конфиг WARP

**Вариант A — ключи из 3x-ui** (можно использовать те же):

Откройте 3x-ui → Исходящие подключения → warp → JSON.  Возьмите `secretKey`, `publicKey`, `endpoint`, `address`.

```bash
cat > /etc/olcrtc/warp-wg.conf << 'EOF'
[Interface]
PrivateKey = ВАШИ_СЕКРЕТНЫЙ_КЛЮЧ_ИЗ_3XUI
Address = 172.16.0.2/32
MTU = 1420

[Peer]
PublicKey = bmXOC+F1FxEMF9dyiK2H5/1SUtzH0JuVo51h2wPfgyo=
Endpoint = 162.159.192.1:2408
AllowedIPs = 0.0.0.0/0, ::/0
EOF
chmod 600 /etc/olcrtc/warp-wg.conf
```

> **Можно ли использовать те же ключи, что в 3x-ui?**  
> Да. Конфликт возможен только если оба WireGuard (kernel и xray) активно держат сессию одновременно.  
> На практике xray подключается к WARP по запросу — проблем обычно нет.  
> Если заметите нестабильность WARP в 3x-ui — сгенерируйте отдельные ключи (вариант B).

**Вариант B — новые ключи через wgcf:**

```bash
# Узнать последнюю версию и скачать
WGCF_VER=$(curl -fsSL https://api.github.com/repos/ViRb3/wgcf/releases/latest | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4)
curl -fsSL "https://github.com/ViRb3/wgcf/releases/download/${WGCF_VER}/wgcf_${WGCF_VER#v}_linux_amd64" -o /usr/local/bin/wgcf
chmod +x /usr/local/bin/wgcf

cd /tmp && wgcf register --accept-tos && wgcf generate
cp wgcf-profile.conf /etc/olcrtc/warp-wg.conf
chmod 600 /etc/olcrtc/warp-wg.conf
```

### 3. Установить скрипт

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/warp-netns.sh -o /usr/local/bin/warp-netns
chmod +x /usr/local/bin/warp-netns
```

### 4. Поднять namespace

```bash
sudo warp-netns up
```

### 5. Проверить

```bash
sudo warp-netns test
```

Должно показать два разных IP:
```
  WARP IP (виден клиенту):  104.28.xxx.xxx    ← Cloudflare
  VPS IP  (настоящий):      185.xxx.xxx.xxx   ← ваш VPS

  ✓ IP отличаются — VPS IP скрыт.
```

### 6. Подключить olcrtc к namespace

```bash
# systemd drop-in
mkdir -p /etc/systemd/system/olcrtc-server.service.d
cat > /etc/systemd/system/olcrtc-server.service.d/warp.conf << 'EOF'
[Unit]
After=warp-netns.service
Requires=warp-netns.service

[Service]
NetworkNamespacePath=/run/netns/warp_olcrtc
EOF

# systemd unit для автозапуска namespace
cat > /etc/systemd/system/warp-netns.service << 'EOF'
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
EOF

systemctl daemon-reload
systemctl enable warp-netns.service
systemctl restart olcrtc-server.service
```

### 7. SOCKS-прокси на localhost

Если ваш SOCKS5-прокси работает на `127.0.0.1` — из namespace он **недоступен** (разные loopback).

Замените адрес в `/etc/olcrtc/env`:

```bash
# Было:
OLCRTC_SOCKS_PROXY=127.0.0.1:1080
# Стало:
OLCRTC_SOCKS_PROXY=10.200.1.1:1080
```

И убедитесь, что прокси (3x-ui inbound) слушает на `0.0.0.0`, а не только `127.0.0.1`.

```bash
systemctl restart olcrtc-server
```

## Управление

| Команда | Описание |
|---------|----------|
| `warp-netns up` | Поднять namespace + туннель |
| `warp-netns down` | Удалить namespace (временно) |
| `warp-netns status` | Показать состояние WireGuard |
| `warp-netns test` | Сравнить WARP IP и VPS IP |
| `warp-netns uninstall` | Полностью удалить WARP, вернуть olcrtc к прямому подключению |

## Удаление WARP

```bash
sudo warp-netns uninstall
```

Или одной командой:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/warp-netns.sh | sudo bash -s uninstall
```

Это:
- Остановит и удалит namespace
- Удалит systemd drop-in (olcrtc вернётся к прямому подключению)
- Удалит warp-netns.service
- Оставит конфиг `/etc/olcrtc/warp-wg.conf` (на случай если захотите вернуть)
- Напомнит вернуть адрес прокси с `10.200.1.1` обратно на `127.0.0.1`

## Файлы

| Путь | Назначение |
|------|-----------|
| `/etc/olcrtc/warp-wg.conf` | WireGuard-конфиг с ключами WARP |
| `/usr/local/bin/warp-netns` | Скрипт управления namespace |
| `/etc/systemd/system/warp-netns.service` | Автозапуск namespace |
| `/etc/systemd/system/olcrtc-server.service.d/warp.conf` | Drop-in: olcrtc в namespace |

## FAQ

**Q: Не сломается ли существующая установка olcrtc?**  
A: Нет. Это systemd drop-in — дополнение к существующему сервису. При `uninstall` всё откатывается.

**Q: Будет ли конфликт с WARP в 3x-ui?**  
A: Обычно нет. xray использует userspace WireGuard и подключается к WARP по запросу. Kernel WireGuard в namespace держит постоянное соединение. Если заметите проблемы — сгенерируйте отдельные ключи через wgcf.

**Q: Влияет ли это на скорость?**  
A: WARP добавляет ~5-15ms латентности. Для VPN-туннеля через WebRTC это незначительно.

**Q: Что если VPS перезагрузится?**  
A: `warp-netns.service` запустится автоматически, потом `olcrtc-server.service` — всё поднимется само.
