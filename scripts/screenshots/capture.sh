#!/usr/bin/env bash
#
# Regenerates docs/screenshots/ from a throwaway demo instance.
#
# The demo runs on the memory runtime with the fictional containers, groups,
# schedules, homepage entries and actions in scripts/screenshots/fixtures, so a
# capture never shows a real host. Two ports are used (18084/18085 by default);
# the run directory, the binary and the server log live in a temporary
# directory that is removed on exit.
#
# The browser is Playwright's Chromium. It is installed into .build by default,
# which keeps node_modules out of the tree and keeps the application build free
# of a Node toolchain. The first run downloads Chromium (~130 MB).
#
#   ./scripts/screenshots/capture.sh          # or: task screenshots
#   GOSPIN_SHOT_PORT=19084 ./scripts/screenshots/capture.sh
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PORT="${GOSPIN_SHOT_PORT:-18084}"
WAITING_PORT="${GOSPIN_SHOT_WAITING_PORT:-18085}"
DEPS="${GOSPIN_SHOT_DEPS:-$ROOT/.build/screenshot-deps}"
PLAYWRIGHT_VERSION="1.48.2"

WORK="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT

command -v node >/dev/null 2>&1 || {
  echo "error: node is required to drive the browser (https://nodejs.org)" >&2
  exit 1
}
command -v npm >/dev/null 2>&1 || {
  echo "error: npm is required to install Playwright" >&2
  exit 1
}

if [[ ! -x "$DEPS/node_modules/.bin/playwright" ]]; then
  echo "installing playwright@$PLAYWRIGHT_VERSION into $DEPS"
  mkdir -p "$DEPS"
  npm install --prefix "$DEPS" --no-audit --no-fund --loglevel=error "playwright@$PLAYWRIGHT_VERSION"
fi
# Idempotent: downloads Chromium once, then only verifies it is present.
"$DEPS/node_modules/.bin/playwright" install chromium

# The UI uses emoji as icons. Without an emoji font the screenshots keep the
# layout but every icon becomes an empty box, so warn instead of committing them
# silently.
if command -v fc-list >/dev/null 2>&1; then
  emoji_fonts="$(fc-list 2>/dev/null | grep -ci emoji || true)"
  if [[ "$emoji_fonts" -eq 0 ]]; then
    echo "warning: no emoji font is installed, so the UI icons will render as boxes." >&2
    echo "         install one (e.g. fonts-noto-color-emoji) or point FONTCONFIG_FILE at a" >&2
    echo "         fontconfig file that adds it, then run the capture again." >&2
  fi
fi

# A server that already answers on the port would silently be captured instead
# of the demo instance, so refuse instead of taking screenshots of a real one.
if curl -fsS --max-time 1 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
  echo "error: something already serves /health on port $PORT; set GOSPIN_SHOT_PORT to a free port" >&2
  exit 1
fi

echo "building the server"
(cd "$ROOT" && go build -o "$WORK/gospin" ./cmd/server)

# The server resolves ./config/homepage.yaml and ./ui relative to its working
# directory, so the run directory gets the demo config and a link to the real
# UI. That keeps the fixtures out of the repository config/ directory, which a
# real instance uses.
mkdir -p "$WORK/run/config"
cp -R "$ROOT/scripts/screenshots/fixtures/." "$WORK/run/config/"
ln -s "$ROOT/ui" "$WORK/run/ui"

echo "starting the demo server on port $PORT"
(
  cd "$WORK/run"
  GO_SPIN_SERVER_PORT="$PORT" \
    GO_SPIN_SERVER_WAITING_SERVER_PORT="$WAITING_PORT" \
    exec "$WORK/gospin"
) >"$WORK/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "error: the demo server exited during startup:" >&2
    cat "$WORK/server.log" >&2
    exit 1
  fi
  sleep 0.5
done

if ! curl -fsS "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
  echo "error: the demo server did not answer on port $PORT:" >&2
  tail -20 "$WORK/server.log" >&2
  exit 1
fi

GOSPIN_SHOT_BASE="http://demo.local:$PORT" \
  GOSPIN_SHOT_API="http://127.0.0.1:$PORT" \
  GOSPIN_SHOT_DIR="$ROOT/docs/screenshots" \
  NODE_PATH="$DEPS/node_modules" \
  node "$ROOT/scripts/screenshots/capture.js"

echo "screenshots updated in docs/screenshots/"
