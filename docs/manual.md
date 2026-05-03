# Мануальная сборка

Этот способ для тех кто хочет собрать бинарник руками без Docker/Podman.
Нужен Go 1.26+, mage, git.

Проект в бете. По проблемам: t.me/openlibrecommunity

---

## Шаг 1: Установить git

```sh
apt install git       # Debian / Ubuntu
pacman -S git         # Arch
dnf install git       # Fedora / RHEL / CentOS
```

---

## Шаг 2: Установить Go 1.26+

### Arch / Fedora (всё просто)

```sh
pacman -S go    # Arch
dnf install go  # Fedora
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
sudo apt install -t testing golang-1.26

sudo update-alternatives --install /usr/bin/go go `which go` 10
sudo update-alternatives --install /usr/bin/gofmt gofmt `which gofmt` 10
sudo update-alternatives --install /usr/bin/go go /usr/lib/go-1.26/bin/go 20
sudo update-alternatives --install /usr/bin/gofmt gofmt /usr/lib/go-1.26/bin/gofmt 20
```

Иначе через SDK:

```sh
apt install golang                          # ставим старый go - он нужен только чтобы скачать новый
go install golang.org/dl/go1.26.0@latest   # скачиваем установщик go1.26
~/go/bin/go1.26.0 download                 # скачиваем сам go1.26
mv ~/go/bin/go1.26.0 /usr/local/bin/go     # заменяем системный go
```

### Проверка

```sh
go version
# go version go1.26.x linux/amd64
```

Если версия меньше 1.26 - смотри инструкцию для Debian выше.

---

## Шаг 3: Установить mage

mage - система сборки для Go-проектов, аналог make.

```sh
go install github.com/magefile/mage@latest
```

Если после установки `mage: command not found` - добавь `~/go/bin` в PATH:

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
mage build   # текущая платформа → build/olcrtc-linux-amd64
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

### telemost + vp8channel (работает везде)

```sh
./build/olcrtc-linux-amd64 \
  -mode srv \
  -carrier telemost \
  -transport vp8channel \
  -id 75587912855134 \
  -key d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799 \
  -link direct \
  -dns 1.1.1.1:53 \
  -data data \
  -vp8-fps 60 \
  -vp8-batch 64
```

Для telemost `-id` - это ID комнаты телемоста. Создай комнату через [telemost.yandex.ru](https://telemost.yandex.ru/) и вставь ID из ссылки.

### jazz + datachannel (максимальная скорость, не работает в telemost)

```sh
./build/olcrtc-linux-amd64 \
  -mode srv \
  -carrier jazz \
  -transport datachannel \
  -id any \
  -key d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799 \
  -link direct \
  -dns 1.1.1.1:53 \
  -data data
```

При `-id any` сервер создаст комнату автоматически и напишет ID в логах:

```
Jazz room created: abc123xyz
```

Этот ID нужно передать клиенту.

### wbstream + seichannel

```sh
./build/olcrtc-linux-amd64 \
  -mode srv \
  -carrier wbstream \
  -transport seichannel \
  -id any \
  -key <hex-key> \
  -link direct \
  -dns 1.1.1.1:53 \
  -data data
```

### Добавить отладку

Добавь `--debug` к любой команде - увидишь каждое соединение:

```
2026/05/03 08:05:23 Connecting link via direct/vp8channel/telemost...
2026/05/03 08:05:25 telemost publisher state: connected
2026/05/03 08:05:27 Link connected
2026/05/03 08:05:43 sid=3 connect icanhazip.com:443
2026/05/03 08:05:43 sid=3 connected icanhazip.com
```

---

## Шаг 8: Запустить клиент

На своей машине. Carrier, transport, id и key должны **точно совпадать** с сервером.

### telemost + vp8channel

```sh
./build/olcrtc-linux-amd64 \
  -mode cnc \
  -carrier telemost \
  -transport vp8channel \
  -id 75587929855134 \
  -key d823fa01cb3e0609b67322f7cf984c4ee2e4ce2e294936fc24ef38c9e59f4799 \
  -link direct \
  -dns 1.1.1.1:53 \
  -data data \
  -socks-host 127.0.0.1 \
  -socks-port 1080 \
  -vp8-fps 60 \
  -vp8-batch 64
```

### jazz + datachannel

```sh
./build/olcrtc-linux-amd64 \
  -mode cnc \
  -carrier jazz \
  -transport datachannel \
  -id abc123xyz \
  -key <hex-key> \
  -link direct \
  -dns 1.1.1.1:53 \
  -data data \
  -socks-host 127.0.0.1 \
  -socks-port 1080
```

После старта в логах появится:

```
SOCKS5 server listening on 127.0.0.1:1080
```

---

## Шаг 9: Проверить

```sh
curl --socks5-hostname 127.0.0.1:1080 https://icanhazip.com
```

Должен вернуть IP сервера, а не домашний.

Или выставить переменную чтобы весь трафик шёл через прокси:

```sh
export all_proxy=socks5h://127.0.0.1:1080
curl https://icanhazip.com
```

---

## Все mage таргеты

```sh
mage build    # собрать для текущей платформы
mage cross    # собрать для всех платформ
mage deps     # скачать и обновить зависимости
mage clean    # удалить build/
mage test     # запустить тесты
mage lint     # запустить линтер
mage podman   # собрать образ через podman
mage docker   # собрать образ через docker
```

---

## Частые проблемы

**`mage: command not found`** - добавь `~/go/bin` в PATH (см. шаг 3).

**`go.mod:3: invalid go version`** - версия go слишком старая, нужна 1.26+ (см. шаг 2).

**`submodule path ... No such file or directory`** - забыл `--recurse-submodules`. Исправить: `git submodule update --init --recursive`.

**curl возвращает домашний IP** - подожди 10-15 секунд, соединение устанавливается не мгновенно. Смотри логи с `--debug`.
