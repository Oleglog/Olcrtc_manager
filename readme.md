<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://www.apache.org/licenses/LICENSE-2.0.html)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>

## About

olcRTC — across the sea.

Tunnels TCP traffic over WebRTC through whitelisted Russian conferencing
services (Yandex Telemost, SaluteJazz, Wildberries Stream) so it cannot be
blocked without breaking the upstream service.

This fork ([Oleglog/Olcrtc_manager](https://github.com/Oleglog/Olcrtc_manager))
adds a one-command systemd installer, interactive management menu, SOCKS5
proxy support for signalling, multi-instance support, and pre-built binaries
for `linux/amd64` and `linux/arm64`.

The olcbox URI and QR code will be made in the future.

## Quick links

| | |
|---|---|
| **Server install (one command)** | [`server-install/`](server-install/) — [README](server-install/README.md) |
| **WARP proxy (hide VPS IP)** | [server-install/WARP-PROXY.md](server-install/WARP-PROXY.md) |
| **Android client** | [Oleglog/Exclave_FORK](https://github.com/Oleglog/Exclave_FORK) |
| **Upstream project** | [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc) |

## Server — quick start

One command, no git required:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

Or from a checkout:
```bash
git clone https://github.com/Oleglog/Olcrtc_manager
cd Olcrtc_manager
sudo bash server-install/olcrtc-setup.sh
```

The script is **self-contained** — it downloads the binary, installs the
systemd service, and prints **Carrier**, **Transport**, **Room ID** and
**Encryption key** to enter into the Android app.

After installation, re-run the same script for an interactive management menu.

See the full server documentation: **[server-install/README.md](server-install/README.md)**

## Carrier & transport matrix

| Transport | telemost | jazz | wbstream |
|-----------|:--------:|:----:|:--------:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓ | ✓ | ✓ |

Speed (descending): **datachannel** (~6 MB/s) > **vp8channel** > **seichannel** > **videochannel** (~200 KB/s)

Default carrier: **wbstream**. Default transport: **datachannel**.

## Server management

Re-run the same script — it detects the existing installation and shows a menu:

```bash
sudo bash olcrtc-setup.sh
```

Menu items include:
- Change carrier / transport
- Regenerate room ID / encryption key
- Configure SOCKS5 proxy
- Toggle debug logging
- Multiple instances (up to 20)
- Update binary
- Full uninstall

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

See [`server-install/README.md`](server-install/README.md) for details.

## Docs (upstream)

- [Quick start with containers](docs/fast.md)
- [Manual setup](docs/manual.md)
- [Settings matrix](docs/settings.md)

## Build from source

```bash
# install mage first
go install github.com/magefile/mage@latest

# build cli + ui
mage build

# build cli only
mage buildCLI

# build cli with b codec, clones b repo, builds libb.so, compiles with -tags b
mage buildCLIB

# cross-compile for linux / windows / darwin
mage cross

# android aar via gomobile
mage mobile

# container image
mage podman
mage docker

# lint / test / clean
mage lint
mage test
mage clean
```

## SOCKS5 proxy for signalling

If your VPS IP is blocked by wbstream / jazz / telemost, route signalling
through a residential SOCKS5 proxy:

```bash
sudo bash olcrtc-setup.sh --socks-proxy user:pass@host:port
```

Only provider API / signalling goes through the proxy. Client TCP tunnel
traffic exits directly from the VPS. See
[server-install/README.md § Outbound SOCKS5 proxy](server-install/README.md#outbound-socks5-proxy-when-your-vps-ip-is-blocked).

## WARP proxy (hide VPS IP)

Route client tunnel traffic through Cloudflare WARP so visited sites see a
WARP IP instead of your VPS IP. Two setup options:

- **wireproxy** — standalone daemon, no dependencies
- **3X-UI** — use an existing Xray WARP outbound as SOCKS5

Enable via menu (item 14) or env:
```bash
# /etc/olcrtc/env
OLCRTC_WARP_PROXY=127.0.0.1:40000
```

Full guide: **[server-install/WARP-PROXY.md](server-install/WARP-PROXY.md)**

## License
Apache License 2.0. See `LICENSE` for details.

<div align="center">

---


</div>
