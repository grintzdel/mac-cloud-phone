#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "$0")/common.sh"

if pid="$(bridge_pid)"; then
  say "bridge already running (pid $pid)"
  exit 0
fi
mkdir -p run logs build

say "building"
go build -o build/bridge ./cmd/bridge
[[ -x build/CloudPhoneAudio.app/Contents/MacOS/CloudPhoneAudio ]] || scripts/build-audiotap.sh >/dev/null

if ! pgrep -xq BlueStacks; then
  say "launching BlueStacks"
  open -g -j -a BlueStacks
fi

say "waiting for Android on $CLOUDPHONE_SERIAL"
deadline=$((SECONDS + 180))
until adb connect "$CLOUDPHONE_SERIAL" 2>/dev/null | grep -q 'connected to' &&
  [[ "$(adb -s "$CLOUDPHONE_SERIAL" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == 1 ]]; do
  (( SECONDS < deadline )) || die "Android did not come up; is ADB enabled in BlueStacks settings?"
  sleep 2
done

if [[ "$CLOUDPHONE_HIDE_BLUESTACKS" == 1 ]]; then
  osascript -e 'tell application "System Events" to set visible of process "BlueStacks" to false' >/dev/null 2>&1 ||
    say "could not hide the BlueStacks window (grant Automation access to your terminal if you want this)"
fi

android_volume "$CLOUDPHONE_IDLE_VOLUME"
offset=$(( $(wc -l <"$logfile" 2>/dev/null || echo 0) + 1 ))
nohup build/bridge serve \
  -serial "$CLOUDPHONE_SERIAL" \
  -port "$CLOUDPHONE_PORT" \
  -log-level "$CLOUDPHONE_LOG_LEVEL" \
  -max-size "$CLOUDPHONE_MAX_SIZE" \
  -video-bit-rate "$CLOUDPHONE_VIDEO_BIT_RATE" \
  -max-fps "$CLOUDPHONE_MAX_FPS" \
  -audio-bit-rate "$CLOUDPHONE_AUDIO_BIT_RATE" \
  </dev/null >>"$logfile" 2>&1 &
pid=$!
echo "$pid" >"$pidfile"
caffeinate -i -w "$pid" </dev/null >/dev/null 2>&1 &

say "waiting for the audio capture before raising the Android volume"
deadline=$((SECONDS + 60))
until tail -n +"$offset" "$logfile" | grep -q 'audio helper connected'; do
  kill -0 "$pid" 2>/dev/null || die "bridge exited, see $logfile"
  (( SECONDS < deadline )) || die "audio capture did not start (macOS permission prompt?), see $logfile"
  sleep 0.5
done
android_volume "$CLOUDPHONE_PLAY_VOLUME"

ip="$(tailscale ip -4 2>/dev/null | head -1 || true)"
if [[ -z "$ip" ]]; then
  say "bridge running (pid $pid) on http://127.0.0.1:$CLOUDPHONE_PORT/ only: Tailscale is not connected"
  exit 0
fi
url="http://$ip:$CLOUDPHONE_PORT/"
say "bridge running (pid $pid): open $url on the iPhone"
if [[ "$CLOUDPHONE_COPY_URL" == 1 ]]; then
  printf '%s#token=%s' "$url" "$(cat "$HOME/.config/cloudphone/token")" | pbcopy
  say "URL with token copied to the clipboard"
fi
