#!/usr/bin/env bash
# Reverse-tunnel local party (:3066) to cubeofb.org so nginx can proxy
# https://cubeofb.org/party/ → this machine.
#
# Usage:
#   ./scripts/tunnel.sh [user@]cubeofb.org

set -euo pipefail

REMOTE="${1:-${PARTY_SSH_REMOTE:-cubeofb.org}}"
LOCAL_PORT="${PARTY_PORT:-3066}"
REMOTE_PORT="${PARTY_REMOTE_PORT:-3066}"

echo "Tunneling ${REMOTE}:127.0.0.1:${REMOTE_PORT} → localhost:${LOCAL_PORT}"
echo "Leave this running. Open https://cubeofb.org/party/"

exec ssh -N \
  -o BatchMode=yes \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 \
  -o ServerAliveCountMax=3 \
  -R "127.0.0.1:${REMOTE_PORT}:127.0.0.1:${LOCAL_PORT}" \
  "$REMOTE"
