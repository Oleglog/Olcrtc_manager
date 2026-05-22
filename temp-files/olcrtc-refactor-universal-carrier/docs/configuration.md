# Настройка

olcrtc считывает всю свою конфигурацию среды выполнения из одного YAML-файла.
теперь флагов CLI нет.

```bash
olcrtc /etc/olcrtc/server.yaml
```

Примеры:

- [`server.example.yaml`](./server.example.yaml)
- [`client.example.yaml`](./client.example.yaml)
- [`failover.example.yaml`](./failover.example.yaml)

## Схема  

| YAML path                                                        | Значение                                                     |
|------------------------------------------------------------------|-----------------------------------------------------------|
| `mode`                                                           | `srv`, `cnc`, or `gen`                                    |
| `link`                                                           | `direct`                                                  |
| `auth.provider`                                                  | `jitsi`, `telemost`, `jazz`, `wbstream`, `none`           |
| `room.id`                                                        | conference room id                                        |
| `crypto.key` / `crypto.key_file`                                 | 64-char hex (32 bytes), inline or read from file          |
| `net.transport`                                                  | `datachannel`, `videochannel`, `seichannel`, `vp8channel` |
| `net.dns`                                                        | resolver `host:port`                                      |
| `socks.host` / `.port`                                           | client-side listener                                      |
| `socks.user` / `.pass`                                           | optional client-side auth                                 |
| `socks.proxy_addr` / `.proxy_port`                               | server-side egress proxy                                  |
| `engine.name` / `.url` / `.token`                                | only when `auth.provider: none`                           |
| `video.*`                                                        | videochannel tuning                                       |
| `vp8.*`                                                          | vp8channel tuning                                         |
| `sei.fps` / `.batch_size` / `.fragment_size` / `.ack_timeout_ms` | seichannel tuning                                         |
| `liveness.interval`                                              | control-stream ping interval, default `10s`               |
| `liveness.timeout`                                               | pong timeout, default `5s`                                |
| `liveness.failures`                                              | missed pongs before reconnect, default `3`                |
| `lifecycle.max_session_duration`                                 | planned session rebuild interval, e.g. `6h`; unset = off  |
| `traffic.max_payload_size`                                       | safe encrypted wire-message cap; `0` = transport default  |
| `traffic.min_delay` / `.max_delay`                               | optional send pacing jitter, e.g. `5ms` / `30ms`          |
| `gen.amount`                                                     | gen mode: number of rooms to create                       |
| `profiles[]`                                                     | ordered srv/cnc failover profiles                         |
| `failover.retry_delay`                                           | delay before trying the next profile, e.g. `2s`           |
| `failover.max_cycles`                                            | stop after N full profile-list passes; `0` = forever      |
| `data`                                                           | path to data directory                                    |
| `debug`                                                          | verbose logging                                           |
| `ffmpeg`                                                         | path to ffmpeg binary                                     |

`mode: cnc` refuses non-loopback `socks.host` values unless both
`socks.user` and `socks.pass` are set.

`crypto.key_file` is resolved relative to the YAML file. Do not set it
together with `crypto.key`.

## Liveness

After `CLIENT_HELLO` / `SERVER_WELCOME`, the first smux stream stays open as
an encrypted control stream. olcrtc now sends `CONTROL_PING` / `CONTROL_PONG`
messages over that stream to prove the real tunnel path still round-trips.
This detects states where a provider or WebRTC layer looks connected but the
encrypted smux path is no longer usable.

```yaml
liveness:
  interval: 10s
  timeout: 5s
  failures: 3
```

When the failure threshold is reached, the current smux session is rebuilt.
In failover mode, a profile that exits after liveness-triggered reconnect
failure lets the supervisor advance to the next profile.

## Lifecycle Rotation

`lifecycle.max_session_duration` sets a planned upper bound for one provider
call/session. When the duration expires, olcrtc cancels the active server or
client session and starts a fresh one with the same config. While this option
is enabled, clean session endings are also restarted so the peer that did not
fire the timer can follow the rebuild. This is useful for long-running
deployments where provider calls get stale, accumulate media state, or should
be periodically re-created.

```yaml
lifecycle:
  max_session_duration: 6h
```

The field is optional and disabled when omitted. Values use Go duration syntax
such as `30m`, `2h`, or `6h`; zero and negative durations are rejected.

## Traffic Shaping

`traffic` applies a shared reliability-oriented wrapper around the selected
transport. It can cap encrypted wire-message size and add small send pacing
delays without truncating data. When a payload would exceed the effective cap,
the send fails clearly instead of cutting bytes and corrupting smux.

```yaml
traffic:
  max_payload_size: 4096
  min_delay: 5ms
  max_delay: 30ms
```

The wrapper clamps the configured payload cap to the selected transport's
advertised `MaxPayloadSize`. Client and server also reduce smux frame size to
fit the effective encrypted payload cap, accounting for crypto overhead. `0`
adds no extra cap beyond the selected transport's advertised limit. Delays use
Go duration syntax; if only `min_delay` is set, it is a fixed delay. Use the
same traffic settings on both peers.

## Failover Profiles

`mode: srv` and `mode: cnc` can define `profiles`. Top-level fields are used
as common defaults; each profile overrides only the fields it sets. The CLI
runs profiles in order. If a profile fails or ends while the process is still
alive, olcrtc waits `failover.retry_delay` and starts the next profile.

```yaml
mode: srv
link: direct
crypto:
  key_file: ./olcrtc.key
net:
  dns: "1.1.1.1:53"
data: data

profiles:
  - name: wb-vp8
    auth:
      provider: wbstream
    room:
      id: "WB_ROOM_ID"
    net:
      transport: vp8channel

  - name: jitsi-dc
    auth:
      provider: jitsi
    room:
      id: "https://meet.example.org/olcrtc-room"
    net:
      transport: datachannel

failover:
  retry_delay: 2s
  max_cycles: 0
```

Both peers must use compatible profile order and room settings. This first
failover layer rebuilds the session on the next profile; active smux streams
do not migrate, but new connections can recover on the next profile.

When `debug: true` is enabled, the CLI also emits a compact supervisor status
snapshot with the active profile, per-profile start/failure counters, and
bounded failover history size.
