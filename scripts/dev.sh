#!/usr/bin/env bash
# Runs the `make dev` servers. See docs/development/developing.md.
#
# Usage: scripts/dev.sh [backend|web]
#
# With no argument, runs both servers and stops the other when either exits. It then exits
# with the failed server's status, or 1 if that server exited cleanly. SIGINT and SIGTERM stop
# both servers and exit 130 or 143. The Makefile sets DEV_WEB_PORT, DEV_BACKEND_PORT, and
# DEV_HOME.
set -euo pipefail

: "${DEV_WEB_PORT:?set by make dev}"
: "${DEV_BACKEND_PORT:?set by make dev}"
: "${DEV_HOME:?set by make dev}"

# Prints the port as a plain decimal number so that equal ports compare equal.
normalize_port() {
  local value=$2
  if ! [[ $value =~ ^[0-9]{1,5}$ ]] || [ "$((10#$value))" -lt 1 ] || [ "$((10#$value))" -gt 65535 ]; then
    echo "dev.sh: $1 must be a TCP port from 1 to 65535, got '$value'" >&2
    exit 2
  fi
  echo "$((10#$value))"
}

DEV_WEB_PORT=$(normalize_port DEV_WEB_PORT "$DEV_WEB_PORT")
DEV_BACKEND_PORT=$(normalize_port DEV_BACKEND_PORT "$DEV_BACKEND_PORT")
if [ "$DEV_WEB_PORT" -eq "$DEV_BACKEND_PORT" ]; then
  echo "dev.sh: DEV_WEB_PORT and DEV_BACKEND_PORT are both $DEV_WEB_PORT; choose two different ports" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
mkdir -p -m 700 tmp
mkdir -p tmp/logs

run_backend() {
  MONEYFLOW_HOME="$DEV_HOME" exec air -c .air.toml -- web --open=false --profile Moneyflow \
    --listen "127.0.0.1:$DEV_BACKEND_PORT" --external-url "http://127.0.0.1:$DEV_WEB_PORT/"
}

run_web() {
  MONEYFLOW_DEV_WEB_PORT="$DEV_WEB_PORT" MONEYFLOW_DEV_BACKEND_PORT="$DEV_BACKEND_PORT" \
    exec bun run --cwd web dev
}

# Leaves the server's own PID, not tee's, in $! so that wait returns the server's exit status.
start() {
  case $1 in
  backend) run_backend ;;
  web) run_web ;;
  esac > >(tee "tmp/logs/$1.log") 2>&1 &
}

is_running() {
  kill -0 "$1" 2>/dev/null
}

# air and bun stop their own children on SIGTERM. air waits up to 2 s for the Go server.
stop() {
  local pid tries=0
  for pid in "$@"; do
    kill -TERM "$pid" 2>/dev/null || true
  done
  for pid in "$@"; do
    while is_running "$pid" && [ "$tries" -lt 50 ]; do
      sleep 0.1
      tries=$((tries + 1))
    done
    if is_running "$pid"; then
      echo "dev.sh: process $pid did not stop after SIGTERM; sending SIGKILL" >&2
      kill -KILL "$pid" 2>/dev/null || true
    fi
  done
}

if [ $# -gt 1 ] || { [ $# -eq 1 ] && [ "$1" != backend ] && [ "$1" != web ]; }; then
  echo "usage: scripts/dev.sh [backend|web]" >&2
  exit 2
fi

# air keeps running when the Go server exits, so a busy backend port would otherwise leave
# Vite serving with no backend.
require_free_port() {
  if (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; then
    echo "dev.sh: port $1 is already in use; stop that process or set $2" >&2
    exit 1
  fi
}

if [ $# -eq 0 ] || [ "$1" = backend ]; then
  require_free_port "$DEV_BACKEND_PORT" DEV_BACKEND_PORT
fi
if [ $# -eq 0 ] || [ "$1" = web ]; then
  require_free_port "$DEV_WEB_PORT" DEV_WEB_PORT
fi

if [ $# -eq 1 ]; then
  start "$1"
  pid=$!
  trap 'stop "$pid"; exit 130' INT
  trap 'stop "$pid"; exit 143' TERM
  status=0
  wait "$pid" || status=$?
  exit "$status"
fi

start backend
backend=$!
start web
web=$!
trap 'stop "$backend" "$web"; exit 130' INT
trap 'stop "$backend" "$web"; exit 143' TERM

while is_running "$backend" && is_running "$web"; do
  sleep 0.2
done
if is_running "$backend"; then
  exited=web
  other=$backend
  pid=$web
else
  exited=backend
  other=$web
  pid=$backend
fi
status=0
wait "$pid" || status=$?
echo "dev.sh: the $exited server exited with status $status; stopping the other server" >&2
stop "$other"
if [ "$status" -eq 0 ]; then
  status=1
fi
exit "$status"
