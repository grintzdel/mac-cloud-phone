root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [[ -f config.env ]]; then
  source config.env
else
  source config.env.example
fi

pidfile="$root/run/bridge.pid"
logfile="$root/logs/bridge.log"

say() { printf '\033[1m[cloudphone]\033[0m %s\n' "$*"; }
die() { say "error: $*" >&2; exit 1; }

bridge_pid() {
  [[ -f "$pidfile" ]] || return 1
  local pid
  pid="$(cat "$pidfile")"
  kill -0 "$pid" 2>/dev/null && [[ "$(ps -o comm= -p "$pid")" == *bridge ]] || return 1
  echo "$pid"
}

android_volume() {
  adb -s "$CLOUDPHONE_SERIAL" shell cmd media_session volume --stream 3 --set "$1" >/dev/null 2>&1
}
