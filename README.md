# Mac Cloud Phone

Play an Android game running in BlueStacks Air on the Mac from an iPhone, without disturbing whoever uses the Mac.

## Status

| Phase | State |
|---|---|
| 0. Feasibility | Done: BlueStacks Air (Android 13) runs Love and Deepspace, scrcpy captures video, a Core Audio process tap captures and mutes the audio |
| 1. Raw capture from Go | Done: `bridge capture` writes `video.h264` and `audio.opus` |
| 2. WebRTC streaming | Done: `bridge serve` streams H.264 + Opus to Safari over Tailscale |
| 3. Touch control | Done: multi-touch from Safari over a WebRTC DataChannel |
| 4. Comfort | Done: `start.sh` / `stop.sh`, `config.env`, automatic restarts, caffeinate (gamepad not done) |

## Architecture

```
BlueStacks Air ──adb──▶ scrcpy-server 5.0.1 ──H.264──▶ bridge (Go) ──WebRTC──▶ Safari (iPhone)
       │                                                  ▲
       └── audio ──▶ CloudPhoneAudio.app (Core Audio tap, Opus) ──unix socket──┘
```

scrcpy's own audio capture is refused inside BlueStacks (`AudioRecord` and `registerAudioPolicy` both fail), so the
audio is captured on the Mac side. The tap is created with `muteBehavior = .muted`: BlueStacks stays silent on the Mac
speakers while the tap is running.

## Requirements

Everything below is free.

- Apple Silicon Mac, macOS 14.2 or later
- [BlueStacks Air](https://www.bluestacks.com/mac) with **Settings → Advanced → Android Debug Bridge** enabled
  (listens on `127.0.0.1:5555`)
- `brew install scrcpy` (the bridge checks that `scrcpy-server` is exactly 5.0.1 by SHA-256)
- Go 1.24, Xcode command line tools, TypeScript (`tsc`) only if you change the page
- [Tailscale](https://tailscale.com) on the Mac and the iPhone, signed in to the same account

## Setup

1. Install BlueStacks Air, sign in to Google, install the game from the Play Store and let it download its data.
2. In BlueStacks, enable **Settings → Advanced → Android Debug Bridge**. Leave remote ADB access off.
3. Start Tailscale on the Mac and on the iPhone.
4. Optionally `cp config.env.example config.env` and adjust it.
5. Run `scripts/start.sh`. The first time, macOS asks to let **CloudPhoneAudio** record audio: accept. Rebuilding the
   helper changes its ad-hoc signature, so macOS may ask again.

### On the iPhone

1. Paste the URL that `start.sh` copied to the clipboard (Universal Clipboard) into Safari.
2. For a real full screen, use **Share → Add to Home Screen** with **Open as Web App** on, and launch the game from that
   icon: no Safari bars, in portrait or landscape. The web app has its own storage, so it asks for the token once
   (`pbcopy < ~/.config/cloudphone/token` on the Mac).
3. Turn the orientation lock off and rotate the phone when the game switches to landscape.
4. Tap **« Toucher pour activer le son »**: Safari requires a gesture before playing audio.

## Security

- The bridge listens on `127.0.0.1` and the Tailscale addresses only, and requires the bearer token stored in
  `~/.config/cloudphone/token` (mode 600).
- BlueStacks' ADB port 5555 listens on **all interfaces**, regardless of its "remote access" setting: anyone on the
  local network can get a shell on the Android instance. Turn the macOS firewall on (**System Settings → Network →
  Firewall**).

## Limitations

- One client at a time; no gamepad.
- If the bridge crashes (rather than being stopped with `stop.sh`), BlueStacks plays the game sound on the Mac again.
- `caffeinate` prevents idle sleep only; closing the lid still sleeps the Mac.
- Windows protected with `FLAG_SECURE` (some purchase or login screens) are captured as black.

## Build by hand

```bash
scripts/build-audiotap.sh
tsc -p web
go build -o build/bridge ./cmd/bridge
```

## Daily use

```bash
scripts/start.sh
scripts/stop.sh
```

`start.sh` builds what is missing, launches BlueStacks hidden if needed, waits for Android, starts the bridge in the
background (`logs/bridge.log`, pid in `run/bridge.pid`), keeps the Mac from idle-sleeping with `caffeinate` while the
bridge runs, and copies the iPhone URL with its token to the clipboard. It raises the Android media volume only once
the audio tap is active, and `stop.sh` lowers it before stopping the bridge, so the Mac never plays the game sound.

Settings live in `config.env` (copy `config.env.example`): resolution cap, bit rates, fps, port, volumes, whether to hide
or quit BlueStacks.

The bridge restarts the scrcpy server and the audio helper on its own (1 s, 2 s, 5 s, then every 10 s), without
dropping the iPhone connection; the page reconnects by itself if the bridge restarts. The audio helper exits when the
BlueStacks audio process disappears, so a BlueStacks restart is picked up too.

## Streaming (phase 2)

```bash
build/bridge serve
```

The bridge listens on `127.0.0.1:8080` and on the Mac's Tailscale addresses only (never on the LAN), and its ICE
candidates are restricted to loopback and Tailscale ranges. On the iPhone, with Tailscale connected, open
`http://<mac tailscale ip>:8080/#token=<token>`; the token lives in `~/.config/cloudphone/token` (mode 600) and the page
keeps it in local storage. Add `?stats` to the URL for fps, jitter buffer and RTT.

- One client at a time: a new connection replaces the previous one.
- On connection and on PLI/FIR, the bridge sends scrcpy's `RESET_VIDEO` control message (type 17) to get a key frame
  immediately; SPS/PPS are prepended to every key frame.
- Video RTP timestamps come from the device PTS, since scrcpy only emits frames when the screen changes.
- `web/dist/app.js` is generated by `tsc` and committed so that `go build` works on its own.

## Touch control (phase 3)

The page opens a `control` DataChannel and sends one JSON message per pointer event:
`{"t":"touch","a":"down|move|up|cancel","id":0-9,"x":0..1,"y":0..1}`, with coordinates normalized to the visible video
area (letterboxing excluded). The bridge converts them to pixels using the last video session size and sends scrcpy's
32-byte `INJECT_TOUCH_EVENT` (type 2): the server drops events whose declared size differs from the current video size.
Clipboard sync is disabled and device messages on the control socket are drained.

## Raw capture (phase 1)

```bash
build/bridge capture -duration 15s -out out
ffplay out/video.h264
ffplay out/audio.opus
```

Useful flags: `-serial`, `-max-size`, `-video-bit-rate`, `-max-fps`, `-no-audio`, `-log-level debug`.

Keep the Android media volume at its maximum: the tap captures what BlueStacks outputs, and mutes it on the Mac.

## Protocol notes (scrcpy 5.0.1, checked in the server source)

- Server launched with `CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server 5.0.1 key=value…`;
  the first argument must equal the server version.
- `tunnel_forward=true`: the server listens on `localabstract:scrcpy_<scid as %08x>`, reached through `adb forward`.
  It writes one dummy byte on the first socket, then a 64-byte NUL-padded device name.
- Video stream: 4-byte big-endian codec id (`0` = disabled, `1` = configuration error), then 12-byte headers.
  If bit 63 is set, the header is a session: `u32 flags | u32 width | u32 height` (flag bit 0 = client resize).
  Otherwise it is `u64 pts_and_flags | u32 size` followed by the payload, with bit 62 = config packet and bit 61 = key
  frame. Unlike scrcpy 2.x, the width and height are no longer in the codec header.
- The audio helper uses the same `u64 pts (µs) | u32 size | payload` framing for 20 ms Opus packets.
