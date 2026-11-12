#!/usr/bin/env bash
set -euo pipefail

LABEL="com.goclaw.orca-serve"
PORT="${ORCA_SERVE_PORT:-6768}"
ORCA_BIN="${ORCA_BIN:-$HOME/Applications/Orca.app/Contents/MacOS/Orca}"
USER_DATA_DIR="$HOME/Library/Application Support/orca"
LOG_DIR="$HOME/Library/Logs"
AGENT_DIR="$HOME/Library/LaunchAgents"
PLIST="$AGENT_DIR/$LABEL.plist"
UID_NUM="$(id -u)"

detect_pairing_address() {
  if [[ -n "${PAIRING_ADDR:-}" ]]; then
    printf '%s' "$PAIRING_ADDR"
    return
  fi
  local ts=""
  if command -v tailscale >/dev/null 2>&1; then
    ts="$(tailscale ip -4 2>/dev/null | head -n1 || true)"
  elif [[ -x /Applications/Tailscale.app/Contents/MacOS/Tailscale ]]; then
    ts="$(/Applications/Tailscale.app/Contents/MacOS/Tailscale ip -4 2>/dev/null | head -n1 || true)"
  fi
  printf '%s' "$ts"
}

uninstall() {
  launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
  rm -f "$PLIST"
  echo "Removed $LABEL. Orca will no longer start automatically."
}

install() {
  if [[ ! -x "$ORCA_BIN" ]]; then
    echo "ERROR: Orca binary not found at $ORCA_BIN (set ORCA_BIN=/path/to/Orca)" >&2
    exit 1
  fi

  local pair_addr
  pair_addr="$(detect_pairing_address)"
  if [[ -z "$pair_addr" ]]; then
    echo "ERROR: could not detect the Tailscale IPv4. Run: PAIRING_ADDR=<ip> $0 install" >&2
    exit 1
  fi

  mkdir -p "$AGENT_DIR" "$LOG_DIR"

  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>$ORCA_BIN</string>
        <string>--user-data-dir=$USER_DATA_DIR</string>
        <string>--serve</string>
        <string>--serve-port</string>
        <string>$PORT</string>
        <string>--serve-pairing-address</string>
        <string>$pair_addr</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
    </dict>
    <key>ProcessType</key>
    <string>Interactive</string>
    <key>StandardOutPath</key>
    <string>$LOG_DIR/orca-serve.log</string>
    <key>StandardErrorPath</key>
    <string>$LOG_DIR/orca-serve.log</string>
</dict>
</plist>
EOF

  plutil -lint "$PLIST" >/dev/null

  launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true

  if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "Port $PORT is already listening (Orca may be running). Quit Orca first, then run: $0 install"
    echo "Plist written to $PLIST; it will take effect at next login."
    return
  fi

  launchctl bootstrap "gui/$UID_NUM" "$PLIST"

  for _ in $(seq 1 30); do
    if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "OK: Orca serving on $pair_addr:$PORT (started at login via $LABEL)"
      return
    fi
    sleep 1
  done
  echo "WARN: port $PORT not listening after 30s. Check $LOG_DIR/orca-serve.log" >&2
  exit 1
}

status() {
  if launchctl print "gui/$UID_NUM/$LABEL" >/dev/null 2>&1; then
    echo "LaunchAgent: loaded"
  else
    echo "LaunchAgent: not loaded"
  fi
  if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | grep -q LISTEN; then
    echo "Port $PORT: listening"
  else
    echo "Port $PORT: NOT listening"
  fi
}

case "${1:-install}" in
  install) install ;;
  uninstall) uninstall ;;
  status) status ;;
  *) echo "Usage: $0 [install|uninstall|status]" >&2; exit 2 ;;
esac
