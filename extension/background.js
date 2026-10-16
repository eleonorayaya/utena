const PROTOCOL = 2;
const api = globalThis.browser ?? globalThis.chrome;
const BROWSER = location.protocol === "moz-extension:" ? "firefox" : "chrome";
const ENDPOINT = `ws://127.0.0.1:3333/browser/${BROWSER}`;
const PING_MS = 20000;
const MAX_BACKOFF_MS = 30000;
const CONNECT_TIMEOUT_MS = 10000;

let socket = null;
let ready = false;
let backoff = 1000;
let retryTimer = null;
let runPromise = null;
let connectStartedAt = 0;

async function loadRun() {
  if (!api.storage.session) return crypto.randomUUID();
  const { run } = await api.storage.session.get("run");
  if (run) return run;
  const fresh = crypto.randomUUID();
  await api.storage.session.set({ run: fresh });
  return fresh;
}

function currentRun() {
  if (!runPromise) {
    runPromise = loadRun().catch(() => {
      runPromise = null;
      return "";
    });
  }
  return runPromise;
}

function scheduleRetry() {
  clearTimeout(retryTimer);
  retryTimer = setTimeout(connect, backoff);
  backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
}

function abandon(ws) {
  if (socket !== ws) return false;
  socket = null;
  ready = false;
  try {
    ws.close();
  } catch (_) {}
  return true;
}

function stuckConnecting() {
  return !!socket && socket.readyState === WebSocket.CONNECTING && Date.now() - connectStartedAt > CONNECT_TIMEOUT_MS;
}

function connect() {
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return;
  clearTimeout(retryTimer);
  const ws = new WebSocket(ENDPOINT);
  socket = ws;
  ready = false;
  connectStartedAt = Date.now();
  let ping = null;
  const connectTimer = setTimeout(() => {
    if (ws.readyState === WebSocket.CONNECTING && abandon(ws)) scheduleRetry();
  }, CONNECT_TIMEOUT_MS);
  ws.onopen = async () => {
    clearTimeout(connectTimer);
    backoff = 1000;
    const run = await currentRun();
    if (socket !== ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "hello", protocol: PROTOCOL, browser: BROWSER, run }));
    ready = true;
    ping = setInterval(() => send({ type: "ping" }), PING_MS);
  };
  ws.onmessage = (e) => handle(JSON.parse(e.data));
  ws.onclose = (e) => {
    clearTimeout(connectTimer);
    clearInterval(ping);
    if (socket !== ws) return;
    socket = null;
    ready = false;
    if (e.reason === "replaced") return;
    scheduleRetry();
  };
}

function send(msg) {
  if (ready && socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
}

function emit(event, data) {
  send({ type: "event", event, data });
}

function toTab(t) {
  return { id: t.id, url: t.url || t.pendingUrl || "", title: t.title || "", active: !!t.active, pinned: !!t.pinned };
}

function toWindow(w) {
  return { id: w.id, focused: !!w.focused, tabs: (w.tabs || []).map(toTab) };
}

const methods = {
  "windows.list": async () => {
    const wins = await api.windows.getAll({ populate: true, windowTypes: ["normal"] });
    return { windows: wins.map(toWindow) };
  },
  "windows.create": async ({ urls, focused }) =>
    toWindow(await api.windows.create({ url: urls, focused: !!focused, type: "normal" })),
  "windows.focus": async ({ windowId }) => {
    await api.windows.update(windowId, { focused: true });
    return {};
  },
  "windows.remove": async ({ windowId }) => {
    await api.windows.remove(windowId);
    return {};
  },
  "tabs.create": async ({ windowId, url }) => toTab(await api.tabs.create({ windowId, url, active: true })),
  "tabs.activate": async ({ tabId }) => {
    await api.tabs.update(tabId, { active: true });
    return {};
  },
};

async function handle(msg) {
  if (msg.type !== "req") return;
  const fn = methods[msg.method];
  if (!fn) {
    send({ type: "res", id: msg.id, error: `unknown method ${msg.method}` });
    return;
  }
  try {
    send({ type: "res", id: msg.id, result: await fn(msg.params || {}) });
  } catch (err) {
    send({ type: "res", id: msg.id, error: String((err && err.message) || err) });
  }
}

function onNormalWindows(event, listener) {
  try {
    event.addListener(listener, { windowTypes: ["normal"] });
  } catch (_) {
    event.addListener(listener);
  }
}

onNormalWindows(api.windows.onCreated, (w) => emit("window.created", { windowId: w.id }));
onNormalWindows(api.windows.onRemoved, (id) => emit("window.removed", { windowId: id }));
onNormalWindows(api.windows.onFocusChanged, (id) => emit("window.focused", { windowId: id }));

const tabsChanged = (windowId) => emit("tabs.changed", { windowId });
api.tabs.onCreated.addListener((t) => tabsChanged(t.windowId));
api.tabs.onRemoved.addListener((_, info) => {
  if (!info.isWindowClosing) tabsChanged(info.windowId);
});
api.tabs.onUpdated.addListener((_, change, t) => {
  if (change.url || change.title) tabsChanged(t.windowId);
});
api.tabs.onActivated.addListener((info) => tabsChanged(info.windowId));
api.tabs.onAttached.addListener((_, info) => tabsChanged(info.newWindowId));
api.tabs.onDetached.addListener((_, info) => tabsChanged(info.oldWindowId));
api.tabs.onMoved.addListener((_, info) => tabsChanged(info.windowId));

api.alarms.create("utena-reconnect", { periodInMinutes: 0.5 });
api.alarms.onAlarm.addListener((a) => {
  if (a.name !== "utena-reconnect") return;
  if (stuckConnecting()) abandon(socket);
  connect();
});
api.runtime.onStartup.addListener(connect);
api.runtime.onInstalled.addListener(connect);
connect();
