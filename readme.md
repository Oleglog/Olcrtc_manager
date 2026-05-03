<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>

## About

olcRTC — across the sea.

Tunnels TCP traffic over WebRTC through whitelisted Russian conferencing
services (Yandex Telemost, SaluteJazz, Wildberries Stream) so it cannot be
blocked without breaking the upstream service.

This fork ([Oleglog/olcrtc_FORK](https://github.com/Oleglog/olcrtc_FORK))
adds a one-command systemd installer, interactive management menu, SOCKS5
proxy support for signalling, multi-instance support, and pre-built binaries
for `linux/amd64` and `linux/arm64`.

## Quick links

| | |
|---|---|
| **Server install (one command)** | [`server-install/`](server-install/) — [README](server-install/README.md) |
| **Android client** | [Oleglog/Exclave_FORK](https://github.com/Oleglog/Exclave_FORK) |
| **Upstream project** | [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc) |
| **Telegram** | [@openlibrecommunity](https://t.me/openlibrecommunity) |

## Server — quick start

```bash
# On your VPS, as root:
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
sudo ./server-install/install.sh
```

Or with a one-liner (no git required):
```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/olcrtc_FORK/master/server-install/install.sh | sudo bash
```

The installer prints **Carrier**, **Transport**, **Room ID** and **Encryption key** —
enter these into the Android app.

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

After installation, re-run `olcrtc-setup.sh` for an interactive menu:

```bash
sudo bash /path/to/olcrtc-setup.sh
```

Menu items include:
- Change carrier / transport
- Regenerate room ID / encryption key
- Configure SOCKS5 proxy
- Toggle debug logging
- Multiple instances (up to 20)
- Update binary
- Full uninstall

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
sudo ./server-install/install.sh --socks-proxy user:pass@host:port
```

Only provider API / signalling goes through the proxy. Client TCP tunnel
traffic exits directly from the VPS. See
[server-install/README.md § Outbound SOCKS5 proxy](server-install/README.md#outbound-socks5-proxy-when-your-vps-ip-is-blocked).

## WARP proxy (Cloudflare)

For an alternative to residential proxies, see
[server-install/WARP-PROXY.md](server-install/WARP-PROXY.md).

## License

WTFPL (inherited from upstream). See also `LICENSE`.

<div align="center">

---

Upstream: [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc)
<br>
Telegram: [@openlibrecommunity](https://t.me/openlibrecommunity)
<br>
Made for: [olcNG](https://github.com/zarazaex69/olcng)

</div>
