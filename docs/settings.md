# Настройки

## Матрица совместимости

Сначала выбери что с чем работает:

| Transport | telemost | jazz | wbstream |
|-----------|:--------:|:----:|:--------:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓¹ | ✓¹ | ✓¹ |

¹ videochannel поддерживается ядром (и Docker entrypoint), но не systemd-лаунчером
из `server-install/`. `olcrtc-setup.sh` намеренно не предлагает videochannel и
отвергает `--transport videochannel`, потому что без `-video-*` флагов бинарь
останавливается с `ErrVideoWidthRequired` и сервис падает. Для запуска
videochannel используйте `script/srv.sh` или Docker-контейнер
(`script/docker/olcrtc-entrypoint.sh`), которые задают видеофлаги.

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

## Тюнинг пропускной способности (env-переменные)

Эти переменные читаются бинарём напрямую — задаются в `/etc/olcrtc/<n>/env`
(или в окружении systemd-юнита) и подхватываются после рестарта инстанса.
Значения **должны совпадать на сервере и на клиенте** (на телефоне это значит
пересборку AAR с теми же дефолтами или установку переменных в среде хоста).

| Переменная | Что регулирует | Дефолт | Когда крутить |
|------------|----------------|:------:|---------------|
| `OLCRTC_DC_MAX_PAYLOAD` | Размер одного DataChannel-сообщения, байт | `65536` | Поднять до 131072–262144 для теста, опустить до 12288 если карьер режет крупные пакеты |
| `OLCRTC_SMUX_MAX_FRAME_SIZE` | Размер smux-PDU, байт | `65536` | Должен быть ≤ `OLCRTC_DC_MAX_PAYLOAD`, обычно равен ему |
| `OLCRTC_SMUX_MAX_RECEIVE_BUFFER` | Сессионное окно приёма, байт | `67108864` (64 MiB) | Поднять для жирных каналов с большим RTT (BDP) |
| `OLCRTC_SMUX_MAX_STREAM_BUFFER` | Окно одного логического стрима, байт | `4194304` (4 MiB) | Поднять если в одном TCP-потоке нужна максимальная скорость |

Откатиться к историческим значениям:

```env
OLCRTC_DC_MAX_PAYLOAD=12288
OLCRTC_SMUX_MAX_FRAME_SIZE=32768
OLCRTC_SMUX_MAX_RECEIVE_BUFFER=16777216
OLCRTC_SMUX_MAX_STREAM_BUFFER=1048576
```

При несовпадении значений сервер/клиент сессия может молча застрять — если
что-то идёт не так, сначала проверь, что обе стороны видят одинаковые env.

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
