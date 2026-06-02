#!/bin/bash

# server-prewarm Installation Script
# Usage: curl -fsSL https://raw.githubusercontent.com/vdohide-core/server-prewarm/main/install.sh | sudo bash -s -- [OPTIONS]

set -e

# ─── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ─── Defaults ──────────────────────────────────────────────────────────────────
PORT="8084"
MONGODB_URI=""
LOG_PATH="logs/service.log"
UNINSTALL=false
PREWARM_POP="auto"
STORAGE_ID=""
APP=false

APP_NAME="server-prewarm"
APP_DIR="/opt/$APP_NAME"
SERVICE_NAME="server-prewarm"
GITHUB_REPO="vdohide-core/server-prewarm"
RELEASES_URL="https://github.com/$GITHUB_REPO/releases/latest/download"

# ─── Helpers ───────────────────────────────────────────────────────────────────
print_status()  { echo -e "${GREEN}[INFO]${NC}    $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC}   $1"; }
print_section() { echo -e "\n${BLUE}══════════════════════════════════════════${NC}"; echo -e "${BLUE}  $1${NC}"; echo -e "${BLUE}══════════════════════════════════════════${NC}"; }

# ─── Argument Parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case $1 in
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --app)
            APP=true
            shift
            ;;
        --pop)
            PREWARM_POP="$2"
            shift 2
            ;;
        --storage-id)
            STORAGE_ID="$2"
            shift 2
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        --mongodb-uri)
            MONGODB_URI="$2"
            shift 2
            ;;
        --log-path)
            LOG_PATH="$2"
            shift 2
            ;;
        -h|--help)
            echo ""
            echo "  server-service Installer — vdohide-core/server-service"
            echo ""
            echo "  Usage:"
            echo "    curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo bash -s -- [OPTIONS]"
            echo ""
            echo "  Options:"
            echo "    -p, --port PORT          HTTP port (default: 8084)"
            echo "    --mongodb-uri URI         MongoDB connection string"
            echo "    --log-path PATH           Log file path (default: logs/service.log)"
            echo "    --uninstall               Remove service and app directory"
            echo ""
            echo "  Examples:"
            echo ""
            echo "    # Install"
            echo "    curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo bash -s -- \\"
            echo "        --port 8084 \\"
            echo "        --mongodb-uri \"mongodb+srv://user:pass@host/platform\""
            echo ""
            echo "    # Uninstall"
            echo "    curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo bash -s -- --uninstall"
            echo ""
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# ═══════════════════════════════════════════════════════════════════════════════
# Uninstall
# ═══════════════════════════════════════════════════════════════════════════════
if [ "$UNINSTALL" = true ]; then
    print_section "Uninstalling $APP_NAME"

    systemctl stop    $SERVICE_NAME 2>/dev/null || true
    systemctl disable $SERVICE_NAME 2>/dev/null || true

    [ -f "/etc/systemd/system/$SERVICE_NAME.service" ] && {
        rm "/etc/systemd/system/$SERVICE_NAME.service"
        systemctl daemon-reload
        print_status "Systemd service removed."
    }

    [ -d "$APP_DIR" ] && {
        rm -rf "$APP_DIR"
        print_status "Application directory removed."
    }

    print_status "✅ Uninstallation complete."
    exit 0
fi

# Root check
if [ "$(id -u)" -ne 0 ]; then
    print_error "This script must be run as root (use sudo)."
    exit 1
fi

# ═══════════════════════════════════════════════════════════════════════════════
# System Dependencies
# ═══════════════════════════════════════════════════════════════════════════════
print_section "System Dependencies"
if command -v apt-get &>/dev/null; then
    apt-get update -qq
    apt-get install -y -qq curl
elif command -v yum &>/dev/null; then
    yum install -y curl
elif command -v dnf &>/dev/null; then
    dnf install -y curl
fi
print_status "Dependencies ready."

# ═══════════════════════════════════════════════════════════════════════════════
# Application Install
# ═══════════════════════════════════════════════════════════════════════════════
print_section "Installing $APP_NAME"

systemctl stop $SERVICE_NAME 2>/dev/null || true

mkdir -p "$APP_DIR"
mkdir -p "$APP_DIR/logs"

# ── Architecture ─────────────────────────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)        BINARY="linux" ;;
    aarch64|arm64) BINARY="linux-arm64" ;;
    *)
        print_error "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# ── Download binary ──────────────────────────────────────────────────────────
print_status "Downloading binary ($BINARY) from latest release..."
curl -fsSL "$RELEASES_URL/$BINARY" -o "$APP_DIR/$APP_NAME"
chmod +x "$APP_DIR/$APP_NAME"
print_status "Binary ready: $APP_DIR/$APP_NAME"

# ─── Write .env ───────────────────────────────────────────────────────────────
if [ -f "$APP_DIR/.env" ] && [ -z "$MONGODB_URI" ]; then
    print_status "Preserving existing .env — updating parameters..."
    if grep -q "^PORT=" "$APP_DIR/.env"; then
        sed -i "s/^PORT=.*/PORT=$PORT/" "$APP_DIR/.env"
    else
        echo "PORT=$PORT" >> "$APP_DIR/.env"
    fi
    if [ -n "$PREWARM_POP" ]; then
        if grep -q "^PREWARM_POP=" "$APP_DIR/.env"; then
            sed -i "s/^PREWARM_POP=.*/PREWARM_POP=$PREWARM_POP/" "$APP_DIR/.env"
        else
            echo "PREWARM_POP=$PREWARM_POP" >> "$APP_DIR/.env"
        fi
    fi
    if [ -n "$STORAGE_ID" ]; then
        if grep -q "^STORAGE_ID=" "$APP_DIR/.env"; then
            sed -i "s/^STORAGE_ID=.*/STORAGE_ID=$STORAGE_ID/" "$APP_DIR/.env"
        else
            echo "STORAGE_ID=$STORAGE_ID" >> "$APP_DIR/.env"
        fi
    fi
else
    print_status "Writing .env..."
    cat > "$APP_DIR/.env" <<EOF
MONGODB_URI=$MONGODB_URI
PORT=$PORT
LOG_PATH=$LOG_PATH
PREWARM_POP=$PREWARM_POP
STORAGE_ID=$STORAGE_ID
EOF
    if [ -z "$MONGODB_URI" ]; then
        print_warning "MONGODB_URI is not set — edit $APP_DIR/.env before starting."
    fi
fi

# ── Systemd service ──────────────────────────────────────────────────────────
print_status "Creating systemd service..."
cat > /etc/systemd/system/$SERVICE_NAME.service <<EOF
[Unit]
Description=server-prewarm (vdohide-core background prewarmer)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$APP_NAME
Restart=always
RestartSec=5
EnvironmentFile=$APP_DIR/.env
Environment=PATH=/usr/bin:/bin

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable $SERVICE_NAME
systemctl start  $SERVICE_NAME

sleep 2
if systemctl is-active --quiet $SERVICE_NAME; then
    print_status "✅ Service running."
else
    print_error "❌ Service failed to start. Run: journalctl -u $SERVICE_NAME -e"
    exit 1
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Done
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo "════════════════════════════════════════════"
print_status "🎉 Installation complete!"
echo "════════════════════════════════════════════"
echo "  Service:  $SERVICE_NAME"
echo "  Port:     $PORT"
echo "  App dir:  $APP_DIR"
echo ""
echo "  Status:   http://localhost:$PORT/api/status"
echo "  UI:       http://localhost:$PORT/"
echo "  WS:       ws://localhost:$PORT/ws"
echo ""
echo "  Commands:"
echo "    systemctl status  $SERVICE_NAME"
echo "    systemctl restart $SERVICE_NAME"
echo "    journalctl -u $SERVICE_NAME -f"
echo "════════════════════════════════════════════"
