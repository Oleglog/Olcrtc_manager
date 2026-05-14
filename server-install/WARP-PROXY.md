# Скрытие IP VPS через WARP-прокси

## Проблема

Клиент, подключённый через olcrtc, видит реальный IP VPS при посещении
сайтов (например 2ip.io). Причина: функция `Server.dial()` открывает
TCP-соединения напрямую от VPS.

Существующий SOCKS5-прокси (`-socks-proxy`) используется **только** для
signaling-трафика и должен сохранять RU-IP. Клиентский трафик через него
идти не должен.

## Решение

Флаг `-warp-proxy` / переменная `OLCRTC_WARP_PROXY` маршрутизируют
**только** клиентский tunnel-трафик через локальный SOCKS5 на базе
Cloudflare WARP. В качестве SOCKS5 можно использовать **wireproxy**
(отдельный демон) или **inbound в 3X-UI** (если панель уже установлена).

### Архитектура

```
┌──────────────────────────────────────────────────────────────┐
│  VPS                                                         │
│                                                              │
│  ┌─────────────┐    signaling     ┌──────────────────────┐   │
│  │ olcrtc-srv  │ ────────────────→│ SOCKS5 RU-прокси     │   │
│  │             │    (carrier      │ (опционально)         │   │
│  │             │     API/WS)      └──────────────────────┘   │
│  │             │                                             │
│  │             │    client TCP    ┌──────────────────────┐   │
│  │             │ ────────────────→│ WARP SOCKS5           │   │
│  │             │    (dial())      │ 127.0.0.1:40000       │   │
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
**Carrier видит:** RU residential IP (через SOCKS5 RU-прокси)
**Остальной VPS трафик:** не затронут

---

## Включение в olcrtc

### Через меню (рекомендуется)

```bash
# Запустить менеджер
sudo bash olcrtc-setup.sh
# Выбрать пункт 14 → ввести адрес (по умолчанию 127.0.0.1:40000)
```

### Через env-файл

```bash
# Добавить/изменить в /etc/olcrtc/env:
OLCRTC_WARP_PROXY=127.0.0.1:40000

# Перезапустить сервис:
sudo systemctl restart olcrtc-server
```

### Через CLI

```bash
olcrtc -mode srv ... -warp-proxy 127.0.0.1 -warp-proxy-port 40000
```

---

## Вариант A: wireproxy (автономный, без 3X-UI)

Подходит если на VPS **нет** 3X-UI или вы хотите изолировать WARP от
остальных сервисов.

### 1. Получить WARP-ключи

```bash
# Установить wgcf (генератор WARP-конфигов)
curl -fsSL https://github.com/ViRb3/wgcf/releases/latest/download/wgcf_2.2.22_linux_amd64 \
  -o /usr/local/bin/wgcf && chmod +x /usr/local/bin/wgcf

cd /tmp
wgcf register   # создаёт wgcf-account.toml
wgcf generate    # создаёт wgcf-profile.conf
cat wgcf-profile.conf
# Запомните: PrivateKey, Address, PublicKey, Endpoint
```

### 2. Установить wireproxy

```bash
WPVER=1.0.9
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  WP_ARCH="amd64" ;;
  aarch64|arm64)  WP_ARCH="arm64" ;;
esac
curl -fsSL "https://github.com/pufferffish/wireproxy/releases/download/v${WPVER}/wireproxy_linux_${WP_ARCH}.tar.gz" \
  | tar xz -C /usr/local/bin/ wireproxy
chmod +x /usr/local/bin/wireproxy
```

### 3. Создать конфиг

```bash
cat > /etc/olcrtc/wireproxy.conf << 'EOF'
[Interface]
PrivateKey = <PrivateKey из wgcf-profile.conf>
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
```

### 4. Создать systemd unit

```bash
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
```

### 5. Проверить и включить

```bash
# Проверить что WARP работает
curl --proxy socks5://127.0.0.1:40000 https://ifconfig.me
# Должен показать Cloudflare IP, не IP VPS

# Включить в olcrtc (пункт 14 в меню или вручную):
sudo bash olcrtc-setup.sh
```

---

## Вариант B: через 3X-UI (Xray-core)

Подходит если на VPS **уже установлен** 3X-UI с настроенным WARP-outbound.
Не требует установки wireproxy — Xray сам поднимает WireGuard-туннель.

### Предварительные условия

В 3X-UI (Настройки → Xray → Исходящие подключения) уже должен быть
outbound с тегом `warp`, протокол `wireguard`. Если его нет — создайте
через кнопку «WARP» в разделе исходящих подключений.

### 1. Создать SOCKS5 inbound

Перейдите: **Подключения** (левое меню) → **«+ Создать подключение»**

| Параметр | Значение |
|----------|----------|
| **Протокол** | `socks` (если нет — `mixed`) |
| **Примечание** | `olcrtc-warp` |
| **Порт** | `40000` |
| **Мониторинг IP** | `127.0.0.1` |
| **Authentication** | выключено |

Сохраните. Запомните **тег** inbound (будет в формате `inbound-40000`
или `inbound-s40000`).

> **Важно:** Мониторинг IP = `127.0.0.1` — порт доступен только локально.
> Не ставьте `0.0.0.0`, иначе порт будет открыт наружу.

### 2. Добавить правило маршрутизации

Перейдите: **Настройки** → **Xray** → **Маршрутизация** → **«+ Создать правило»**

| Параметр | Значение |
|----------|----------|
| **Входящее подключение** | тег из шага 1 (напр. `inbound-40000`) |
| **Исходящее подключение** | `warp` |

Сохраните. Нажмите «Сохранить» ещё раз в верхней панели чтобы Xray
перечитал конфиг.

### 3. Проверить

```bash
# Проверить что порт слушается
ss -tlnp | grep 40000

# Проверить что трафик идёт через WARP
curl --proxy socks5://127.0.0.1:40000 https://ifconfig.me
# Должен показать Cloudflare IP (104.28.x.x), не IP VPS
```

### 4. Включить в olcrtc

```bash
# Через меню (пункт 14):
sudo bash olcrtc-setup.sh

# Или вручную:
# В /etc/olcrtc/env добавить:
#   OLCRTC_WARP_PROXY=127.0.0.1:40000
# И перезапустить:
#   sudo systemctl restart olcrtc-server
```

---

## Сравнение вариантов

| | wireproxy | 3X-UI |
|---|-----------|-------|
| **Требуется** | wireproxy + wgcf | 3X-UI уже установлен |
| **Изоляция** | Отдельный systemd-юнит | Общий процесс с Xray |
| **Управление** | Конфиг-файл | Веб-панель |
| **Падение** | Не влияет на 3X-UI и наоборот | Рестарт Xray отключает WARP olcrtc |
| **Сложность** | ~5 команд в консоли | Пара кликов в UI |

---

## Отключение WARP

```bash
# Через меню (пункт 15):
sudo bash olcrtc-setup.sh

# Или вручную:
# Убрать OLCRTC_WARP_PROXY из /etc/olcrtc/env
# sudo systemctl restart olcrtc-server
```

Без `OLCRTC_WARP_PROXY` olcrtc работает как раньше — прямое подключение
от VPS.
