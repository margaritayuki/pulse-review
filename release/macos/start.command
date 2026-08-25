#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$ROOT_DIR/pulse-review"
PORT=4567

if [[ ! -f "$BINARY" ]]; then
  osascript -e 'display alert "Pulse Review" message "Файл pulse-review не найден. Полностью распакуйте ZIP-архив и попробуйте снова." as critical'
  exit 1
fi

if [[ -f "$ROOT_DIR/.env.local" ]]; then
  configured_port="$(sed -n 's/^PORT=//p' "$ROOT_DIR/.env.local" | tail -n 1)"
  if [[ "$configured_port" =~ ^[0-9]+$ ]]; then
    PORT="$configured_port"
  fi
fi

chmod +x "$BINARY"
cd "$ROOT_DIR"
"$BINARY" &
SERVER_PID=$!

stop_server() {
  kill "$SERVER_PID" 2>/dev/null || true
}
trap stop_server EXIT INT TERM

for _ in {1..30}; do
  if curl --silent --fail "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then
    open "http://127.0.0.1:$PORT/"
    wait "$SERVER_PID"
    exit $?
  fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    wait "$SERVER_PID"
    exit $?
  fi
  sleep 0.2
done

osascript -e 'display alert "Pulse Review" message "Не удалось открыть локальный сервер. Попробуйте запустить Pulse Review ещё раз." as critical'
exit 1
