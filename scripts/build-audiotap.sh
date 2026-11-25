#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
app="$root/build/CloudPhoneAudio.app"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS"
cp "$root/macos/audiotap/Info.plist" "$app/Contents/Info.plist"
swiftc -O "$root/macos/audiotap/main.swift" -o "$app/Contents/MacOS/CloudPhoneAudio"
codesign --force --sign - "$app"
echo "$app"
