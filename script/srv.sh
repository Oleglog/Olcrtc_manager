#!/bin/bash

echo "ЕСЛИ У ВАС ЕСТЬ ПРОБЛЕМЫ - Я В КУРСЕ, ПРОЕКТ В БЕТЕ, ПО ПРОБЛЕМАМ В ЧАТ t.me/openlibrecommunity ИЛИ ВООБЩЕ НЕКУДА, ЖДИТЕ РЕЛИЗА"

set -e

CONTAINER_NAME="olcrtc-server"
IMAGE_NAME="docker.io/library/golang:1.26-alpine"
REPO_URL="https://github.com/openlibrecommunity/olcrtc.git"
WORK_DIR="/tmp/olcrtc-deploy"
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

echo "=== OlcRTC Server Deployment Script ==="
echo ""
echo "[*] Using branch: $BRANCH"
echo ""

if ! command -v podman &> /dev/null; then
    echo "[!] Installing Podman..."

    if [ "$(id -u)" -eq 0 ]; then
        SUDO=""
    else
        SUDO="sudo"
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
echo "Select carrier:"
echo "  1) telemost"
echo "  2) jazz"
echo "  3) wbstream"
read -p "Enter choice [1-3, default: 1]: " CARRIER_CHOICE

case "$CARRIER_CHOICE" in
    2)
        CARRIER="jazz"
        ;;
    3)
        CARRIER="wbstream"
        ;;
    *)
        CARRIER="telemost"
        ;;
esac

echo "[*] Using carrier: $CARRIER"
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

if [ "$CARRIER" = "jazz" ]; then
    echo "Jazz room options:"
    echo "  1) Auto-generate new room (recommended)"
    echo "  2) Use specific room ID (enter roomId:password)"
    read -p "Enter choice [1-2, default: 1]: " JAZZ_CHOICE

    case "$JAZZ_CHOICE" in
        2)
            read -p "Enter Room ID (format: roomId:password): " ROOM_ID
            if [ -z "$ROOM_ID" ]; then
                echo "[X] Room ID cannot be empty"
                exit 1
            fi
            ;;
        *)
            ROOM_ID="any"
            echo "[*] Will auto-generate Jazz room"
            ;;
    esac
elif [ "$CARRIER" = "wbstream" ]; then
    echo "WB Stream room options:"
    echo "  1) Auto-generate new room (recommended)"
    echo "  2) Use specific room ID"
    read -p "Enter choice [1-2, default: 1]: " WB_CHOICE

    case "$WB_CHOICE" in
        2)
            read -p "Enter Room ID: " ROOM_ID
            if [ -z "$ROOM_ID" ]; then
                echo "[X] Room ID cannot be empty"
                exit 1
            fi
            ;;
        *)
            ROOM_ID="any"
            echo "[*] Will auto-generate WB Stream room"
            ;;
    esac
else
    read -p "Enter Room ID: " ROOM_ID
    if [ -z "$ROOM_ID" ]; then
        echo "[X] Room ID cannot be empty"
        exit 1
    fi
fi

echo ""
read -p "DNS server [default: 1.1.1.1:53]: " DNS_INPUT
DNS=${DNS_INPUT:-1.1.1.1:53}

echo ""
read -p "Use SOCKS5 proxy for egress? (y/N): " USE_PROXY

EXTRA_ARGS=()

if [[ "$USE_PROXY" =~ ^[Yy]$ ]]; then
    read -p "Enter SOCKS5 proxy address [default: 127.0.0.1]: " PROXY_ADDR_INPUT
    SOCKS_PROXY_ADDR=${PROXY_ADDR_INPUT:-127.0.0.1}

    read -p "Enter SOCKS5 proxy port [default: 1080]: " PROXY_PORT_INPUT
    SOCKS_PROXY_PORT=${PROXY_PORT_INPUT:-1080}

    echo "[*] Will use SOCKS5 proxy: $SOCKS_PROXY_ADDR:$SOCKS_PROXY_PORT"
    EXTRA_ARGS+=(-socks-proxy "$SOCKS_PROXY_ADDR" -socks-proxy-port "$SOCKS_PROXY_PORT")
fi

TRANSPORT_ARGS=()

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

            TRANSPORT_ARGS+=(-video-tile-module "$VIDEO_TILE_MODULE" -video-tile-rs "$VIDEO_TILE_RS")
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

            if [ "$VIDEO_QR_SIZE" -gt 0 ]; then
                TRANSPORT_ARGS+=(-video-qr-size "$VIDEO_QR_SIZE")
            fi
            TRANSPORT_ARGS+=(-video-qr-recovery "$VIDEO_QR_RECOVERY")
            ;;
    esac

    read -p "Video FPS [default: 30]: " VFPS_INPUT
    VIDEO_FPS=${VFPS_INPUT:-30}

    read -p "Video bitrate [default: 2M]: " VBRT_INPUT
    VIDEO_BITRATE=${VBRT_INPUT:-2M}

    read -p "Hardware acceleration (none/nvenc) [default: none]: " VHW_INPUT
    VIDEO_HW=${VHW_INPUT:-none}

    TRANSPORT_ARGS+=(-video-w "$VIDEO_W" -video-h "$VIDEO_H" -video-fps "$VIDEO_FPS" \
        -video-bitrate "$VIDEO_BITRATE" -video-hw "$VIDEO_HW" -video-codec "$VIDEO_CODEC")
fi

if [ "$TRANSPORT" = "vp8channel" ]; then
    echo ""
    echo "--- VP8channel settings ---"

    read -p "VP8 FPS [default: 25]: " VP8FPS_INPUT
    VP8_FPS=${VP8FPS_INPUT:-25}

    read -p "VP8 batch size (frames per tick) [default: 1]: " VP8BATCH_INPUT
    VP8_BATCH=${VP8BATCH_INPUT:-1}

    TRANSPORT_ARGS+=(-vp8-fps "$VP8_FPS" -vp8-batch "$VP8_BATCH")
fi

echo ""
echo "[*] Stopping old instance..."
podman stop $CONTAINER_NAME 2>/dev/null || true
podman rm $CONTAINER_NAME 2>/dev/null || true

echo "[*] Cleaning workspace..."
rm -rf $WORK_DIR
mkdir -p $WORK_DIR

echo "[*] Cloning repository..."
git clone --depth 1 --recurse-submodules --branch "$BRANCH" $REPO_URL $WORK_DIR

echo "[*] Pulling Go image..."
podman pull $IMAGE_NAME

echo "[*] Building OlcRTC..."
podman run --rm \
    -v $WORK_DIR:/app:Z \
    -w /app \
    $IMAGE_NAME \
    sh -c "go mod tidy && go build -o olcrtc cmd/olcrtc/main.go"

if [ ! -f "$WORK_DIR/olcrtc" ]; then
    echo "[X] Build failed"
    exit 1
fi

KEY_FILE="$HOME/.olcrtc_key"

if [ -f "$KEY_FILE" ]; then
    echo "[*] Loading existing encryption key..."
    KEY=$(cat "$KEY_FILE")
else
    echo "[*] Generating new encryption key..."
    KEY=$(openssl rand -hex 32)
    echo "$KEY" > "$KEY_FILE"
    chmod 600 "$KEY_FILE"
    echo ""
    echo "=========================================="
    echo "NEW ENCRYPTION KEY (saved to $KEY_FILE):"
    echo "$KEY"
    echo "=========================================="
    echo ""
fi

echo "[*] Starting OlcRTC server..."
podman run -d \
    --name $CONTAINER_NAME \
    --restart unless-stopped \
    -v $WORK_DIR:/app:Z \
    -w /app \
    $IMAGE_NAME \
    ./olcrtc -mode srv -carrier "$CARRIER" -id "$ROOM_ID" -key "$KEY" \
        -link direct -transport "$TRANSPORT" -dns "$DNS" -data data \
        "${EXTRA_ARGS[@]}" "${TRANSPORT_ARGS[@]}"

sleep 3

ACTUAL_ROOM_ID="$ROOM_ID"

if [ "$CARRIER" = "jazz" ] && [ "$ROOM_ID" = "any" ]; then
    echo "[*] Waiting for Jazz room creation..."
    sleep 2
    LOGS=$(podman logs $CONTAINER_NAME 2>&1)
    ACTUAL_ROOM_ID=$(echo "$LOGS" | grep -oP 'Jazz room created: \K[^\s]+' | head -1)

    if [ -z "$ACTUAL_ROOM_ID" ]; then
        echo "[!] WARNING: Could not extract Jazz room ID from logs"
        echo "[*] Full logs:"
        podman logs $CONTAINER_NAME
        ACTUAL_ROOM_ID="(check logs above)"
    else
        echo "[+] Jazz room created: $ACTUAL_ROOM_ID"
    fi
elif [ "$CARRIER" = "wbstream" ] && [ "$ROOM_ID" = "any" ]; then
    echo "[*] Waiting for WB Stream room creation..."
    sleep 2
    LOGS=$(podman logs $CONTAINER_NAME 2>&1)
    ACTUAL_ROOM_ID=$(echo "$LOGS" | grep -oP 'WB Stream room created: \K[^\s]+' | head -1)

    if [ -z "$ACTUAL_ROOM_ID" ]; then
        echo "[!] WARNING: Could not extract WB Stream room ID from logs"
        echo "[*] Full logs:"
        podman logs $CONTAINER_NAME
        ACTUAL_ROOM_ID="(check logs above)"
    else
        echo "[+] WB Stream room created: $ACTUAL_ROOM_ID"
    fi
fi

echo ""
echo "[+] Server started successfully!"
echo ""
echo "Container name: $CONTAINER_NAME"
echo "Carrier:        $CARRIER"
echo "Transport:      $TRANSPORT"
echo "Room ID:        $ACTUAL_ROOM_ID"
echo "Encryption key: $KEY"

if [ ${#EXTRA_ARGS[@]} -gt 0 ]; then
    echo "SOCKS5 proxy:   $SOCKS_PROXY_ADDR:$SOCKS_PROXY_PORT"
fi

echo ""
echo "View logs:"
echo "  podman logs -f $CONTAINER_NAME"
echo ""
echo "Stop server:"
echo "  podman stop $CONTAINER_NAME"
echo ""
echo "Client command:"
echo -n "  ./olcrtc -mode cnc -carrier \"$CARRIER\" -id \"$ACTUAL_ROOM_ID\" -key \"$KEY\" \\"
echo ""
echo -n "    -link direct -transport \"$TRANSPORT\" -dns 1.1.1.1:53 -data data \\"
echo ""

if [ "$TRANSPORT" = "videochannel" ]; then
    echo -n "    -video-w \"$VIDEO_W\" -video-h \"$VIDEO_H\" -video-fps \"$VIDEO_FPS\" \\"
    echo ""
    echo -n "    -video-bitrate \"$VIDEO_BITRATE\" -video-hw \"$VIDEO_HW\" -video-codec \"$VIDEO_CODEC\" \\"
    echo ""
    if [ "$VIDEO_CODEC" = "tile" ]; then
        echo -n "    -video-tile-module \"$VIDEO_TILE_MODULE\" -video-tile-rs \"$VIDEO_TILE_RS\" \\"
        echo ""
    fi
fi

if [ "$TRANSPORT" = "vp8channel" ]; then
    echo -n "    -vp8-fps \"$VP8_FPS\" -vp8-batch \"$VP8_BATCH\" \\"
    echo ""
fi

echo "    -socks-host 0.0.0.0 -socks-port 1080"
echo ""
