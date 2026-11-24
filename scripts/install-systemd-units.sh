#!/usr/bin/env bash
# Install and start party + SSH tunnel + auto-update systemd units on the Pi.
#
# Expected layout:
#   /home/michael/party/                 (git clone)
#   /home/michael/party/dist/party-linux-arm64
#
# Usage (on the Pi):
#   sudo ./scripts/install-systemd-units.sh

set -euo pipefail

REPO="/home/michael/party"
PHOTOBOOTH_ENV="/etc/photobooth.env"
UNIT_DIR="/etc/systemd/system"
ENV_FILE="/etc/party.env"
BIN="${REPO}/dist/party-linux-arm64"

upsert_env_line() {
  local file="$1" key="$2" line="$3"
  local tmp
  tmp="$(mktemp)"
  if [[ -f "$file" ]] && grep -qE "^[[:space:]]*(export[[:space:]]+)?${key}=" "$file"; then
    awk -v k="$key" -v l="$line" '
      BEGIN { re = "^[[:space:]]*(export[[:space:]]+)?" k "=" }
      $0 ~ re { print l; seen=1; next }
      { print }
      END { if (!seen) print l }
    ' "$file" > "$tmp"
    cat "$tmp" > "$file"
    rm -f "$tmp"
  else
    printf '\n%s\n' "$line" >> "$file"
  fi
  chmod 600 "$file"
}

copy_twilio_from_photobooth() {
  if [[ ! -r "$PHOTOBOOTH_ENV" ]]; then
    echo "No readable $PHOTOBOOTH_ENV; Twilio keys not copied"
    return 0
  fi
  local copied=0
  local line stripped key
  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line%$'\r'}"
    stripped="${line#"${line%%[![:space:]]*}"}"
    if [[ -z "$stripped" || "$stripped" == \#* ]]; then
      continue
    fi
    if [[ "$stripped" == export\ * || "$stripped" == export$'\t'* ]]; then
      stripped="${stripped#export}"
      stripped="${stripped#"${stripped%%[![:space:]]*}"}"
    fi
    case "$stripped" in
      TWILIO_*=*) ;;
      *) continue ;;
    esac
    key="${stripped%%=*}"
    upsert_env_line "$ENV_FILE" "$key" "$stripped"
    copied=$((copied + 1))
  done < "$PHOTOBOOTH_ENV"
  if [[ "$copied" -gt 0 ]]; then
    echo "Copied $copied Twilio setting(s) from $PHOTOBOOTH_ENV into $ENV_FILE"
  else
    echo "No TWILIO_* values found in $PHOTOBOOTH_ENV"
  fi
}

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root: sudo $0" >&2
  exit 1
fi

if [[ ! -d "$REPO" ]]; then
  echo "Repo not found at $REPO" >&2
  exit 1
fi

if [[ ! -x "$BIN" && -f "$BIN" ]]; then
  chmod +x "$BIN"
fi

if [[ ! -x "$BIN" ]]; then
  echo "Missing executable binary: $BIN" >&2
  echo "On a build machine run: make pi  &&  commit/copy dist/party-linux-arm64" >&2
  exit 1
fi

chmod +x "${REPO}/scripts/tunnel.sh" "${REPO}/scripts/auto-update.sh" "${REPO}/scripts/install-systemd-units.sh"

install -m 644 "${REPO}/systemd/party.service" "${UNIT_DIR}/party.service"
install -m 644 "${REPO}/systemd/party-tunnel.service" "${UNIT_DIR}/party-tunnel.service"
install -m 644 "${REPO}/systemd/party-update.service" "${UNIT_DIR}/party-update.service"
install -m 644 "${REPO}/systemd/party-update.timer" "${UNIT_DIR}/party-update.timer"
if [[ ! -f "$ENV_FILE" ]]; then
  install -m 600 "${REPO}/systemd/party.env.example" "$ENV_FILE"
  echo "Installed $ENV_FILE"
else
  echo "Left existing $ENV_FILE in place"
fi
copy_twilio_from_photobooth

systemctl daemon-reload
systemctl enable --now party.service party-tunnel.service party-update.timer

echo
systemctl --no-pager --full status party.service party-tunnel.service || true
systemctl --no-pager --full status party-update.timer || true
echo
echo "Installed. Party: https://cubeofb.org/party/"
echo "Logs: journalctl -u party -u party-tunnel -u party-update -f"
