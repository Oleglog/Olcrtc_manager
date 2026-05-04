# datachannel-throughput build artifacts

Test binaries built from commit `e722c68` (`feat/datachannel-throughput`) using:

```
go build -trimpath -ldflags "-s -w" -o build/olcrtc-linux-<arch> ./cmd/olcrtc
```

with `CGO_ENABLED=0` (statically linked, no glibc dependency).

## Files

| File | Target | SHA256 |
|---|---|---|
| `olcrtc-linux-amd64` | Linux x86_64 (most VPS) | see SHA256SUMS |
| `olcrtc-linux-arm64` | Linux aarch64 (Oracle ARM, Hetzner ARM) | see SHA256SUMS |

## Usage

Replace your existing `olcrtc` binary on the server (location depends on how
you installed; typical paths are `/usr/local/bin/olcrtc`, `/opt/olcrtc/olcrtc`).
Then restart the systemd unit:

```
sudo systemctl stop olcrtc@<n>           # or whatever your unit is called
sudo cp olcrtc-linux-amd64 /usr/local/bin/olcrtc
sudo chmod +x /usr/local/bin/olcrtc
sudo systemctl start olcrtc@<n>
sudo systemctl status olcrtc@<n>          # verify it started cleanly
```

Same client (Exclave_olcrtc) MUST also be on `feat/datachannel-throughput` —
APK from PR Oleglog/Exclave_olcrtc#5.

## Rollback

If anything breaks: replace with the previous binary (or just
`git checkout master && mage build` and copy the resulting binary back).

You can also temporarily disable the new tuning by setting env vars on the
server (no rebuild needed):

```
OLCRTC_DC_MAX_PAYLOAD=12288
OLCRTC_SMUX_MAX_FRAME_SIZE=32768
OLCRTC_SMUX_MAX_RECEIVE_BUFFER=16777216
OLCRTC_SMUX_MAX_STREAM_BUFFER=1048576
```

— with these, the new binary behaves exactly like master.
