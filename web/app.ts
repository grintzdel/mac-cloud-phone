const TOKEN_KEY = "cloudphone.token";
const GATHER_TIMEOUT_MS = 2000;
const RETRY_DELAYS_MS = [1000, 2000, 5000, 10000];

const video = document.getElementById("screen") as HTMLVideoElement;
const login = document.getElementById("login") as HTMLFormElement;
const tokenInput = document.getElementById("token") as HTMLInputElement;
const overlay = document.getElementById("overlay") as HTMLDivElement;
const statusLine = document.getElementById("status") as HTMLDivElement;
const startButton = document.getElementById("start") as HTMLButtonElement;
const statsBox = document.getElementById("stats") as HTMLDivElement;

const MAX_POINTERS = 10;

let pc: RTCPeerConnection | null = null;
let controlChannel: RTCDataChannel | null = null;
const pointerSlots = new Map<number, number>();
let retry = 0;
let retryTimer: number | undefined;

class UnauthorizedError extends Error {}

function readToken(): string | null {
  const fragment = new URLSearchParams(location.hash.slice(1)).get("token");
  if (fragment) {
    localStorage.setItem(TOKEN_KEY, fragment);
    history.replaceState(null, "", location.pathname + location.search);
  }
  return localStorage.getItem(TOKEN_KEY);
}

function showStatus(text: string, withStart = false): void {
  statusLine.textContent = text;
  startButton.hidden = !withStart;
  overlay.hidden = false;
}

function hideOverlay(): void {
  overlay.hidden = true;
}

function waitForGathering(conn: RTCPeerConnection): Promise<void> {
  if (conn.iceGatheringState === "complete") return Promise.resolve();
  return new Promise((resolve) => {
    const done = () => {
      conn.removeEventListener("icegatheringstatechange", check);
      resolve();
    };
    const check = () => {
      if (conn.iceGatheringState === "complete") done();
    };
    conn.addEventListener("icegatheringstatechange", check);
    setTimeout(done, GATHER_TIMEOUT_MS);
  });
}

async function negotiate(conn: RTCPeerConnection, token: string): Promise<RTCSessionDescriptionInit> {
  await conn.setLocalDescription(await conn.createOffer());
  await waitForGathering(conn);
  const response = await fetch("offer", {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify(conn.localDescription),
  });
  if (response.status === 401) throw new UnauthorizedError();
  if (!response.ok) throw new Error(`signaling failed: ${response.status}`);
  return (await response.json()) as RTCSessionDescriptionInit;
}

function lowLatency(receiver: RTCRtpReceiver): void {
  const r = receiver as RTCRtpReceiver & { jitterBufferTarget?: number | null; playoutDelayHint?: number };
  if ("jitterBufferTarget" in r) r.jitterBufferTarget = 0;
  if ("playoutDelayHint" in r) r.playoutDelayHint = 0;
}

function scheduleReconnect(): void {
  clearTimeout(retryTimer);
  const delay = RETRY_DELAYS_MS[Math.min(retry, RETRY_DELAYS_MS.length - 1)];
  retry++;
  showStatus(`Connexion perdue, nouvel essai dans ${delay / 1000} s…`);
  retryTimer = setTimeout(connect, delay);
}

async function connect(): Promise<void> {
  const token = readToken();
  if (!token) {
    overlay.hidden = true;
    login.hidden = false;
    return;
  }
  pc?.close();
  const conn = new RTCPeerConnection();
  pc = conn;
  controlChannel = conn.createDataChannel("control", { ordered: true });
  conn.addTransceiver("video", { direction: "recvonly" });
  conn.addTransceiver("audio", { direction: "recvonly" });

  const stream = new MediaStream();
  conn.ontrack = (event) => {
    lowLatency(event.receiver);
    stream.addTrack(event.track);
    video.srcObject = stream;
  };
  conn.onconnectionstatechange = () => {
    if (conn !== pc) return;
    if (conn.connectionState === "connected") {
      retry = 0;
      if (video.muted) showStatus("Connecté", true);
      else hideOverlay();
      void video.play().catch(() => showStatus("Connecté", true));
    }
    if (conn.connectionState === "failed" || conn.connectionState === "disconnected") scheduleReconnect();
  };

  showStatus("Connexion…");
  try {
    await conn.setRemoteDescription(await negotiate(conn, token));
  } catch (err) {
    if (err instanceof UnauthorizedError) {
      localStorage.removeItem(TOKEN_KEY);
      conn.close();
      overlay.hidden = true;
      login.hidden = false;
      return;
    }
    scheduleReconnect();
  }
}

async function updateStats(): Promise<void> {
  if (!pc || statsBox.hidden) return;
  const lines: string[] = [];
  (await pc.getStats()).forEach((report) => {
    if (report.type === "inbound-rtp" && report.kind === "video") {
      const jitter = report.jitterBufferEmittedCount
        ? (1000 * report.jitterBufferDelay) / report.jitterBufferEmittedCount
        : 0;
      lines.push(`${report.frameWidth ?? "?"}x${report.frameHeight ?? "?"} ${report.framesPerSecond ?? 0} fps`);
      lines.push(`jitter buffer ${jitter.toFixed(0)} ms, lost ${report.packetsLost}`);
    }
    if (report.type === "candidate-pair" && report.nominated && report.currentRoundTripTime !== undefined) {
      lines.push(`rtt ${(report.currentRoundTripTime * 1000).toFixed(0)} ms`);
    }
  });
  statsBox.textContent = lines.join("\n");
}

login.addEventListener("submit", (event) => {
  event.preventDefault();
  localStorage.setItem(TOKEN_KEY, tokenInput.value.trim());
  login.hidden = true;
  void connect();
});

startButton.addEventListener("click", () => {
  video.muted = false;
  void video.play();
  hideOverlay();
});

type TouchAction = "down" | "move" | "up" | "cancel";

function normalized(event: PointerEvent): { x: number; y: number } | null {
  if (!video.videoWidth || !video.videoHeight) return null;
  const rect = video.getBoundingClientRect();
  const scale = Math.min(rect.width / video.videoWidth, rect.height / video.videoHeight);
  const width = video.videoWidth * scale;
  const height = video.videoHeight * scale;
  const x = (event.clientX - rect.left - (rect.width - width) / 2) / width;
  const y = (event.clientY - rect.top - (rect.height - height) / 2) / height;
  return { x, y };
}

function freeSlot(): number | null {
  const used = new Set(pointerSlots.values());
  for (let slot = 0; slot < MAX_POINTERS; slot++) if (!used.has(slot)) return slot;
  return null;
}

function sendTouch(action: TouchAction, slot: number, x: number, y: number): void {
  if (controlChannel?.readyState !== "open") return;
  controlChannel.send(JSON.stringify({ t: "touch", a: action, id: slot, x, y }));
}

function onPointer(action: TouchAction, event: PointerEvent): void {
  event.preventDefault();
  const pos = normalized(event);
  if (!pos) return;
  let slot = pointerSlots.get(event.pointerId);
  if (action === "down") {
    if (slot !== undefined || pos.x < 0 || pos.x > 1 || pos.y < 0 || pos.y > 1) return;
    const free = freeSlot();
    if (free === null) return;
    slot = free;
    pointerSlots.set(event.pointerId, slot);
    video.setPointerCapture(event.pointerId);
  }
  if (slot === undefined) return;
  sendTouch(action, slot, pos.x, pos.y);
  if (action === "up" || action === "cancel") pointerSlots.delete(event.pointerId);
}

function cancelAllPointers(): void {
  for (const slot of pointerSlots.values()) sendTouch("cancel", slot, 0, 0);
  pointerSlots.clear();
}

video.addEventListener("pointerdown", (e) => onPointer("down", e));
video.addEventListener("pointermove", (e) => onPointer("move", e));
video.addEventListener("pointerup", (e) => onPointer("up", e));
video.addEventListener("pointercancel", (e) => onPointer("cancel", e));
video.addEventListener("contextmenu", (e) => e.preventDefault());
document.addEventListener("gesturestart", (e) => e.preventDefault());
document.addEventListener("visibilitychange", () => {
  if (document.hidden) cancelAllPointers();
});

if (new URLSearchParams(location.search).has("stats")) statsBox.hidden = false;
setInterval(() => void updateStats(), 1000);
void connect();
