#!/usr/bin/env bash
set -euo pipefail

# Build a static repomouse binary and install it on the server.
#
# Usage:
#   DEPLOY_HOST=mouse.example.com ./deploy.sh
#   ./deploy.sh --dry-run            # build + test, show what would be copied
#   ./deploy.sh --quick              # skip tests (hotfix)
#
# Environment variables:
#   DEPLOY_HOST   server hostname or IP (required)
#   DEPLOY_USER   SSH user with write access to DEPLOY_PATH (default: root)
#   DEPLOY_PATH   where to install the binary on the server (default: /usr/local/bin/repomouse)
#   DEPLOY_REPO_USER  user that owns the repos (default: mouse)
#   DEPLOY_RELOAD run `repair-hooks` + `sync` as DEPLOY_REPO_USER afterwards (default: 1)

DRY_RUN=0
QUICK=0
for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=1 ;;
    --quick)   QUICK=1 ;;
    -h|--help) sed -n '4,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

: "${DEPLOY_HOST:?DEPLOY_HOST is required (e.g. DEPLOY_HOST=myserver.com ./deploy.sh)}"
: "${DEPLOY_USER:=root}"
: "${DEPLOY_PATH:=/usr/local/bin/repomouse}"
: "${DEPLOY_REPO_USER:=mouse}"
: "${DEPLOY_RELOAD:=1}"

BINARY=repomouse-linux-amd64
trap 'rm -f "$BINARY"' EXIT

if [[ "$QUICK" == "0" ]]; then
  echo "==> Running tests"
  go test ./...
  go vet ./...
fi

echo "==> Building repomouse for linux/amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o "$BINARY" ./cmd/repomouse
echo "    $(du -sh "$BINARY" | cut -f1) — OK"

if [[ "$DRY_RUN" == "1" ]]; then
  echo "==> Dry run: would scp $BINARY to ${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}"
  exit 0
fi

echo "==> Deploying to ${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}"
# Upload alongside the target and rename, so a failed transfer cannot leave a
# truncated binary in place (OpenSSH sessions are already running from it).
scp "$BINARY" "${DEPLOY_USER}@${DEPLOY_HOST}:${DEPLOY_PATH}.new"
ssh "${DEPLOY_USER}@${DEPLOY_HOST}" \
  "chmod 755 '${DEPLOY_PATH}.new' && mv -f '${DEPLOY_PATH}.new' '${DEPLOY_PATH}'"

echo "==> Verifying"
ssh "${DEPLOY_USER}@${DEPLOY_HOST}" "'${DEPLOY_PATH}' --help | head -1"

if [[ "$DEPLOY_RELOAD" == "1" ]]; then
  echo "==> Refreshing hooks, authorized_keys and config cache (as ${DEPLOY_REPO_USER})"
  # Must run as the repo-owning user: sync writes that user's ~/.ssh/authorized_keys
  # and ~/.repomouse/config.yaml.
  if [[ "$DEPLOY_USER" == "$DEPLOY_REPO_USER" ]]; then
    remote_prefix=""
  else
    remote_prefix="sudo -u '${DEPLOY_REPO_USER}' -- "
  fi
  ssh "${DEPLOY_USER}@${DEPLOY_HOST}" \
    "${remote_prefix}'${DEPLOY_PATH}' repair-hooks && \
     ${remote_prefix}'${DEPLOY_PATH}' sync"
fi

echo "==> Done"
