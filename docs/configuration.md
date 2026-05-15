# Настройка

olcrtc считывает всю свою конфигурацию среды выполнения из одного YAML-файла.
теперь флагов CLI нет.

```bash
olcrtc /etc/olcrtc/server.yaml
```

Примеры:

- [`server.example.yaml`](./server.example.yaml)
- [`client.example.yaml`](./client.example.yaml)

## Схема  

| YAML path                                                        | Значение                                                     |
|------------------------------------------------------------------|-----------------------------------------------------------|
| `mode`                                                           | `srv`, `cnc`, or `gen`                                    |
| `link`                                                           | `direct`                                                  |
| `auth.provider`                                                  | `jitsi`, `telemost`, `jazz`, `wbstream`, `none`           |
| `room.id`                                                        | conference room id                                        |
| `crypto.key`                                                     | 64-char hex (32 bytes)                                    |
| `net.transport`                                                  | `datachannel`, `videochannel`, `seichannel`, `vp8channel` |
| `net.dns`                                                        | resolver `host:port`                                      |
| `socks.host` / `.port`                                           | client-side listener                                      |
| `socks.user` / `.pass`                                           | optional client-side auth                                 |
| `socks.proxy_addr` / `.proxy_port`                               | server-side egress proxy                                  |
| `engine.name` / `.url` / `.token`                                | only when `auth.provider: none`                           |
| `video.*`                                                        | videochannel tuning                                       |
| `vp8.*`                                                          | vp8channel tuning                                         |
| `sei.fps` / `.batch_size` / `.fragment_size` / `.ack_timeout_ms` | seichannel tuning                                         |
| `gen.amount`                                                     | gen mode: number of rooms to create                       |
| `data`                                                           | path to data directory                                    |
| `debug`                                                          | verbose logging                                           |
| `ffmpeg`                                                         | path to ffmpeg binary                                     |
