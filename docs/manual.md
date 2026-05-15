<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>

# Мануальная сборка

Этот способ для тех кто хочет собрать бинарник руками без Docker/Podman.
Нужен Go 1.25+, mage, git.

Проект в бете. По проблемам: t.me/openlibrecommunity

---

## Шаг 1: Установить git

```sh
apt install git       # Debian   / Ubuntu  / Mint
pacman -S git         # Arch    / CachyOS / Manjaro
dnf install git       # Fedora / RHEL   / CentOS
```

---

## Шаг 2: Установить Go 1.25+

### Arch / Fedora (всё просто)

```sh
pacman -S go    # Arch / CachyOS / Manjaro
dnf install go  # Fedora / RHEL   / CentOS
```

### Debian / Ubuntu (системный пакет устаревший)

На Debian/Ubuntu в репозитории обычно Go 1.19.

На Debian 13 лучше через `testing` c `APT Pinning`, чтобы не засорять ОС:

```sh
echo 'deb http://deb.debian.org/debian/ testing main non-free-firmware' | sudo tee /etc/apt/sources.list.d/testing.list

cat <<EOF | sudo tee /etc/apt/preferences.d/testing-pin
Package: *
Pin: release a=testing
Pin-Priority: 100
EOF

sudo apt update
sudo apt install -t testing golang-go

sudo update-alternatives --install /usr/bin/go go `which go` 10
sudo update-alternatives --install /usr/bin/gofmt gofmt `which gofmt` 10
```

Иначе через SDK:

```sh
apt install golang                         # ставим старый go - он нужен только чтобы скачать новый
go install golang.org/dl/go1.25.0@latest   # скачиваем установщик go1.25
~/go/bin/go1.25.0 download                 # скачиваем сам go1.25
mv ~/go/bin/go1.25.0 /usr/local/bin/go     # заменяем системный go
```

### Проверка

```sh
go version
# go version go1.25.x linux/amd64
```

---

## Шаг 3: Установить mage

mage - система сборки для Go-проектов, аналог make.

```sh
go install github.com/magefile/mage@latest
```

Добавь `~/go/bin` в PATH:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.bashrc
source ~/.bashrc
```

Проверка:

```sh
mage --version
# mage vx.x.x
```

---

## Шаг 4: Скачать репозиторий

```sh
git clone https://github.com/openlibrecommunity/olcrtc --recurse-submodules
cd olcrtc
```

`--recurse-submodules` обязателен - без него videochannel не соберётся.

---

## Шаг 5: Собрать

```sh
mage build   # текущая платформа
mage cross   # все платформы сразу (если собираешь для другой машины)
```

Результат в `build/`:

```
build/olcrtc-linux-amd64
build/olcrtc-linux-arm64
build/olcrtc-windows-amd64.exe
build/olcrtc-darwin-amd64
```

---

## Шаг 6: Сгенерировать ключ шифрования

Делается один раз на сервере. Ключ должен совпадать на сервере и клиенте.

```sh
openssl rand -hex 32 
# d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799
```

Сохрани вывод - понадобится при запуске клиента.

---

## Шаг 7: Запустить сервер

На серверной машине (VPS и т.д.). Подбери нужную комбинацию carrier + transport из матрицы в [settings.md](settings.md).

### jitsi + datachannel (рекомендуется)

Самый простой способ: используй любой self-hosted или публичный Jitsi Meet инстанс. Регистрация не нужна, имя комнаты выдумывается на лету. По умолчанию в примерах ниже — `meet.cryptopro.ru` (публичный CryptoPro Jitsi), но подойдёт любой другой (`meet.jit.si`, свой self-hosted и т.п.).

Создай YAML конфиг:

```yaml
# server.yaml
mode: srv
link: direct
auth:
  provider: jitsi
room:
  id: "https://meet.cryptopro.ru/myroom"
crypto:
  key: "d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799"
net:
  transport: datachannel
  dns: "1.1.1.1:53"
data: data
```

Запусти:

```sh
./build/olcrtc-linux-amd64 server.yaml
```

Сервер сам присоединится к комнате (в качестве участника без камеры/микрофона) и будет ждать, пока клиент тоже зайдёт. Без второго участника Jicofo не выдаёт session-initiate — это особенность Jitsi.

### wbstream + vp8channel (альтернатива)

Сначала создай руму вручную через сайт [wbstream](https://stream.wb.ru) (автогенерация через `mode: gen` для wbstream больше не поддерживается) и сохрани её ID.

`wbstream + datachannel` **не работает** в обычном guest flow — WB Stream выдаёт токены с `canPublishData=false`, и DC не маршрутизирует данные. Для обычного использования выбирай `vp8channel`.

Создай YAML конфиг:

```yaml
# server.yaml
mode: srv
link: direct
auth:
  provider: wbstream
room:
  id: "<room-id-со-stream.wb.ru>"
crypto:
  key: "d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799"
net:
  transport: vp8channel
  dns: "1.1.1.1:53"
data: data
```

Запусти:

```sh
./build/olcrtc-linux-amd64 server.yaml
```

Room ID нужно передать клиенту.

### Добавить отладку

Добавь `debug: true` в YAML конфиг - увидишь каждое соединение:

```
2026/05/03 08:05:23 Connecting link via direct/vp8channel/wbstream...
2026/05/03 08:05:25 wbstream publisher state: connected
2026/05/03 08:05:27 Link connected
2026/05/03 08:05:43 sid=3 connect icanhazip.com:443
2026/05/03 08:05:43 sid=3 connected icanhazip.com
```

---

## Шаг 8: Запустить клиент

На своей машине. Auth provider, transport, room ID и key должны совпадать с сервером.

### jitsi + datachannel (рекомендуется)

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: jitsi
room:
  id: "https://meet.cryptopro.ru/myroom"
crypto:
  key: "<hex-key-такой-же-как-на-сервере>"
net:
  transport: datachannel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
data: data
```

```sh
./build/olcrtc-linux-amd64 client.yaml
```

После запуска SOCKS5 будет слушать на `127.0.0.1:8808`. Используй любой клиент с поддержкой SOCKS5 (`curl --socks5 127.0.0.1:8808 ...`, браузер с переключателем прокси и т.п.).

### wbstream + vp8channel (альтернатива)

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: wbstream
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
data: data
```

```sh
./build/olcrtc-linux-amd64 client.yaml
```

После старта в логах появится:

```
SOCKS5 server listening on 127.0.0.1:8808
```

Если нужно защитить прокси логином и паролем (например на машине с несколькими пользователями), добавь `socks.user` и `socks.pass` в конфиг:

```yaml
# client.yaml
mode: cnc
link: direct
auth:
  provider: wbstream
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
  user: myuser
  pass: mypass
data: data
```

Без этих полей аутентификация отключена - поведение прежнее.

---

## Шаг 9: Проверить

```sh
curl --socks5-hostname 127.0.0.1:8808 https://icanhazip.com
```

Должен вернуть IP сервера.

Или выставить переменную чтобы весь трафик шёл через прокси:

```sh
export all_proxy=socks5h://127.0.0.1:8808
curl https://icanhazip.com
```

---

## Все mage таргеты

```sh
mage build    # собрать для текущей платформы
mage buildCLI # собрать только CLI бинарник
mage cross    # собрать для всех платформ
mage deps     # скачать и обновить зависимости
mage clean    # удалить build/
mage test     # запустить тесты
mage e2e      # запустить E2E тесты (нужны реальные провайдеры)
mage lint     # запустить линтер
mage podman   # собрать образ через podman
mage docker   # собрать образ через docker
mage mobile   # собрать Android AAR
```

---

Используешь скрипты вместо ручной сборки? -> [Быстрый старт](fast.md)

Все настройки и матрица совместимости -> [settings.md](settings.md)
