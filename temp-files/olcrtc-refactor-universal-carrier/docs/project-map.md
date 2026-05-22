# olcRTC Project Map

This is a developer map for finding the useful parts of the project quickly.
It focuses on code ownership, runtime flow, extension points, and areas that
are worth deeper work.

## One-Sentence Model

olcRTC is an encrypted TCP-over-WebRTC tunnel: the client exposes a local
SOCKS5 listener, the server dials requested TCP targets, and both sides carry
the smux byte stream through a selected WebRTC carrier and transport.

## Runtime Stack

```text
YAML config
  -> cmd/olcrtc
  -> internal/config
  -> internal/app/session
  -> internal/server or internal/client
  -> internal/link/direct
  -> internal/transport/{datachannel,vp8channel,seichannel,videochannel}
  -> internal/carrier/builtin
  -> internal/auth/<provider> + internal/engine/<engine>
  -> external service SFU / signaling
```

Tunnel data path:

```text
local app
  -> client SOCKS5
  -> smux stream
  -> muxconn AEAD encrypt
  -> link.Send
  -> transport encoding
  -> carrier/engine
  -> SFU/service
  -> peer engine/carrier
  -> transport decoding
  -> muxconn AEAD decrypt
  -> smux stream
  -> server TCP dial
  -> target host
```

## Entrypoints

| Path | Purpose |
|---|---|
| `cmd/olcrtc/main.go` | Main CLI. Accepts one YAML file, applies auth and transport defaults, starts `srv`, `cnc`, or `gen`. |
| `cmd/olcrtc-cgo/main.go` | Small c-shared entrypoint for desktop/native consumers. |
| `pkg/olcrtc` | Embeddable lower-level API that returns a `net.Conn`-like handle over an engine data path. |
| `pkg/olcrtc/tunnel` | Embeddable server-side tunnel API with auth and traffic hooks. |
| `mobile/mobile.go` | gomobile API for Android clients, including VPN socket protection. |
| `script/srv.sh`, `script/cnc.sh` | Interactive shell launchers that generate YAML and run/build the app. |
| `Dockerfile`, `script/docker/*` | Container build and server entrypoint/healthcheck. |

## Config And Session Layer

`internal/config` owns YAML parsing and file-backed secret loading.

Important fields:

| YAML | Runtime field | Notes |
|---|---|---|
| `mode` | `session.Config.Mode` | `srv`, `cnc`, or `gen`. |
| `auth.provider` | `Auth` | `jitsi`, `telemost`, `jazz`, `wbstream`, or `none`. |
| `room.id` | `RoomID` | Carrier-specific room reference. |
| `crypto.key` / `crypto.key_file` | `KeyHex` | Shared 32-byte key encoded as 64 hex chars. |
| `net.transport` | `Transport` | `datachannel`, `vp8channel`, `seichannel`, or `videochannel`. |
| `net.dns` | `DNSServer` | Resolver used by server-side target dials and provider HTTP where wired. |
| `socks.*` | SOCKS fields | Client listener and optional server egress proxy. |
| `engine.*` | direct engine fields | Used only with `auth.provider: none`. |
| `liveness.*` | control liveness | Ping/pong interval, timeout, and missed-pong threshold. |
| `lifecycle.*` | session lifecycle | Planned call/session rotation. |
| `traffic.*` | send shaping | Encrypted wire-message size cap and optional pacing jitter. |

`internal/app/session` is the main router:

1. Registers built-ins via `RegisterDefaults`.
2. Applies auth defaults: auth provider decides engine and default service URL.
3. Applies transport defaults: documented defaults for `vp8`, `sei`, and `video`.
4. Validates mode, auth, link, transport, room, key, DNS, transport options, and SOCKS listener safety.
5. Runs `server.Run`, `client.Run`, or `Gen`.

## Server Side

`internal/server` accepts encrypted smux sessions from the peer and proxies
each smux stream to a TCP target.

Core pieces:

| Symbol | Role |
|---|---|
| `server.Run` | Creates cipher, link, smux server, and serve loop. |
| `bringUpLink` | Builds `link.Link`, wires reconnect callbacks, connects carrier. |
| `installSession` / `reinstallSession` | Creates or replaces `muxconn + smux.Session`. |
| `acceptHandshake` | First smux stream; runs `handshake.Server`. |
| `handleStream` | Reads connect JSON and dispatches a tunnel stream. |
| `dispatch` | Dials target, sends ready byte, copies both directions. |
| `AuthHook` | Embedders can authorize clients after `CLIENT_HELLO`. |
| `OnSessionOpen`, `OnSessionClose`, `OnTraffic` | Observability hooks. |

Server risk areas:

- Target dialing is powerful by design. Any real product wrapper should add
  an `AuthHook` and probably destination policy.
- `defaultAuthHook` admits everyone who knows the room and key.
- Reconnect rebuilds smux sessions; active streams are sacrificed.

## Client Side

`internal/client` exposes a local SOCKS5 listener and opens one smux stream
per SOCKS CONNECT request.

Core pieces:

| Symbol | Role |
|---|---|
| `RunWithReady` | Starts link, opens smux client, listens on local SOCKS. |
| `openControlStream` | First smux stream; runs `handshake.Client`. |
| `handleSocks5` | SOCKS method negotiation and CONNECT parsing. |
| `sendConnectRequest` | Sends server-side target JSON and waits for ready byte. |
| `handleReconnect` | Rebuilds smux and control stream after carrier reconnect. |
| `resolveDeviceID` | Optional persistent client identity for hooks. |

Client risk areas:

- A non-loopback SOCKS listener must require `socks.user` and `socks.pass`.
- SOCKS credentials are simple static credentials, not a full account system.
- Existing streams do not survive reconnect; new SOCKS connections can recover.

## Wire Protocol Above WebRTC

`internal/muxconn` adapts `link.Link` to `io.ReadWriteCloser`.

- Every smux write is encrypted with `internal/crypto`.
- Every inbound link message is decrypted and appended to an internal byte buffer.
- Bad AEAD frames are dropped.
- `CanSend` provides backpressure before encrypting and sending.

`internal/crypto` uses XChaCha20-Poly1305 with a random nonce prepended to
each ciphertext.

`internal/handshake` runs on the first smux stream:

```text
CLIENT_HELLO { version, device_id, claims }
SERVER_WELCOME { version, session_id }
or
SERVER_REJECT { version, reason }
```

The handshake has a 64 KiB frame cap and a default 15 second timeout.

After handshake, `internal/control` keeps that same encrypted smux stream open
and exchanges length-prefixed JSON control messages:

```text
CONTROL_PING { version, seq, sent_unix_nano }
CONTROL_PONG { version, seq, sent_unix_nano }
```

Defaults are `liveness.interval: 10s`, `liveness.timeout: 5s`, and
`liveness.failures: 3`. Missed pongs mark the smux session unhealthy and
trigger a session rebuild/reconnect path.

Client and server runtimes also maintain a `control.Status` snapshot with
session ID, last pong time, RTT, missed pongs, reconnect count, and unhealthy
event count. Embedders can consume it through the client/server health
callbacks.

## Registries And Plugin Shape

The universal-carrier refactor centers on small registries:

| Registry | Package | Registers |
|---|---|---|
| Auth providers | `internal/auth` | Service-specific credential and room creation flows. |
| Engines | `internal/engine` | Wire-level SFU protocol implementations. |
| Carriers | `internal/carrier` | Auth + engine adapters exposed as byte/video capability providers. |
| Transports | `internal/transport` | Byte transport strategy over carrier primitives. |
| Links | `internal/link` | Higher-level link abstraction; currently only `direct`. |

`internal/carrier/builtin` connects the auth and engine worlds:

```text
carrier "wbstream" -> auth/wbstream -> engine/livekit
carrier "jazz"    -> auth/salutejazz -> engine/salutejazz
carrier "telemost"-> auth/telemost -> engine/goolom
carrier "jitsi"   -> auth/jitsi -> engine/jitsi
carrier "none"    -> direct user-supplied engine/url/token
```

## Auth Providers

| Provider | Engine | Room generation | Notes |
|---|---|---:|---|
| `jitsi` | `jitsi` | No | Parses host/room from a public or self-hosted Jitsi URL. No HTTP auth. |
| `telemost` | `goolom` | No | Calls Telemost room-info flow and returns Goolom credentials. |
| `wbstream` | `livekit` | Yes | Registers guest, optionally creates room, joins room, fetches LiveKit token. |
| `jazz` / `salutejazz` | `salutejazz` | Yes | Creates or joins SaluteJazz room and returns room/password tuple. |
| `none` | chosen by config | No | Direct engine mode for downstream tools or self-hosted SFUs. |

## Engines

Engines expose the low-level service/SFU protocol.

| Engine | Package | Byte stream | Video track | Main job |
|---|---|---:|---:|---|
| `livekit` | `internal/engine/livekit` | Yes | Yes | LiveKit SDK room, data packets, local/remote tracks, reconnect with credential refresh. |
| `goolom` | `internal/engine/goolom` | Yes | Yes | Yandex Telemost/Goolom signaling, split publisher/subscriber peer connections, telemetry/keepalive. |
| `jitsi` | `internal/engine/jitsi` | Yes | Best effort | Jitsi MUC/Jingle/colibri-ws plus optional video track negotiation. |
| `salutejazz` | `internal/engine/salutejazz` | Yes | Yes | SaluteJazz WebSocket signaling and split media peer connections. |

Engine work is where most provider breakage and reconnect complexity lives.

## Transports

Transports decide how raw tunnel bytes are carried once the carrier provides
either a byte stream or a video track.

| Transport | Primitive | Reliability model | Best fit | Notes |
|---|---|---|---|---|
| `datachannel` | Carrier byte stream | Native reliable ordered messages | Jitsi, direct engines, some Jazz cases | Simple pass-through with 12 KiB message cap. |
| `vp8channel` | VP8 video track | KCP over VP8-looking frames | WB Stream and Telemost-style video paths | Highest-performance video-path transport. Uses epochs and binding tokens to survive restarts/loopback. |
| `seichannel` | H264 SEI video track | Custom fragments + ACK/retry | WB Stream fallback | Carries data in SEI NAL units with fragmentation, CRC, ACK. |
| `videochannel` | Visual frames via ffmpeg | QR/tile frames + ACK/retry | Experimental/inspection-friendly path | Encodes visual payload frames, requires ffmpeg, supports QR and tile codecs. |

Transport work is where throughput, loss recovery, and adaptive tuning should
happen.

## Public/Embedding Surfaces

| Package | User |
|---|---|
| `pkg/olcrtc` | Go programs that want a `net.Conn` over a selected auth/engine. |
| `pkg/olcrtc/tunnel` | Go programs that want to embed the server-side tunnel with auth/traffic hooks. |
| `mobile` | Android app bindings. Wraps client mode, VPN socket protection, logging, simple health checks. |
| `cmd/olcrtc-cgo` | Native desktop/client integrations using c-shared Go export. |

These surfaces are important if the CLI becomes only one frontend among many.

## Tests

The project has broad unit coverage:

- Config/session validation and defaults.
- Auth provider HTTP flows with test servers.
- Engine helper logic and reconnect paths.
- SOCKS parsing, smux handshake, server dispatch.
- Crypto, muxconn, names, protect, logging.
- Transport frame codecs, ACK paths, KCP loopback, ffmpeg helpers.
- Memory-backed E2E tunnel tests and optional real-provider E2E matrix.

Useful commands:

```sh
go test -count=1 ./...
go test -race -count=1 ./cmd/olcrtc ./internal/app/session ./internal/config ./internal/engine/livekit
go test -race -count=1 -v ./internal/e2e
E2E_CARRIERS=wbstream E2E_TRANSPORTS=vp8channel mage e2e
go build -trimpath -o build/olcrtc ./cmd/olcrtc
```

## High-Value Coding Areas

### 1. Supervisor And Multi-Profile Failover

The first supervisor layer exists in `internal/supervisor`: the CLI can run a
prioritized list of carrier/transport profiles and move to the next profile
when the active one fails or ends.

```yaml
mode: srv
link: direct
crypto:
  key_file: ./olcrtc.key
net:
  dns: "1.1.1.1:53"
profiles:
  - name: wb-vp8
    auth:
      provider: wbstream
    room:
      id: WB_ROOM_ID
    net:
      transport: vp8channel
  - name: jitsi-dc
    auth:
      provider: jitsi
    room:
      id: https://meet.example.org/olcrtc-room
    net:
      transport: datachannel
failover:
  retry_delay: 2s
  max_cycles: 0
```

Implemented:

- Config schema for `profiles[]`.
- Ordered supervisor loop.
- `failover.retry_delay`.
- `failover.max_cycles`.
- Profile start/end logs.
- Planned session rotation with `lifecycle.max_session_duration`.
- Shared supervisor status snapshots with bounded failover history.
- Shared traffic wrapper with payload cap, pacing jitter, and smux frame sizing.

Still valuable:

- Health scoring per profile.
- Control-stream coordination before switching.
- Stream draining and migration instead of dropping active smux streams.
- User-facing status endpoint/export for the active profile and failover history.

Likely files:

- `internal/config/config.go`
- `internal/app/session/session.go`
- `internal/supervisor`
- `internal/server`
- `internal/client`
- `docs/configuration.md`
- `internal/e2e/tunnel_test.go`

### 2. Transport Telemetry And Adaptive Tuning

Add metrics from transport to link/session:

- Send queue depth.
- ACK latency.
- Retries.
- Reconnect count.
- Dropped/decrypt-failed frames.
- KCP RTT/loss where available.

Then make `vp8.batch_size`, `sei.fragment_size`, ACK timeout, and pacing
adaptive instead of static YAML knobs.

### 3. Control Stream Protocol

The first smux stream now carries control ping/pong after handshake. It is
still the natural place for:

- Server policy updates.
- Graceful reconnect notifications.
- Drain/start markers for failover.
- More per-session stats.

Likely files:

- `internal/control`
- `internal/server`
- `internal/client`

### 4. Destination Policy And Real Auth

The tunnel can dial arbitrary server-side TCP targets. A production wrapper
should use `AuthHook` and enforce:

- Allowed destination CIDRs/domains/ports.
- Per-device or per-plan policy.
- Session expiration.
- Traffic accounting limits.
- Sanitized rejection reasons.

This mostly belongs in `pkg/olcrtc/tunnel` and `internal/server`.

### 5. Provider Hardening

Provider APIs can drift. Worth adding:

- Central protected HTTP/WebSocket client creation with TLS 1.2+,
  environment proxy support, HTTP/2 for HTTP, and bounded timeouts.
- Better typed errors from auth providers.
- Provider health probes.
- Fixture-based contract tests for API response changes.
- Per-provider rate/backoff policy.
- Safer secret/log redaction.

Likely files:

- `internal/auth/*`
- `internal/engine/*`
- `internal/carrier/builtin`

### 6. Codebase Hygiene

Some public-facing text and comments are not suitable for a serious external
project. Cleaning that up would improve maintainability and downstream trust.
The most obvious targets are top-level docs and a large hostile block comment
in `internal/transport/vp8channel/transport.go`.

## Where To Look First

| Goal | Start here |
|---|---|
| Change YAML schema | `internal/config/config.go`, `cmd/olcrtc/main.go`, docs examples. |
| Change validation/defaults | `internal/app/session/session.go`. |
| Add a new auth provider | `internal/auth`, then register in `internal/carrier/builtin/register.go`. |
| Add a new SFU protocol | `internal/engine`, then connect through auth/carrier. |
| Add a new byte transport | `internal/transport`, then register in `session.RegisterDefaults`. |
| Add link behavior above transports | `internal/link`; currently only `direct`. |
| Improve SOCKS behavior | `internal/client`. |
| Improve server target dialing or policy | `internal/server`, `pkg/olcrtc/tunnel`. |
| Improve reconnect | Engines first, then `internal/client` and `internal/server` smux rebuild behavior. |
| Improve Android app integration | `mobile`, `internal/protect`, `client.RunWithReady`. |

## Mental Model For Big Changes

Prefer to keep the layer boundaries:

- Auth creates credentials; it should not know transport details.
- Engine speaks service/SFU protocol; it should not know SOCKS or smux.
- Carrier adapts auth+engine into byte/video capabilities.
- Transport turns byte/video capabilities into reliable-ish tunnel bytes.
- Link is policy above transport.
- Client/server own SOCKS, smux, handshake, target dialing, and session hooks.

If a change crosses more than two layers, it probably deserves a new
orchestrator package instead of pushing more state into an engine or transport.
