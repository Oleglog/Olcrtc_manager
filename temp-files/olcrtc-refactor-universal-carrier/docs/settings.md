<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>


# Настройки

## Матрица совместимости

| Transport | telemost | jazz | wbstream | jitsi |
|-----------|:--------:|:----:|:--------:|:-----:|
| datachannel | - | ~ | ~ | + |
| vp8channel | + | - | + | ~ |
| seichannel | - | - | + | ~ |
| videochannel | + | - | + | ~ |

**Легенда:**
- `+` - работает (pass в E2E тестах)
- `-` - не работает / не поддерживается (fail в E2E тестах)
- `~` - нестабильно (может работать, но нестабильно)

**Jazz:** только datachannel проходит E2E тесты. Все non-data транспорты (vp8channel, seichannel, videochannel) не работают — Jazz не поддерживает VideoTrack для туннелирования. Кроме того, Jazz **банит IP** за паттерны datachannel трафика.

**Telemost:** только vp8channel стабильно проходит. DataChannel удалён из Telemost. seichannel не поддерживается. videochannel — best effort.

**WBStream:** все транспорты кроме datachannel работают. DataChannel в обычном guest flow без выдавания модератора не работает — WB Stream выдаёт токены с `canPublishData=false`, и DC не маршрутизирует данные.

**Jitsi:** datachannel стабильно проходит — реализован поверх colibri-ws bridge channel и шлёт байты через `EndpointMessage{raw}` broadcast. Подходит для self-hosted и публичных Jitsi Meet инстансов без аутентификации (`https://meet.cryptopro.ru/...`, `https://meet.jit.si/...` и т.п.). Видео-транспорты (vp8channel, seichannel, videochannel) экспонируют sendable VideoTrack через pion PeerConnection после Jingle session-accept, но Jicofo требует дополнительных протокольных шагов (LastN, ReceiverVideoConstraints, source-add) для маршрутизации видео — поэтому они помечены `~` (best effort).

**Jitsi + seichannel — отдельная оговорка.** SEI NAL-юниты идут пассажиром в H.264 видеопотоке, а Jicofo на self-hosted инстансах (например `meet.cryptopro.ru`) периодически режет/откладывает upstream видео когда ресивера в комнате формально нет — для нас это выглядит как `seichannel ack timeout` при формально живом PeerConnection. В steady-state транспорт работает, но e2e матрица помечает его `Unstable` (флаппит): зелёного и красного результата в CI достаточно, тест suite на этом не валится. Для надёжной передачи данных через jitsi предпочтительнее `datachannel` или `vp8channel`.

**Рекомендуемая комбинация: `jitsi + datachannel`** — стабильно работает на любом self-hosted или публичном Jitsi Meet (например `meet.cryptopro.ru`), не требует регистрации, простая руму создания. Альтернатива: `wbstream + vp8channel` — стабильно для коммерческих сценариев, не требует специальных прав.

Скорость по убыванию: `datachannel` > `vp8channel` > `seichannel` > `videochannel`

---

## Обязательные поля YAML конфига

| YAML поле | Что вводить |
|-----------|-------------|
| `mode` | `srv` на сервере, `cnc` на клиенте, `gen` для генерации Room ID |
| `auth.provider` | `telemost`, `jazz`, `wbstream` или `jitsi` |
| `net.transport` | `datachannel`, `vp8channel`, `seichannel` или `videochannel` |
| `room.id` | Room ID |
| `crypto.key` или `crypto.key_file` | Ключ шифрования hex 64 символа. Генерация: `openssl rand -hex 32` |
| `link` | Всегда `direct` |
| `data` | Всегда `data` |
| `net.dns` | DNS-сервер, например `1.1.1.1:53` |

---

## Необязательные поля

| YAML поле | Описание |
|-----------|----------|
| `debug` | `true` для подробных логов соединений |
| `profiles` | Список профилей failover для `srv`/`cnc` |
| `failover.retry_delay` | Пауза перед следующим профилем, например `2s` |
| `failover.max_cycles` | Сколько полных проходов по профилям сделать; `0` = бесконечно |
| `liveness.interval` | Интервал ping по control stream, по умолчанию `10s` |
| `liveness.timeout` | Сколько ждать pong, по умолчанию `5s` |
| `liveness.failures` | Сколько pong можно пропустить перед rebuild, по умолчанию `3` |
| `lifecycle.max_session_duration` | Плановый rebuild сессии после указанного времени, например `6h`; если поле не задано, выключено |
| `traffic.max_payload_size` | Лимит размера зашифрованного wire-message; `0` = лимит транспорта |
| `traffic.min_delay` / `.max_delay` | Необязательный pacing отправки, например `5ms` / `30ms` |

`crypto.key_file` читается относительно YAML-файла. Не указывай `crypto.key` и `crypto.key_file` одновременно.

Если задан `profiles`, поля верхнего уровня становятся общими defaults, а
каждый профиль переопределяет только свои `auth`, `room`, `net`, `engine` и
настройки транспорта/liveness. Порядок профилей должен совпадать на сервере и
клиенте.

`liveness` проверяет именно зашифрованный smux control stream после handshake,
а не только статус WebRTC/provider соединения. Если pong не приходит несколько
раз подряд, текущая smux-сессия пересоздается.

`lifecycle.max_session_duration` ограничивает длительность одного звонка /
provider session. Когда таймер истекает, текущая `srv` или `cnc` сессия
закрывается и стартует заново с тем же конфигом. Пока эта настройка включена,
чистое завершение сессии тоже перезапускается, чтобы второй peer мог догнать
плановый rebuild. Формат значения: `30m`, `2h`, `6h`; `0s` и отрицательные
значения не принимаются.

`traffic` добавляет общий wrapper над выбранным transport. Он может ограничить
размер зашифрованного сообщения и добавить небольшую задержку перед отправкой.
Данные не обрезаются: если сообщение не помещается в эффективный лимит, send
возвращает явную ошибку. При заданном `max_payload_size` smux frame size также
уменьшается с учетом crypto overhead; при `0` остается лимит выбранного
transport. Используй одинаковые traffic-настройки на обеих сторонах.

---

## mode: gen

Генерирует Room ID заранее, не запуская сервер. Поддерживается для auth-провайдеров с автосозданием комнат: `jazz` и `wbstream`. Для `telemost` комнату нужно создавать вручную через сайт.

**Обязательные поля:**

| YAML поле | Описание |
|-----------|----------|
| `auth.provider` | `jazz` или `wbstream` |
| `net.dns` | DNS-сервер |
| `gen.amount` | Количество комнат |

```yaml
# gen.yaml
mode: gen
auth:
  provider: wbstream
net:
  dns: "1.1.1.1:53"
gen:
  amount: 3
```

```sh
./olcrtc gen.yaml
# room-id-1
# room-id-2
# room-id-3
```

---

## Поля только для сервера (`mode: srv`)

| YAML поле | Описание |
|-----------|----------|
| `socks.proxy_addr` | Адрес SOCKS5-прокси для исходящего трафика сервера |
| `socks.proxy_port` | Порт этого прокси |

---

## Поля только для клиента (`mode: cnc`)

| YAML поле | Описание | По умолчанию |
|-----------|----------|:------------:|
| `socks.host` | На каком адресе поднять SOCKS5 | `127.0.0.1` |
| `socks.port` | На каком порту поднять SOCKS5 | `1080` |
| `socks.user` | Логин для входящих SOCKS5-подключений (необязательно) | - |
| `socks.pass` | Пароль для входящих SOCKS5-подключений (необязательно) | - |

Если `socks.user` не задан - аутентификация отключена (любой локальный клиент может подключиться).  
Если задан - клиент принимает только подключения с правильным логином и паролем (RFC 1929).

Если `socks.host` не loopback (`127.0.0.1`, `::1`, `localhost`), `socks.user` и `socks.pass` обязательны.
Это защита от случайного открытого SOCKS5-прокси в локальной сети или интернете.

---

## datachannel

Дополнительных полей нет - всё по умолчанию.

---

## vp8channel

**Рекомендуется: `fps: 60`, `batch_size: 64`** (числа лучше чётные, больший batch = выше скорость)

| YAML поле | Описание | По умолчанию |
|-----------|----------|:------------:|
| `vp8.fps` | FPS VP8 потока | `25` |
| `vp8.batch_size` | Кадров за тик | `1` |

---

## seichannel

**Рекомендуется: `fps: 60`, `batch_size: 64`, `fragment_size: 900`, `ack_timeout_ms: 2000`**

| YAML поле | Описание | По умолчанию |
|-----------|----------|:------------:|
| `sei.fps` | FPS H264 потока | `60` |
| `sei.batch_size` | Кадров за тик | `64` |
| `sei.fragment_size` | Размер фрагмента в байтах | `900` |
| `sei.ack_timeout_ms` | Таймаут ACK в миллисекундах | `2000` |

---

## videochannel

**Рекомендуется: `codec: qrcode`, `width: 1080`, `height: 1080`, `fps: 60`, `bitrate: "5000k"`, `hw: none`**

| YAML поле | Описание | По умолчанию |
|-----------|----------|:------------:|
| `video.codec` | `qrcode` или `tile` | `qrcode` |
| `video.width` | Ширина в пикселях | `1920` |
| `video.height` | Высота в пикселях | `1080` |
| `video.fps` | FPS | `30` |
| `video.bitrate` | Битрейт, например `"2M"` или `"5000k"` | `"2M"` |
| `video.hw` | Аппаратное ускорение: `none` или `nvenc` | `none` |
| `video.qr_recovery` | Коррекция ошибок QR: `low` / `medium` / `high` / `highest` | `low` |
| `video.qr_size` | Размер фрагмента QR в байтах, `0` = авто | `0` |
| `video.tile_module` | Размер тайла в пикселях 1..270 (только `tile`) | `4` |
| `video.tile_rs` | Reed-Solomon паритет % 0..200 (только `tile`) | `20` |
| `ffmpeg` | Путь к исполняемому файлу ffmpeg | `ffmpeg` |

Для codec `tile` нужно точно `1080x1080`.

---

## Готовые конфиги

### wbstream + datachannel (не работает в обычном guest flow)

WB Stream DataChannel **не работает** в обычном guest flow — WB Stream выдаёт токены с `canPublishData=false`, и DC не маршрутизирует данные. Этот режим помечен как expected fail в E2E тестах. Для обычного использования выбирай `vp8channel`, `seichannel` или `videochannel`.

```yaml
# room ID нужно создать вручную через https://stream.wb.ru

# server.yaml
mode: srv
link: direct
auth:
  provider: wbstream
room:
  id: "<room-id-со-stream.wb.ru>"
crypto:
  key: "<hex-key>"
net:
  transport: datachannel
  dns: "1.1.1.1:53"
data: data
```

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: wbstream
room:
  id: "<room-id-со-stream.wb.ru>"
crypto:
  key: "<hex-key>"
net:
  transport: datachannel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
data: data
```

### wbstream + datachannel + SOCKS5 аутентификация (не работает в обычном guest flow)

```yaml
# client.yaml с логином и паролем на прокси
mode: cnc
link: direct
auth:
  provider: wbstream
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: datachannel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
  user: myuser
  pass: mypass
data: data
```

Использование:
```sh
curl --socks5-hostname myuser:mypass@127.0.0.1:8808 https://icanhazip.com
# или
export all_proxy=socks5h://myuser:mypass@127.0.0.1:8808
```

---

### telemost + vp8channel

```yaml
# server.yaml
mode: srv
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: vp8channel
  dns: "1.1.1.1:53"
vp8:
  fps: 60
  batch_size: 64
data: data
```

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: vp8channel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
vp8:
  fps: 60
  batch_size: 64
data: data
```

### telemost + seichannel (не работает)

> ⚠️ Эта комбинация помечена как expected fail в E2E тестах. Telemost не поддерживает seichannel.

```yaml
# server.yaml
mode: srv
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: seichannel
  dns: "1.1.1.1:53"
sei:
  fps: 60
  batch_size: 64
  fragment_size: 900
  ack_timeout_ms: 2000
data: data
```

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: seichannel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
sei:
  fps: 60
  batch_size: 64
  fragment_size: 900
  ack_timeout_ms: 2000
data: data
```

### telemost + videochannel (best effort, нестабильно)

```yaml
# server.yaml
mode: srv
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: videochannel
  dns: "1.1.1.1:53"
video:
  codec: qrcode
  width: 1080
  height: 1080
  fps: 60
  bitrate: "5000k"
  hw: none
data: data
```

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: telemost
room:
  id: "<room-id>"
crypto:
  key: "<hex-key>"
net:
  transport: videochannel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
video:
  codec: qrcode
  width: 1080
  height: 1080
  fps: 60
  bitrate: "5000k"
  hw: none
data: data
```

---

Подробнее про запуск: [Быстрый старт](fast.md) · [Мануальная сборка](manual.md)

URI-формат для клиентов: [uri.md](uri.md) · [Формат подписки](sub.md)
