#!/bin/bash

echo "ЕСЛИ У ВАС ЕСТЬ ПРОБЛЕМЫ - Я В КУРСЕ, ПРОЕКТ В БЕТЕ, ПО ПРОБЛЕМАМ В ЧАТ t.me/openlibrecommunity ИЛИ ВООБЩЕ НЕКУДА, ЖДИТЕ РЕЛИЗА"


set -e

PODMAN_ID=$(tr -dc 'a-z0-9' </dev/urandom | head -c 8)
CONTAINER_NAME="olcrtc-client-$PODMAN_ID"
IMAGE_NAME="docker.io/library/golang:1.26-alpine"
REPO_URL="https://github.com/openlibrecommunity/olcrtc.git"
WORK_DIR="/tmp/olcrtc-client-$PODMAN_ID"

SOCKS_IP="127.0.0.1"
SOCKS_PORT="8808"
BRANCH="master"

while [[ $# -gt 0 ]]; do
    case $1 in
        --branch=*)
            BRANCH="${1#*=}"
            shift
            ;;
        *)
            shift
            ;;
    esac
done

echo "=== OlcRTC Client Deployment Script ==="
echo ""
echo "[*] Using branch: $BRANCH"
echo ""

if ! command -v podman &> /dev/null; then
    echo "[!] Installing Podman..."

    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    elif command -v sudo &> /dev/null; then
        SUDO="sudo"
    elif command -v doas &> /dev/null; then
        SUDO="doas"
    else
        echo "[X] No sudo/doas found and not running as root. Cannot install podman."
        exit 1
    fi

    if command -v apt &> /dev/null; then
        echo "[*] Detected apt (Debian/Ubuntu)"
        $SUDO apt update
        $SUDO apt install -y podman
    elif command -v dnf &> /dev/null; then
        echo "[*] Detected dnf (Fedora/RHEL)"
        $SUDO dnf install -y podman
    elif command -v yum &> /dev/null; then
        echo "[*] Detected yum (CentOS/RHEL)"
        $SUDO yum install -y podman
    elif command -v pacman &> /dev/null; then
        echo "[*] Detected pacman (Arch)"
        $SUDO pacman -Sy --noconfirm podman
    else
        echo "[X] Unsupported package manager. Install podman manually."
        exit 1
    fi
fi

echo "[+] Using Podman"
echo ""
echo "Select auth provider:"
echo "  1) telemost"
echo "  2) jazz"
echo "  3) wbstream"
read -p "Enter choice [1-3, default: 3]: " AUTH_CHOICE

case "$AUTH_CHOICE" in
    1)
        AUTH="telemost"
        ;;
    2)
        AUTH="jazz"
        ;;
    *)
        AUTH="wbstream"
        ;;
esac

echo "[*] Using auth: $AUTH"
echo ""

echo "Select transport:"
echo "  1) datachannel"
echo "  2) videochannel"
echo "  3) seichannel"
echo "  4) vp8channel"
read -p "Enter choice [1-4, default: 1]: " TRANSPORT_CHOICE

case "$TRANSPORT_CHOICE" in
    2)
        TRANSPORT="videochannel"
        ;;
    3)
        TRANSPORT="seichannel"
        ;;
    4)
        TRANSPORT="vp8channel"
        ;;
    *)
        TRANSPORT="datachannel"
        ;;
esac

echo "[*] Using transport: $TRANSPORT"
echo ""

read -p "Enter Room ID: " ROOM_ID

if [ -z "$ROOM_ID" ]; then
    echo "[X] Room ID cannot be empty"
    exit 1
fi

echo ""
read -p "Enter Encryption Key (hex): " KEY

if [ -z "$KEY" ]; then
    echo "[X] Encryption key cannot be empty"
    exit 1
fi

echo ""
read -p "DNS server [default: 8.8.8.8:53]: " DNS_INPUT
DNS_RAW=${DNS_INPUT:-8.8.8.8:53}

# Map 127.0.0.1 to host.containers.internal for container access
DNS="$DNS_RAW"
if [[ "$DNS_RAW" == "127.0.0.1"* ]] || [[ "$DNS_RAW" == "localhost"* ]]; then
    DNS="${DNS_RAW/127.0.0.1/host.containers.internal}"
    DNS="${DNS/localhost/host.containers.internal}"
fi

echo ""
read -p "SOCKS5 ip [default: 127.0.0.1]: " IP_INPUT
SOCKS_IP=${IP_INPUT:-127.0.0.1}

echo ""
read -p "SOCKS5 port [default: 8808]: " PORT_INPUT
SOCKS_PORT=${PORT_INPUT:-8808}

echo ""
read -p "SOCKS5 username (leave empty to disable auth): " SOCKS_USER_INPUT
SOCKS_USER=${SOCKS_USER_INPUT:-}

SOCKS_PASS=""
if [ -n "$SOCKS_USER" ]; then
    read -s -p "SOCKS5 password: " SOCKS_PASS_INPUT
    echo ""
    SOCKS_PASS=${SOCKS_PASS_INPUT:-}
fi

# Transport-specific settings
VIDEO_W=1920; VIDEO_H=1080; VIDEO_FPS=30; VIDEO_BITRATE="2M"; VIDEO_HW="none"
VIDEO_CODEC="qrcode"; VIDEO_QR_SIZE=0; VIDEO_QR_RECOVERY="low"
VIDEO_TILE_MODULE=4; VIDEO_TILE_RS=20
VP8_FPS=25; VP8_BATCH=1
SEI_FPS=20; SEI_BATCH=1; SEI_FRAG=900; SEI_ACK=3000

if [ "$TRANSPORT" = "videochannel" ]; then
    echo ""
    echo "--- Videochannel settings ---"

    echo ""
    echo "Video codec:"
    echo "  1) qrcode"
    echo "  2) tile (requires 1080x1080)"
    read -p "Enter choice [1-2, default: 1]: " VCODEC_CHOICE

    case "$VCODEC_CHOICE" in
        2)
            VIDEO_CODEC="tile"
            VIDEO_W=1080
            VIDEO_H=1080
            echo "[*] Tile codec selected - forcing 1080x1080"

            read -p "Tile module size in pixels 1..270 [default: 4]: " VTILE_MOD_INPUT
            VIDEO_TILE_MODULE=${VTILE_MOD_INPUT:-4}

            read -p "Tile Reed-Solomon parity percent 0..200 [default: 20]: " VTILE_RS_INPUT
            VIDEO_TILE_RS=${VTILE_RS_INPUT:-20}
            ;;
        *)
            VIDEO_CODEC="qrcode"

            read -p "Video width [default: 1920]: " VW_INPUT
            VIDEO_W=${VW_INPUT:-1920}

            read -p "Video height [default: 1080]: " VH_INPUT
            VIDEO_H=${VH_INPUT:-1080}

            read -p "QR error correction (low/medium/high/highest) [default: low]: " VQREC_INPUT
            VIDEO_QR_RECOVERY=${VQREC_INPUT:-low}

            read -p "QR fragment size bytes [default: 0 (auto)]: " VQRSZ_INPUT
            VIDEO_QR_SIZE=${VQRSZ_INPUT:-0}
            ;;
    esac

    read -p "Video FPS [default: 30]: " VFPS_INPUT
    VIDEO_FPS=${VFPS_INPUT:-30}

    read -p "Video bitrate [default: 2M]: " VBRT_INPUT
    VIDEO_BITRATE=${VBRT_INPUT:-2M}

    read -p "Hardware acceleration (none/nvenc) [default: none]: " VHW_INPUT
    VIDEO_HW=${VHW_INPUT:-none}
fi

if [ "$TRANSPORT" = "vp8channel" ]; then
    echo ""
    echo "--- VP8channel settings ---"

    read -p "VP8 FPS [default: 25]: " VP8FPS_INPUT
    VP8_FPS=${VP8FPS_INPUT:-25}

    read -p "VP8 batch size (frames per tick) [default: 1]: " VP8BATCH_INPUT
    VP8_BATCH=${VP8BATCH_INPUT:-1}
fi

if [ "$TRANSPORT" = "seichannel" ]; then
    echo ""
    echo "--- SEIchannel settings ---"

    read -p "SEI FPS [default: 20]: " SEIFPS_INPUT
    SEI_FPS=${SEIFPS_INPUT:-20}

    read -p "SEI batch size (frames per tick) [default: 1]: " SEIBATCH_INPUT
    SEI_BATCH=${SEIBATCH_INPUT:-1}

    read -p "SEI fragment size in bytes [default: 900]: " SEIFRAG_INPUT
    SEI_FRAG=${SEIFRAG_INPUT:-900}

    read -p "SEI ACK timeout in milliseconds [default: 3000]: " SEIACK_INPUT
    SEI_ACK=${SEIACK_INPUT:-3000}
fi

echo ""
echo "[*] Cleaning workspace..."
rm -rf $WORK_DIR
mkdir -p $WORK_DIR

echo "[*] Cloning repository..."
git clone --depth 1 --recurse-submodules --branch "$BRANCH" $REPO_URL $WORK_DIR

echo "[*] Pulling Go image..."
podman pull $IMAGE_NAME

echo "[*] Building OlcRTC..."
podman run --rm \
    --add-host=host.containers.internal:host-gateway \
    -v $WORK_DIR:/app:Z \
    -w /app \
    $IMAGE_NAME \
    sh -c "go mod tidy && go build -o olcrtc cmd/olcrtc/main.go"

if [ ! -f "$WORK_DIR/olcrtc" ]; then
    echo "[X] Build failed"
    exit 1
fi

# Generate YAML config
CONFIG_FILE="$WORK_DIR/client.yaml"
cat > "$CONFIG_FILE" <<EOF
mode: cnc
link: direct
auth:
  provider: "$AUTH"
room:
  id: "$ROOM_ID"
crypto:
  key: "$KEY"
net:
  transport: "$TRANSPORT"
  dns: "$DNS"
socks:
  host: "0.0.0.0"
  port: $SOCKS_PORT
EOF

if [ -n "$SOCKS_USER" ]; then
    cat >> "$CONFIG_FILE" <<EOF
  user: "$SOCKS_USER"
  pass: "$SOCKS_PASS"
EOF
fi

if [ "$TRANSPORT" = "vp8channel" ]; then
    cat >> "$CONFIG_FILE" <<EOF
vp8:
  fps: $VP8_FPS
  batch_size: $VP8_BATCH
EOF
fi

if [ "$TRANSPORT" = "seichannel" ]; then
    cat >> "$CONFIG_FILE" <<EOF
sei:
  fps: $SEI_FPS
  batch_size: $SEI_BATCH
  fragment_size: $SEI_FRAG
  ack_timeout_ms: $SEI_ACK
EOF
fi

if [ "$TRANSPORT" = "videochannel" ]; then
    cat >> "$CONFIG_FILE" <<EOF
video:
  width: $VIDEO_W
  height: $VIDEO_H
  fps: $VIDEO_FPS
  bitrate: "$VIDEO_BITRATE"
  hw: $VIDEO_HW
  codec: $VIDEO_CODEC
  qr_size: $VIDEO_QR_SIZE
  qr_recovery: $VIDEO_QR_RECOVERY
  tile_module: $VIDEO_TILE_MODULE
  tile_rs: $VIDEO_TILE_RS
EOF
fi

cat >> "$CONFIG_FILE" <<EOF
data: data
debug: false
EOF

echo "[*] Starting OlcRTC client..."
podman run -d \
    --name $CONTAINER_NAME \
    --add-host=host.containers.internal:host-gateway \
    --restart unless-stopped \
    -p $SOCKS_IP:$SOCKS_PORT:$SOCKS_PORT \
    -v $WORK_DIR:/app:Z \
    -w /app \
    $IMAGE_NAME \
    ./olcrtc client.yaml

sleep 2

echo ""
echo "[+] Client started successfully!"
echo ""
echo "Container name: $CONTAINER_NAME"
echo "Auth:           $AUTH"
echo "Transport:      $TRANSPORT"
echo "Room ID:        $ROOM_ID"
if [ -n "$SOCKS_USER" ]; then
echo "SOCKS5 proxy:   $SOCKS_IP:$SOCKS_PORT (auth: $SOCKS_USER)"
else
echo "SOCKS5 proxy:   $SOCKS_IP:$SOCKS_PORT"
fi
echo ""
echo "View logs:"
echo "  podman logs -f $CONTAINER_NAME"
echo ""
echo "Stop client:"
echo "  podman stop $CONTAINER_NAME"
echo ""
echo "Test proxy:"
if [ -n "$SOCKS_USER" ]; then
echo "  export all_proxy=socks5h://$SOCKS_USER:$SOCKS_PASS@$SOCKS_IP:$SOCKS_PORT"
else
echo "  export all_proxy=socks5h://$SOCKS_IP:$SOCKS_PORT"
fi
echo "  curl -fsSL https://ifconfig.me"
echo ""
