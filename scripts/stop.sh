#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"

android_volume "$CLOUDPHONE_IDLE_VOLUME" || say "could not lower the Android volume"

if pid="$(bridge_pid)"; then
  say "stopping bridge (pid $pid)"
  kill -TERM "$pid"
  for _ in $(seq 1 50); do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  kill -0 "$pid" 2>/dev/null && kill -KILL "$pid"
else
  say "bridge not running"
fi
rm -f "$pidfile"
pkill -x CloudPhoneAudio 2>/dev/null || true

if [[ "$CLOUDPHONE_QUIT_BLUESTACKS_ON_STOP" == 1 ]]; then
  say "quitting BlueStacks"
  osascript -e 'quit app "BlueStacks"' >/dev/null 2>&1 || true
fi
say "stopped"
