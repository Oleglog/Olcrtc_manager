## server-v1.5.1 — hotfix for SaluteJazz "Media without auto-subscribe"

Patch release on top of `server-v1.5.0`.

### What's fixed

`auth/salutejazz` now advertises `mediaWithoutAutoSubscribeSupport: true`
in the preconnect payload. Newly created Jazz Next rooms reject the
previous `false` with HTTP 406:

```json
{"code":"ROOM_NOT_SUPPORTED_BY_CLIENT",
 "message":"Media without auto-subscribe not supported by client"}
```

This caused the server to enter a restart loop and never join the room,
manifesting as "сижу в Jazz один" — instance shows `running` in
systemd while `journalctl -u olcrtc-server` repeats `preconnect failed:
status 406`.

The Python PoC (`code/jazz_poc_datachannel.py`, `code/jazz_info.py`) has
always shipped this flag as `true`; the Go provider is now consistent.

### Upgrade

```bash
sudo /usr/local/bin/olcrtc-setup.sh --update
sudo systemctl restart olcrtc-server
sudo systemctl reset-failed olcrtc-server   # clear the restart-too-quickly state if you hit it
```

Or, on a fresh box:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/refactor-universal-carrier-fork/server-install/olcrtc-setup.sh | sudo bash
```

`INSTALLER_VERSION` is bumped to `1.5.1` so `--update` pulls assets from
this release.

### Assets

Same 11 artifacts as `server-v1.5.0`, all rebuilt:
`olcrtc-{linux,darwin,freebsd,openbsd}-{amd64,arm64}`,
`olcrtc-admin-linux-{amd64,arm64}`,
`olcrtc-windows-amd64.exe`. AAR is not regenerated.
