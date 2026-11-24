#!/usr/bin/env bash
# Check origin/main; if behind, git pull. Restart party when the
# arm64 binary changes. Intended to run every minute on the Pi via systemd.
#
# Usage:
#   sudo /home/michael/party/scripts/auto-update.sh

set -euo pipefail

REPO="/home/michael/party"
BIN_REL="dist/party-linux-arm64"
BIN="${REPO}/${BIN_REL}"
REPO_USER="michael"
LOCK_FILE="/run/party-auto-update.lock"
LOG_TAG="party-auto-update"

log() {
  echo "$(date -Iseconds) $*"
  logger -t "$LOG_TAG" "$*" 2>/dev/null || true
}

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Run as root (systemd timer or sudo)" >&2
  exit 1
fi

if [[ ! -d "${REPO}/.git" ]]; then
  log "ERROR: not a git repo: $REPO"
  exit 1
fi

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log "another update already running; exiting"
  exit 0
fi

git_as_user() {
  sudo -u "$REPO_USER" -H git -C "$REPO" "$@"
}

if ! git_as_user fetch --quiet origin main; then
  log "ERROR: git fetch origin main failed"
  exit 1
fi

LOCAL="$(git_as_user rev-parse HEAD)"
REMOTE="$(git_as_user rev-parse origin/main)"

if [[ "$LOCAL" == "$REMOTE" ]]; then
  exit 0
fi

log "update available: ${LOCAL:0:7} → ${REMOTE:0:7}"

bin_hash() {
  if [[ -f "$BIN" ]]; then
    sha256sum "$BIN" | awk '{print $1}'
  else
    echo "missing"
  fi
}

BEFORE="$(bin_hash)"

if ! git_as_user pull --ff-only --quiet origin main; then
  log "ERROR: git pull --ff-only origin main failed"
  exit 1
fi

AFTER="$(bin_hash)"
NEW_HEAD="$(git_as_user rev-parse --short HEAD)"
log "pulled to $NEW_HEAD"

if [[ -f "$BIN" ]]; then
  chmod +x "$BIN"
  chown "${REPO_USER}:${REPO_USER}" "$BIN" || true
fi

if [[ "$BEFORE" != "$AFTER" ]]; then
  log "arm64 binary changed (${BEFORE:0:12} → ${AFTER:0:12}); restarting party"
  systemctl restart party.service
  log "party.service restarted"
else
  log "arm64 binary unchanged; party left running"
fi
