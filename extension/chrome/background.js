const PROTOCOL = 2;
const ENDPOINT = "ws://127.0.0.1:3333/browser/chrome";
const PING_MS = 20000;
const MAX_BACKOFF_MS = 30000;

let socket = null;
let ready = false;
let backoff = 1000;
let retryTimer = null;
let runPromise = null;

async function loadRun() {
  const { run } = await chrome.storage.session.get("run");
  if (run) return run;
  const fresh = crypto.randomUUID();
  await chrome.storage.session.set({ run: fresh });
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

function connect() {
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return;
  clearTimeout(retryTimer);
  const ws = new WebSocket(ENDPOINT);
  socket = ws;
  ready = false;
  let ping = null;
  ws.onopen = async () => {
    backoff = 1000;
    const run = await currentRun();
    if (socket !== ws || ws.readyState !== WebSocket.OPEN) return;
    ws.send(JSON.stringify({ type: "hello", protocol: PROTOCOL, browser: "chrome", run }));
    ready = true;
    ping = setInterval(() => send({ type: "ping" }), PING_MS);
  };
  ws.onmessage = (e) => handle(JSON.parse(e.data));
  ws.onclose = (e) => {
    clearInterval(ping);
    if (socket !== ws) return;
    socket = null;
    ready = false;
    if (e.reason === "replaced") return;
    retryTimer = setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
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
    const wins = await chrome.windows.getAll({ populate: true, windowTypes: ["normal"] });
    return { windows: wins.map(toWindow) };
  },
  "windows.create": async ({ urls, focused }) =>
    toWindow(await chrome.windows.create({ url: urls, focused: !!focused, type: "normal" })),
  "windows.focus": async ({ windowId }) => {
    await chrome.windows.update(windowId, { focused: true });
    return {};
  },
  "windows.remove": async ({ windowId }) => {
    await chrome.windows.remove(windowId);
    return {};
  },
  "tabs.create": async ({ windowId, url }) => toTab(await chrome.tabs.create({ windowId, url, active: true })),
  "tabs.activate": async ({ tabId }) => {
    await chrome.tabs.update(tabId, { active: true });
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

const normalOnly = { windowTypes: ["normal"] };
chrome.windows.onCreated.addListener((w) => emit("window.created", { windowId: w.id }), normalOnly);
chrome.windows.onRemoved.addListener((id) => emit("window.removed", { windowId: id }), normalOnly);
chrome.windows.onFocusChanged.addListener((id) => emit("window.focused", { windowId: id }), normalOnly);

const tabsChanged = (windowId) => emit("tabs.changed", { windowId });
chrome.tabs.onCreated.addListener((t) => tabsChanged(t.windowId));
chrome.tabs.onRemoved.addListener((_, info) => {
  if (!info.isWindowClosing) tabsChanged(info.windowId);
});
chrome.tabs.onUpdated.addListener((_, change, t) => {
  if (change.url || change.title) tabsChanged(t.windowId);
});
chrome.tabs.onActivated.addListener((info) => tabsChanged(info.windowId));
chrome.tabs.onAttached.addListener((_, info) => tabsChanged(info.newWindowId));
chrome.tabs.onDetached.addListener((_, info) => tabsChanged(info.oldWindowId));
chrome.tabs.onMoved.addListener((_, info) => tabsChanged(info.windowId));

chrome.alarms.create("utena-reconnect", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener((a) => {
  if (a.name === "utena-reconnect") connect();
});
chrome.runtime.onStartup.addListener(connect);
chrome.runtime.onInstalled.addListener(connect);
connect();
