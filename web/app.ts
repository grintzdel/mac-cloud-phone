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

let pc: RTCPeerConnection | null = null;
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

video.addEventListener("dblclick", () => {
  statsBox.hidden = !statsBox.hidden;
});

if (new URLSearchParams(location.search).has("stats")) statsBox.hidden = false;
setInterval(() => void updateStats(), 1000);
void connect();
