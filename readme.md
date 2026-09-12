<div align="center">

# OlConnect Manager

**Панель управления, инсталлятор и серверная часть для протоколов OlConnect и OpenFlux.**

</div>

## Что это

`OlConnect Manager` разворачивает и управляет серверной стороной **OlConnect**: TCP- и IP-туннели прячутся внутри легитимного трафика доверенных сервисов (видеоконференции WebRTC и документы Yandex Docs), а наружу трафик выходит с вашего VPS.

### Поддерживаемые провайдеры (Carriers):

| Provider | Транспорты | Описание и статус |
|---|---|---|
| `openflux` | `auto`, `vyandex`, `yandex` | **Рекомендуемый.** Шифрованный L3 IP-туннель через Yandex Docs (Volga и классический редактор). Не требует регистрации комнат. |
| `telemost` | `vp8channel`, `videochannel` | WebRTC-сессии через Яндекс.Телемост с автоматической генерацией Room ID. |
| `jitsi` | `vp8channel`, `datachannel`, `seichannel`, `videochannel` | Быстрый старт через публичные серверы Jitsi Meet (meet.jit.si и др.). |
| `wbstream` | `vp8channel`, `datachannel` | WebRTC через WB Stream с поддержкой автоматизации авторизации в браузере. |

---

## Быстрый старт

На чистом Linux VPS (Ubuntu 20.04+, Debian 11+, Alma/Fedora):

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

После установки откройте веб-панель: `https://<IP-вашего-VPS>:8443`.
- При первом входе смените логин и пароль администратора.
- Сертификат панели самоподписанный — подтвердите исключение безопасности в браузере.

### Полное удаление с сервера:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

---

## Основные возможности

- **Современная веб-панель (OlConnect Manager)**: управление инстансами, выбор провайдеров (`openflux`, `telemost`, `jitsi`, `wbstream`), мониторинг статуса, статистика трафика, встроенный апдейтер.
- **Поддержка OpenFlux**: встроенная поддержка exit-node, автоматическая нормализация и детекция Volga-редактора, управление правилами iptables и raw-сокетами.
- **Подписки (`/sub/<slug>`)**: встроенный сервис подписок прямо в панели управления, позволяющий объединять любые инстансы (`olconnect://`, `olcrtc://`, `openflux://`).
- **Зашифрованные зеркала в Яндекс.Диск**: публикация подписок в зашифрованном AES-256-GCM виде на Яндекс.Диск. Клиент OlConnect автоматически скачивает обновления подписки с зеркала, даже если IP-адрес вашего VPS временно недоступен или заблокирован.
- **WARP & SOCKS5 прокси**: поддержка маршрутизации сигнального трафика через прокси и egress через Cloudflare WARP.
- **Автоматизация WB Stream**: встроенная headless-сессия Chromium для быстрого создания комнат и обновления токенов.

---

## Клиентские приложения

- **Android**: официальный клиент [OlConnect на Android](https://github.com/Oleglog/Olcrtc_client). Поддерживает импорт через ссылки `olconnect://`, `olcrtc://`, `openflux://`, QR-коды, подписки и зеркала Яндекс.Диска.

---

## Сборка из исходников

```bash
go install github.com/magefile/mage@latest
mage test && mage lint && mage build && mage cross
```

Управление сервисом через CLI установщика:
```bash
sudo bash server-install/olcrtc-setup.sh --status
sudo bash server-install/olcrtc-setup.sh --update
```

---

## Лицензия

WTFPL.
