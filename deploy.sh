#!/usr/bin/env bash
set -euo pipefail

# Usage:
#   ./deploy.sh                        # uses env vars below
#   DEPLOY_HOST=myserver.com ./deploy.sh
#
# Environment variables:
#   DEPLOY_HOST   server hostname or IP (required)
#   DEPLOY_USER   SSH user with write access to DEPLOY_PATH (default: root)
#   DEPLOY_PATH   where to install the binary on the server (default: /usr/local/bin/repomouse)

: "${DEPLOY_HOST:?DEPLOY_HOST is required (e.g. DEPLOY_HOST=myserver.com ./deploy.sh)}"
: "${DEPLOY_USER:=root}"
: "${DEPLOY_PATH:=/usr/local/bin/repomouse}"

BINARY=repomouse-linux-amd64

echo "==> Building repomouse for linux/amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -ldflags="-s -w" -o "$BINARY" ./cmd/repomouse
echo "    $(du -sh "$BINARY" | cut -f1) — OK"

echo "==> Deploying to ${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}"
scp "$BINARY" "${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}"
ssh "${DEPLOY_USER}@${DEPLOY_HOST}" "chmod 755 ${DEPLOY_PATH}"

echo "==> Verifying"
ssh "${DEPLOY_USER}@${DEPLOY_HOST}" "${DEPLOY_PATH} 2>&1 | head -1"

echo "==> Cleaning up"
rm "$BINARY"

echo "==> Done"
