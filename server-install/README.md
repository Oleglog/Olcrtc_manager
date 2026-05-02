# olcRTC server — systemd installer

One-shot installer that puts an [olcrtc](https://github.com/openlibrecommunity/olcrtc)
server CLI on a Linux VPS with a hardened `systemd` service. The binaries
themselves are not committed to this branch — they live in
[GitHub Releases](https://github.com/Oleglog/olcrtc_FORK/releases) and the
installer fetches them on demand. You can also build them locally from source
with `./build-from-source.sh`.

What the installer does:

- detects the VPS architecture (`linux/amd64` or `linux/arm64`),
- downloads the matching pre-built binary from the GitHub Release that
  corresponds to this installer version (or uses a local `bin/$arch` if you
  built / placed one),
- installs it to `/usr/local/bin/olcrtc` and a small launcher to
  `/usr/local/bin/olcrtc-launcher`,
- creates a dedicated `olcrtc` system user,
- generates a 256-bit hex encryption key (`/etc/olcrtc/key.hex`),
- registers a hardened `systemd` service (`olcrtc-server.service`),
- asks Wildberries Stream / Yandex SaluteJazz / Yandex Telemost to provision a
  room on first start,
- captures the auto-generated room ID from `journalctl` and pins it into the
  service environment so the same room is reused across restarts,
- supports an optional outbound SOCKS5 proxy (NO_AUTH or RFC 1929
  USER/PASSWORD), useful when the VPS IP is blocked by
  wb_stream / jazz / telemost, and an optional `-debug` flag,
- prints the credentials you need to fill into the Android app.

Default provider is **`wb_stream`**.

## Requirements

- A Linux VPS with `systemd`, `bash`, `openssl`, `curl`, `journalctl`. Any
  recent Ubuntu / Debian / Fedora / Alma / Arch will do. CGO is NOT required.
- Outbound internet access on TCP 443 + UDP (for ICE/TURN). No inbound ports
  need to be opened.
- `x86_64` or `aarch64` CPU.
- Recommended: 1 vCPU, 1 GB RAM, 10 GB disk. The binary is ~20 MB and uses
  ~50–250 MB RAM depending on traffic.

## Quick start (default — wb_stream)

**Option A — from a clean checkout of master** (binary auto-downloaded from
the matching Release):

```bash
# On your VPS, as root:
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
sudo ./server-install/install.sh
```

**Option B — from a release tarball** (binary already inside):

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/olcrtc_FORK/releases/latest/download/olcrtc-server-installer-0.1.2.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo /tmp/olcrtc-server-installer-*/install.sh
```

**Option C — build from source** (no GitHub access on the VPS / reproducible
build required):

```bash
cd olcrtc_FORK
./server-install/build-from-source.sh   # produces server-install/bin/olcrtc-linux-{amd64,arm64}
sudo ./server-install/install.sh
```

The installer prints the credentials you need to enter into the **olcRTC**
Android app at the end:

```
==========================================================
        olcRTC server is up.
==========================================================

  Provider:        wb_stream
  Room ID:         01HZX...
  Encryption key:  7b3c1f...
  DNS resolver:    1.1.1.1:53
  Public IP:       a.b.c.d
...
```

## Picking a different provider

```bash
sudo ./install.sh --provider telemost          # provider = telemost
sudo ./install.sh --provider jazz              # provider = jazz
sudo ./install.sh --provider wb_stream         # provider = wb_stream (default)
```

For **telemost**, the room ID is whatever string you choose — it is not
provisioned by Yandex. You can override the auto-generated `olcrtc-XXXX`
ID by setting `OLCRTC_ROOM_ID=...` before running the installer:

```bash
sudo OLCRTC_ROOM_ID=my-vpn-room ./install.sh --provider telemost
```

## Re-running the installer

The installer is idempotent — re-running keeps the existing key, room ID,
proxy and debug settings unless you ask otherwise:

```bash
sudo ./install.sh                                   # update binary / unit file, keep everything
sudo ./install.sh --regenerate                      # keep key, get a new room ID
sudo ./install.sh --regenerate-key                  # rotate everything (key + room)
sudo ./install.sh --socks-proxy host:port           # route outbound through SOCKS5 (NO_AUTH)
sudo ./install.sh --socks-proxy user:pass@h:port    # route outbound through SOCKS5 (USER/PASSWORD)
sudo ./install.sh --socks-proxy ""                  # remove existing SOCKS5 proxy
sudo ./install.sh --debug                           # enable -debug logging
sudo ./install.sh --no-debug                        # disable -debug logging
```

Rotating the key invalidates every existing client; you will need to update
the Android app profile with the new key.

## Outbound SOCKS5 proxy (when your VPS IP is blocked)

Wildberries Stream and SaluteJazz block many datacenter IPs and require a
residential / Russian IP to register a guest session. Yandex Telemost is
more permissive but can still throttle or rotate sessions on suspicious IPs.

If your VPS gets `i/o timeout` connecting to `stream.wb.ru` (or similar),
or is blocked by wb_stream / jazz, rent a residential SOCKS5 proxy. Both
`NO_AUTH` (IP-whitelisted) and `RFC 1929 USER/PASSWORD` are supported —
use whichever your provider gives you:

```bash
# IP-whitelisted proxy (no credentials):
sudo ./install.sh --socks-proxy 1.2.3.4:1080

# Username/password proxy (RFC 1929):
sudo ./install.sh --socks-proxy alice:hunter2@1.2.3.4:1080

# Optional `socks5://` / `socks5h://` scheme is accepted and stripped:
sudo ./install.sh --socks-proxy socks5://alice:hunter2@1.2.3.4:1080
```

This writes `OLCRTC_SOCKS_PROXY=...` into `/etc/olcrtc/env`. The launcher
splits credentials from `host:port` and invokes the binary with
`-socks-proxy <host> -socks-proxy-port <port>` (and
`-socks-proxy-user` / `-socks-proxy-pass` when credentials are present).
All requests to wb_stream / jazz / telemost — including the initial guest
registration HTTP call — go out through the proxy, so the upstream
providers see the proxy's IP rather than the VPS's. The WebRTC media
path itself still goes peer-to-peer over UDP (SOCKS5 cannot tunnel UDP
via CONNECT).

## Debug logging

```bash
sudo ./install.sh --debug
journalctl -u olcrtc-server -f
```

You will see ICE candidate negotiation, DTLS state changes, and per-stream
errors. Useful for diagnosing reconnects on Telemost or one-off DTLS
timeouts. Re-run with `--no-debug` to switch back.

## Manage the service

```bash
systemctl status olcrtc-server      # status snapshot
journalctl -u olcrtc-server -f      # live logs
systemctl restart olcrtc-server     # restart
systemctl stop olcrtc-server        # stop (won't restart on reboot)
systemctl disable olcrtc-server     # don't start on reboot
```

## Uninstall

```bash
sudo systemctl disable --now olcrtc-server
sudo rm -f /etc/systemd/system/olcrtc-server.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /usr/local/bin/olcrtc
sudo userdel olcrtc 2>/dev/null || true
```

## How it picks the room ID

For `wb_stream` and `jazz`, the room is allocated server-side by the
respective provider when the olcrtc binary calls their REST API on startup.
The first run uses `-id any`, which makes the upstream API allocate a fresh
room and log a line of the form:

    WB Stream room created: 01HZX...
    Jazz room created: ...

The installer scrapes that line out of `journalctl`, persists the value to
`/etc/olcrtc/env`, and restarts the service so subsequent restarts pin the
same room until you explicitly rotate it with `--regenerate`.

For `telemost`, no API call is needed — the user-supplied ID is the room.

## Where things live

| Path | Owner | Mode | Contents |
| --- | --- | --- | --- |
| `/usr/local/bin/olcrtc` | root:root | 0755 | The Go binary |
| `/usr/local/bin/olcrtc-launcher` | root:root | 0755 | Bash wrapper that translates env to flags |
| `/etc/olcrtc/key.hex` | root:olcrtc | 0640 | 64-char hex encryption key |
| `/etc/olcrtc/env` | root:olcrtc | 0640 | EnvironmentFile read by systemd (PROVIDER, ROOM_ID, KEY, DNS, DEBUG, SOCKS_PROXY) |
| `/var/lib/olcrtc/` | olcrtc:olcrtc | 0750 | Per-process state directory |
| `/etc/systemd/system/olcrtc-server.service` | root:root | 0644 | Hardened systemd unit |

## Licenses

- olcrtc itself is **WTFPL**.
- The binaries and installer in this repository are derivative works of
  https://github.com/openlibrecommunity/olcrtc and inherit its license.
