#!/usr/bin/env bash
# KAVIRA — remote deploy (cross-compile + scp + systemd user service)
#
# KAVIRA is one static Go binary with the console embedded, so there is no
# image build, no Kubernetes, and no rsync of a checkout. This script builds
# linux binary locally, copies it to the host, and runs it as a systemd *user*
# service that needs no root.
#
# Usage:
#   ./scripts/deploy-remote.sh HOST USER        e.g. 80.79.5.173 sus
#   ./scripts/deploy-remote.sh user@host
#   ./scripts/deploy-remote.sh user@host --dry-run | --verify-only | --status
#
# Flags:
#   --demo   keep the documented demo login (admin / Admin@321), like Netra's lab deploys. Off by default.
#   --http   serve plain HTTP. Default is HTTPS with a self-signed certificate that covers the host.
#
# Environment:
#   KAVIRA_REMOTE_DIR   install dir relative to $HOME   (default: .kavira)
#   KAVIRA_REMOTE_PORT  listen port                     (default: 8090)
#   KAVIRA_REMOTE_ARCH  GOARCH of the host              (default: detected)
#   KAVIRA_DEPLOY_WARN_DISK_PCT / KAVIRA_DEPLOY_MAX_DISK_PCT   (default: 80 / 95)
#
# TLS: kavira terminates HTTPS itself with a self-signed certificate kept in $HOME/<dir>/tls, regenerated only
# when the host changes or it nears expiry, so a browser's "accept the risk" decision sticks. Browsers warn once.
#
# Secrets: the session key is random, created once in $HOME/<dir>/env (mode 600). Without --demo the admin
# password is random too and never leaves the host. With --demo no password is set, so the demo gate is on:
# anyone who can reach the port can sign in. That is a test-box choice, so it must be asked for.
#
# Own directory by design: other projects' deploy scripts rsync --delete into
# ~/.deployments, so KAVIRA stays out of it.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIR="${KAVIRA_REMOTE_DIR:-.kavira}"
PORT="${KAVIRA_REMOTE_PORT:-8090}"
WARN_PCT="${KAVIRA_DEPLOY_WARN_DISK_PCT:-80}"
MAX_PCT="${KAVIRA_DEPLOY_MAX_DISK_PCT:-95}"
# One multiplexed connection for the whole run: every step otherwise pays a fresh handshake, which is slow on a busy host.
# ControlPath stays under /tmp because unix socket paths are limited to ~104 bytes.
SSH_OPTS=(-o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ServerAliveInterval=30
  -o ControlMaster=auto -o "ControlPath=/tmp/kavira-ssh-%C" -o ControlPersist=120)
MODE="deploy"
DEMO=false
SCHEME=https
POSITIONAL=()

usage() { sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'; exit 0; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage ;;
    --dry-run) MODE="dry-run"; shift ;;
    --verify-only) MODE="verify"; shift ;;
    --status) MODE="status"; shift ;;
    --demo) DEMO=true; shift ;;
    --http) SCHEME=http; shift ;;
    -*) echo "unknown flag: $1" >&2; exit 2 ;;
    *) POSITIONAL+=("$1"); shift ;;
  esac
done

case ${#POSITIONAL[@]} in
  1) TARGET="${POSITIONAL[0]}" ;;
  2) if [[ "${POSITIONAL[0]}" == *@* ]]; then TARGET="${POSITIONAL[0]}"
     elif [[ "${POSITIONAL[1]}" == *@* ]]; then TARGET="${POSITIONAL[1]}"
     else TARGET="${POSITIONAL[1]}@${POSITIONAL[0]}"; fi ;;
  *) echo "usage: $0 HOST USER | user@host [--dry-run|--verify-only|--status]" >&2; exit 2 ;;
esac
HOST="${TARGET#*@}"

cleanup() { ssh "${SSH_OPTS[@]}" -O exit "$TARGET" >/dev/null 2>&1 || true; }
trap cleanup EXIT

say() { printf '==> %s\n' "$*"; }
# shellcheck disable=SC2029 # arguments are meant to expand on the client side
remote() { ssh "${SSH_OPTS[@]}" "$TARGET" "$@"; }

CURL=(curl -fsS --max-time 10)
[[ "$SCHEME" == https ]] && CURL+=(-k)   # self-signed

verify() {
  local base="$SCHEME://$HOST:$PORT" out
  say "verifying $base"
  out="$("${CURL[@]}" "$base/api/v1/health")" || { echo "health check failed" >&2; return 1; }
  echo "$out"
  if $DEMO; then
    grep -q '"demo_gate": true' <<<"$out" || { echo "expected the demo gate to be on (--demo)" >&2; return 1; }
  else
    grep -q '"demo_gate": false' <<<"$out" || { echo "demo gate is enabled on the remote host (pass --demo if intended)" >&2; return 1; }
  fi
  "${CURL[@]}" -o /dev/null "$base/" && echo "console: ok"
  local code
  code="$(curl -sk -o /dev/null -w '%{http_code}' --max-time 10 "$base/api/v1/incidents")"
  [[ "$code" == 401 ]] || { echo "unauthenticated /incidents returned $code, want 401" >&2; return 1; }
  echo "auth gate: ok (401 without a session)"
  if $DEMO; then
    local c; c="$(curl -sk -o /dev/null -w '%{http_code}' --max-time 10 -H 'Content-Type: application/json' -d '{"username":"admin","password":"Admin@321"}' "$base/api/v1/session")"
    [[ "$c" == 200 ]] || { echo "demo login returned $c, want 200" >&2; return 1; }
    echo "demo login (admin / Admin@321): ok"
  fi
  if [[ "$SCHEME" == https ]]; then
    local sc; sc="$(curl -sk -D - -o /dev/null --max-time 10 -H 'Content-Type: application/json' -d '{"username":"admin","password":"Admin@321"}' "$base/api/v1/session" | tr -d '\r' | grep -i '^set-cookie' || true)"
    [[ -z "$sc" ]] || grep -qi 'secure' <<<"$sc" || { echo "session cookie is not Secure over https" >&2; return 1; }
    echo "tls: ok (cookie is Secure)"
  fi
}

case "$MODE" in
  verify) verify; exit $? ;;
  status) remote "systemctl --user status kavira --no-pager -l | head -15"; exit 0 ;;
esac

say "probing $TARGET"
# shellcheck disable=SC2016 # the command substitutions must run on the host
read -r OS MACHINE < <(remote 'echo "$(uname -s) $(uname -m)"')
[[ "$OS" == Linux ]] || { echo "unsupported host OS: $OS" >&2; exit 1; }
case "${KAVIRA_REMOTE_ARCH:-$MACHINE}" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "unsupported arch: $MACHINE" >&2; exit 1 ;;
esac

USED="$(remote "df --output=pcent \"\$HOME\" | tail -1 | tr -dc 0-9")"
if (( USED >= MAX_PCT )); then echo "disk ${USED}% >= ${MAX_PCT}%: refusing" >&2; exit 1; fi
if (( USED >= WARN_PCT )); then echo "warning: disk ${USED}% used on $HOST" >&2; fi

if remote "ss -ltn | awk '{print \$4}' | grep -q ':$PORT\$'" && ! remote "systemctl --user is-active --quiet kavira"; then
  echo "port $PORT is already in use by something else; set KAVIRA_REMOTE_PORT" >&2; exit 1
fi

VERSION="$(sed -n 's/^var version = "\(.*\)"$/\1/p' "$ROOT/cmd/kavira/main.go")"
say "building kavira ${VERSION:-dev} linux/$GOARCH"
BIN="$(mktemp -d)/kavira"
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w -X main.version=${VERSION:-dev}" -o "$BIN" ./cmd/kavira)

if [[ "$MODE" == dry-run ]]; then
  say "dry run: would install $(du -h "$BIN" | cut -f1) to $TARGET:~/$DIR/bin/kavira, port $PORT"; exit 0
fi

say "installing to ~/$DIR"
remote "mkdir -p ~/$DIR/bin ~/.config/systemd/user && chmod 700 ~/$DIR"
# The uplink to some hosts is slow, so send a compressed delta against the binary already installed
# (rsync uses the existing destination file as its basis) and swap it in atomically.
remote "cp -f ~/$DIR/bin/kavira ~/$DIR/bin/kavira.new 2>/dev/null || true"
rsync -z --partial -e "ssh ${SSH_OPTS[*]}" "$BIN" "$TARGET:$DIR/bin/kavira.new"
remote "chmod 755 ~/$DIR/bin/kavira.new && mv -f ~/$DIR/bin/kavira.new ~/$DIR/bin/kavira"

# Secrets live on the host. The session key is created once; the admin password is random unless --demo.
if $DEMO; then PWMODE=demo; else PWMODE=random; fi
remote "umask 077; f=\$HOME/$DIR/env; touch \$f
  grep -q '^KAVIRA_SESSION_KEY=' \$f || printf 'KAVIRA_SESSION_KEY=%s\n' \"\$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')\" >> \$f
  if [ $PWMODE = demo ]; then { grep -v '^KAVIRA_ADMIN_PASSWORD=' \$f || true; } > \$f.tmp && mv \$f.tmp \$f
  else grep -q '^KAVIRA_ADMIN_PASSWORD=' \$f || printf 'KAVIRA_ADMIN_PASSWORD=%s\n' \"\$(head -c 24 /dev/urandom | base64 | tr -dc A-Za-z0-9 | head -c 24)\" >> \$f; fi
  chmod 600 \$f"

TLS_ARGS=""
if [[ "$SCHEME" == https ]]; then TLS_ARGS="-tls-self-signed %h/$DIR/tls -tls-host $HOST"; fi

remote "cat > ~/.config/systemd/user/kavira.service" <<UNIT
[Unit]
Description=KAVIRA console (incident compiler)
After=network.target

[Service]
EnvironmentFile=%h/$DIR/env
ExecStart=%h/$DIR/bin/kavira serve -addr 0.0.0.0:$PORT $TLS_ARGS
WorkingDirectory=%h/$DIR
Restart=on-failure
RestartSec=2
NoNewPrivileges=true

[Install]
WantedBy=default.target
UNIT

say "starting service"
remote "systemctl --user daemon-reload && systemctl --user enable kavira >/dev/null 2>&1; systemctl --user restart kavira; loginctl enable-linger \$(id -un) 2>/dev/null || echo 'note: could not enable linger; service stops when the last session logs out'"

for _ in $(seq 1 20); do
  curl -fsSk --max-time 2 -o /dev/null "$SCHEME://$HOST:$PORT/api/v1/health" 2>/dev/null && break
  sleep 1
done
verify
if $DEMO; then
  say "deployed: $SCHEME://$HOST:$PORT   sign in as admin / Admin@321 (demo gate ON: anyone who can reach the port can sign in)"
else
  say "deployed: $SCHEME://$HOST:$PORT   (admin password: ssh $TARGET 'grep ADMIN ~/$DIR/env')"
fi
[[ "$SCHEME" == https ]] && say "the certificate is self-signed: accept the browser warning once"
