# olcRTC server — pre-built systemd installer

Pre-built statically-linked binaries of the
[olcrtc](https://github.com/openlibrecommunity/olcrtc) server CLI for
`linux/amd64` and `linux/arm64`, plus a one-shot installer that:

- detects the VPS architecture,
- installs the binary to `/usr/local/bin/olcrtc`,
- creates a dedicated `olcrtc` system user,
- generates a 256-bit hex encryption key (`/etc/olcrtc/key.hex`),
- registers a hardened `systemd` service (`olcrtc-server.service`),
- asks Wildberries Stream / Yandex SaluteJazz / Yandex Telemost to provision a
  room on first start,
- captures the auto-generated room ID from `journalctl` and pins it into the
  service environment so the same room is reused across restarts,
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

```bash
# On your VPS, as root:
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
git checkout binaries/wb_stream-server   # branch with binaries + installer
sudo ./server-install/install.sh
```

…or download the tarball / zip from the release attached to this fork and run:

```bash
tar xzf olcrtc-server-installer-*.tgz
cd olcrtc-server-installer-*
sudo ./install.sh
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

The installer is idempotent — re-running keeps the existing key and room ID
unless you ask otherwise:

```bash
sudo ./install.sh                  # update binary / unit file, keep key+room
sudo ./install.sh --regenerate     # keep key, get a new room ID
sudo ./install.sh --regenerate-key # rotate everything (key + room)
```

Rotating the key invalidates every existing client; you will need to update
the Android app profile with the new key.

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
| `/etc/olcrtc/key.hex` | root:olcrtc | 0640 | 64-char hex encryption key |
| `/etc/olcrtc/env` | root:olcrtc | 0640 | EnvironmentFile read by systemd (PROVIDER, ROOM_ID, KEY, DNS) |
| `/var/lib/olcrtc/` | olcrtc:olcrtc | 0750 | Per-process state directory |
| `/etc/systemd/system/olcrtc-server.service` | root:root | 0644 | Hardened systemd unit |

## Licenses

- olcrtc itself is **WTFPL**.
- The binaries and installer in this repository are derivative works of
  https://github.com/openlibrecommunity/olcrtc and inherit its license.
