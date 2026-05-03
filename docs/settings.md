# Настройки

## Матрица совместимости

Сначала выбери что с чем работает:

| Transport | telemost | jazz | wbstream |
|-----------|:--------:|:----:|:--------:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓ | ✓ | ✓ |

Скорость по убыванию: datachannel (~6 МБ/с) > vp8channel > seichannel > videochannel (~200 КБ/с)

---

## Обязательные флаги

| Флаг | Что вводить |
|------|-------------|
| `-mode` | `srv` на сервере, `cnc` на клиенте |
| `-carrier` | `telemost`, `jazz` или `wbstream` |
| `-transport` | `datachannel`, `vp8channel`, `seichannel` или `videochannel` |
| `-id` | Room ID. Для jazz/wbstream можно `any` - сгенерируется автоматически |
| `-key` | Ключ шифрования hex 64 символа. Генерация: `openssl rand -hex 32` |
| `-link` | Всегда `direct` |
| `-data` | Всегда `data` |

---

## Необязательные флаги

| Флаг | Описание | По умолчанию |
|------|----------|:------------:|
| `-dns` | DNS-сервер | `1.1.1.1:53` |
| `--debug` | Подробные логи соединений | выкл |

---

## Флаги только для сервера (`-mode srv`)

| Флаг | Описание |
|------|----------|
| `-socks-proxy` | Адрес SOCKS5-прокси для исходящего трафика сервера |
| `-socks-proxy-port` | Порт этого прокси |

---

## Флаги только для клиента (`-mode cnc`)

| Флаг | Описание | По умолчанию |
|------|----------|:------------:|
| `-socks-host` | На каком адресе поднять SOCKS5 | `127.0.0.1` |
| `-socks-port` | На каком порту поднять SOCKS5 | `1080` |

---

## vp8channel

**Рекомендуется: `-vp8-fps 60 -vp8-batch 64`** (числа лучше чётные, больший batch = выше скорость)

| Флаг | Описание | По умолчанию |
|------|----------|:------------:|
| `-vp8-fps` | FPS VP8 потока | `25` |
| `-vp8-batch` | Кадров за тик | `1` |

---

## videochannel

**Рекомендуется: `-video-codec qrcode -video-w 1080 -video-h 1080 -video-fps 60 -video-bitrate 5000k -video-hw none`**

| Флаг | Описание | По умолчанию |
|------|----------|:------------:|
| `-video-codec` | `qrcode` или `tile` | `qrcode` |
| `-video-w` | Ширина в пикселях | `1920` |
| `-video-h` | Высота в пикселях | `1080` |
| `-video-fps` | FPS | `30` |
| `-video-bitrate` | Битрейт, например `2M` или `5000k` | `2M` |
| `-video-hw` | Аппаратное ускорение: `none` или `nvenc` | `none` |
| `-video-qr-recovery` | Коррекция ошибок QR: `low` / `medium` / `high` / `highest` | `low` |
| `-video-qr-size` | Размер фрагмента QR в байтах, `0` = авто | `0` |
| `-video-tile-module` | Размер тайла в пикселях 1..270 (только `tile`) | `4` |
| `-video-tile-rs` | Reed-Solomon паритет % 0..200 (только `tile`) | `20` |

Для codec `tile` нужно точно `1080x1080`.

---

## seichannel / datachannel

Дополнительных флагов нет - всё по умолчанию.

---

## Готовые команды

### telemost + vp8channel

```sh
# сервер
./olcrtc -mode srv -carrier telemost -transport vp8channel \
  -id <room-id> -key <hex-key> -link direct -data data \
  -vp8-fps 60 -vp8-batch 64

# клиент
./olcrtc -mode cnc -carrier telemost -transport vp8channel \
  -id <room-id> -key <hex-key> -link direct -data data \
  -socks-host 127.0.0.1 -socks-port 1080 \
  -vp8-fps 60 -vp8-batch 64
```

### jazz + datachannel (максимальная скорость)

```sh
# сервер - room ID создастся сам, смотри логи
./olcrtc -mode srv -carrier jazz -transport datachannel \
  -id any -key <hex-key> -link direct -data data

# клиент
./olcrtc -mode cnc -carrier jazz -transport datachannel \
  -id <room-id> -key <hex-key> -link direct -data data \
  -socks-host 127.0.0.1 -socks-port 1080
```

### telemost + videochannel (крайний случай)

```sh
# сервер
./olcrtc -mode srv -carrier telemost -transport videochannel \
  -id <room-id> -key <hex-key> -link direct -data data \
  -video-codec qrcode -video-w 1080 -video-h 1080 \
  -video-fps 60 -video-bitrate 5000k -video-hw none

# клиент
./olcrtc -mode cnc -carrier telemost -transport videochannel \
  -id <room-id> -key <hex-key> -link direct -data data \
  -socks-host 127.0.0.1 -socks-port 1080 \
  -video-codec qrcode -video-w 1080 -video-h 1080 \
  -video-fps 60 -video-bitrate 5000k -video-hw none
```
