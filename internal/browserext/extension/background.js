// tend's extension: the browser's end of the session (docs/BROWSER.md).
//
// It talks to tend through the native messaging host dev.tend.browser — tend
// browser bridge, which the browser starts and which relays to the session's
// automation socket. The bridge sends the commands the session gives (open,
// navigate, select); this sends back what the user picked and wrote about.
//
// The elements picked on a page, and the notes on them, are kept here per tab
// and page (chrome.storage.session: gone when the browser closes), so the
// page's picker finds them again after a reload.

const HOST = "dev.tend.browser";

let port = null;
let seq = 0;
// Which tab asked, for each request waiting on its answer.
const pending = new Map();

function connect() {
  if (port) return port;
  try {
    port = chrome.runtime.connectNative(HOST);
  } catch (e) {
    port = null;
    return null;
  }
  port.onMessage.addListener(fromTend);
  port.onDisconnect.addListener(() => {
    port = null;
    chrome.action.setBadgeText({ text: "!" });
    chrome.action.setBadgeBackgroundColor({ color: "#c0392b" });
    // The bridge goes when the session does; try again, so a session
    // started later is found without restarting the browser.
    setTimeout(connect, 5000);
  });
  chrome.action.setBadgeText({ text: "" });
  return port;
}

function tell(tabId, msg) {
  if (tabId !== undefined) chrome.tabs.sendMessage(tabId, msg).catch(() => {});
}

function fromTend(msg) {
  if (!msg || typeof msg !== "object") return;
  if (msg.type === "browser_command") {
    command(msg);
    return;
  }
  if (msg.type === "reply" && pending.has(msg.id)) {
    const { tabId, ids } = pending.get(msg.id);
    pending.delete(msg.id);
    tell(tabId, {
      type: "tend-result",
      ok: !msg.error,
      ids,
      error: msg.error ? String(msg.error.message || msg.error) : "",
    });
  }
}

async function activeTab() {
  const [tab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  return tab;
}

async function command(cmd) {
  switch (cmd.action) {
    case "open":
      await chrome.tabs.create({ url: cmd.url });
      break;
    case "navigate": {
      const tab = await activeTab();
      if (tab) await chrome.tabs.update(tab.id, { url: cmd.url });
      else await chrome.tabs.create({ url: cmd.url });
      break;
    }
    case "select": {
      const tab = await activeTab();
      if (tab) await picking(tab.id, !!cmd.on);
      break;
    }
  }
}

// picking turns the page's picker on or off. The picker is in every page
// already (the manifest's content script); a page open from before the
// extension was, gets it now. A page the browser keeps to itself
// (chrome://, the web store) cannot be scripted, which the badge says.
async function picking(tabId, on) {
  try {
    await chrome.tabs.sendMessage(tabId, { type: "tend-select", on });
  } catch (e) {
    try {
      await chrome.scripting.executeScript({ target: { tabId }, files: ["content.js"] });
      await chrome.tabs.sendMessage(tabId, { type: "tend-select", on });
    } catch (e2) {
      chrome.action.setBadgeText({ tabId, text: "✕" });
    }
  }
}

chrome.action.onClicked.addListener((tab) => {
  if (tab && tab.id !== undefined) picking(tab.id, true);
});

const key = (tabId, url) => `items:${tabId}:${url}`;

chrome.tabs.onRemoved.addListener(async (tabId) => {
  const all = await chrome.storage.session.get(null);
  const gone = Object.keys(all).filter((k) => k.startsWith(`items:${tabId}:`));
  if (gone.length) await chrome.storage.session.remove(gone);
});

chrome.runtime.onMessage.addListener((msg, sender, reply) => {
  if (!msg || !sender.tab) return;
  const tabId = sender.tab.id;
  switch (msg.type) {
    case "tend-load":
      chrome.storage.session.get(key(tabId, msg.url)).then((got) => reply(got[key(tabId, msg.url)] || null));
      return true; // the answer comes later
    case "tend-save":
      chrome.storage.session.set({ [key(tabId, msg.url)]: { items: msg.items, message: msg.message || "" } });
      return;
    case "tend-send": {
      const p = connect();
      if (!p) {
        tell(tabId, { type: "tend-result", ok: false, ids: [], error: "tend is not reachable" });
        return;
      }
      const id = String(++seq);
      pending.set(id, { tabId, ids: msg.items.map((it) => it.id) });
      const items = msg.items.map(({ id, sent, ...it }) => it);
      // Into tend's context, where the user looks them over and sends them on
      // to an agent from the context panel, which opens for them.
      p.postMessage({ id, method: "browser.context", params: { items, message: msg.message || "" } });
      return;
    }
  }
});

connect();
