// tend's picker and notes, in every page (docs/BROWSER.md).
//
// Picking: the element under the pointer is outlined and named; a click takes
// it and asks for a note on it; picking goes on until Esc or Done, so several
// can be taken in a row. Each element taken is numbered on the page. A
// floating chat button holds them all with their notes: each can be edited,
// sent to tend on its own or removed, and all of them copied or sent at once.
// Sent means put in tend's context, whose panel opens in tend for them to be
// looked over and handed to an agent.
//
// Everything drawn lives in a shadow root, so the page's styles do not reach
// it and its styles do not reach the page. What was taken is kept by the
// extension per tab and page, so a reload finds it again.
(() => {
  if (window.__tendPicker) return;

  // --- state ---------------------------------------------------------------

  let items = []; // {id, url, title, selector, tag, text, attributes, note, sent}
  // message is what the user wrote in the panel for all of them together:
  // the request they are the context of. It leads a copy and a send of all.
  let message = "";
  let picking = false;
  let panelOpen = false;
  let hover = null;
  let editing = null; // the item whose note is being written after a pick
  let clearArmed = false;
  let nextId = 1;

  const pageKey = () => location.href.split("#")[0];

  function save() {
    chrome.runtime.sendMessage({ type: "tend-save", url: pageKey(), items, message }).catch(() => {});
  }

  // --- drawing --------------------------------------------------------------

  const host = document.createElement("tend-picker");
  host.style.cssText = "all:initial;position:fixed;inset:0;pointer-events:none;z-index:2147483647;";
  const root = host.attachShadow({ mode: "open" });
  root.innerHTML = `
<style>
  :host { all: initial; }
  * { box-sizing: border-box; font: 13px/1.4 ui-sans-serif, system-ui, sans-serif; }
  .mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
  .box { position: fixed; border: 2px solid #7aa2f7; background: rgba(122,162,247,.14); border-radius: 3px; display: none; }
  .label { position: fixed; background: #1a1b26; color: #c0caf5; padding: 2px 6px; border-radius: 3px; display: none;
           max-width: 60vw; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .badge { position: fixed; min-width: 20px; height: 20px; padding: 0 5px; border-radius: 10px; background: #7aa2f7; color: #1a1b26;
           font-weight: 700; font-size: 11px; line-height: 20px; text-align: center; box-shadow: 0 1px 4px rgba(0,0,0,.4); }
  .badge.sent { background: #9ece6a; }
  .ring { position: fixed; border: 2px dashed rgba(122,162,247,.8); border-radius: 3px; }
  .note { position: fixed; display: none; width: 300px; background: #1a1b26; color: #c0caf5; border: 1px solid #7aa2f7; border-radius: 8px;
          padding: 8px; pointer-events: auto; box-shadow: 0 6px 24px rgba(0,0,0,.45); }
  .note .what { color: #7aa2f7; margin-bottom: 6px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  textarea { width: 100%; min-height: 64px; resize: vertical; background: #16161e; color: #c0caf5; border: 1px solid #3b4261;
             border-radius: 6px; padding: 6px; outline: none; }
  textarea:focus { border-color: #7aa2f7; }
  .row { display: flex; gap: 6px; margin-top: 6px; align-items: center; }
  .grow { flex: 1; }
  button { cursor: pointer; border: 1px solid #3b4261; background: #24283b; color: #c0caf5; border-radius: 6px; padding: 4px 9px; }
  button:hover { border-color: #7aa2f7; }
  button.primary { background: #7aa2f7; color: #1a1b26; border-color: #7aa2f7; font-weight: 600; }
  button.danger { color: #f7768e; }
  .hint { color: #565f89; font-size: 11px; }
  .fab { position: fixed; right: 18px; bottom: 18px; width: 48px; height: 48px; border-radius: 24px; background: #7aa2f7;
         color: #1a1b26; display: none; align-items: center; justify-content: center; font-size: 22px; cursor: pointer;
         pointer-events: auto; box-shadow: 0 4px 16px rgba(0,0,0,.4); user-select: none; }
  .fab .count { position: absolute; top: -4px; right: -4px; min-width: 20px; height: 20px; border-radius: 10px; background: #f7768e;
                color: #fff; font-size: 11px; font-weight: 700; line-height: 20px; text-align: center; padding: 0 5px; }
  .done { position: fixed; right: 76px; bottom: 28px; display: none; pointer-events: auto; }
  .panel { position: fixed; right: 18px; bottom: 76px; width: 380px; max-height: min(560px, 70vh); display: none; flex-direction: column;
           background: #1a1b26; color: #c0caf5; border: 1px solid #3b4261; border-radius: 10px; pointer-events: auto;
           box-shadow: 0 10px 32px rgba(0,0,0,.5); overflow: hidden; }
  .head { padding: 10px 12px; border-bottom: 1px solid #3b4261; }
  .title { font-weight: 700; color: #7aa2f7; }
  .list { overflow: auto; padding: 6px 8px 10px; }
  .item { border: 1px solid #292e42; border-radius: 8px; padding: 8px; margin-top: 6px; }
  .item:hover { border-color: #3b4261; }
  .item .top { display: flex; gap: 8px; align-items: baseline; cursor: pointer; }
  .num { color: #1a1b26; background: #7aa2f7; border-radius: 9px; padding: 0 6px; font-weight: 700; font-size: 11px; }
  .num.sent { background: #9ece6a; }
  .sel { color: #bb9af7; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .text { color: #a9b1d6; margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .empty { color: #565f89; padding: 16px 8px; text-align: center; }
  textarea.message { min-height: 44px; margin-top: 8px; }
  .toast { position: fixed; right: 18px; bottom: 76px; background: #1a1b26; color: #c0caf5; padding: 8px 12px; border-radius: 6px;
           border: 1px solid #7aa2f7; display: none; }
</style>
<div class="box"></div><div class="label mono"></div>
<div class="marks"></div>
<div class="note">
  <div class="what mono"></div>
  <textarea placeholder="Add a note for the agent… (Enter saves, Shift+Enter a new line, Esc skips)"></textarea>
  <div class="row"><span class="hint grow">picking goes on — Esc or Done to stop</span>
    <button data-act="skip">Skip</button><button class="primary" data-act="save">Save</button></div>
</div>
<button class="done primary">Done picking</button>
<div class="fab" title="tend: the elements picked and their notes"><span>💬</span><span class="count">0</span></div>
<div class="panel">
  <div class="head">
    <div class="row" style="margin-top:0"><span class="title grow">tend · <span class="n">0</span> elements</span>
      <button data-act="pick">Pick more</button></div>
    <textarea class="message" placeholder="Message for the agent — goes first when you copy or send all…"></textarea>
    <div class="row"><button data-act="copy">Copy all</button><button class="primary" data-act="sendall">Send all to tend</button>
      <span class="grow"></span><button class="danger" data-act="clear">Clear</button></div>
  </div>
  <div class="list"></div>
</div>
<div class="toast"></div>`;

  const $ = (s) => root.querySelector(s);
  const box = $(".box"), label = $(".label"), marks = $(".marks");
  const note = $(".note"), noteWhat = $(".note .what"), noteText = $(".note textarea");
  const doneBtn = $(".done"), fab = $(".fab"), fabCount = $(".fab .count");
  const panel = $(".panel"), list = $(".list"), toast = $(".toast");
  const messageBox = $("textarea.message");
  messageBox.addEventListener("input", () => {
    message = messageBox.value;
    save();
  });

  function mount() {
    if (!host.isConnected) document.documentElement.appendChild(host);
  }

  function say(text, bad) {
    mount();
    toast.textContent = text;
    toast.style.borderColor = bad ? "#f7768e" : "#7aa2f7";
    toast.style.bottom = panelOpen ? `${panel.getBoundingClientRect().height + 90}px` : "76px";
    toast.style.display = "block";
    clearTimeout(say.timer);
    say.timer = setTimeout(() => (toast.style.display = "none"), 2600);
  }

  // --- what an element is -------------------------------------------------

  // selector is a CSS selector for the element: its id when that is unique,
  // otherwise a path of tags, a class or two, and :nth-of-type where needed,
  // from the nearest ancestor with a unique id.
  function selector(el) {
    const unique = (s) => {
      try {
        return document.querySelectorAll(s).length === 1;
      } catch (e) {
        return false;
      }
    };
    if (el.id && unique("#" + CSS.escape(el.id))) return "#" + CSS.escape(el.id);
    const parts = [];
    let node = el;
    while (node && node.nodeType === 1 && node !== document.documentElement) {
      if (node !== el && node.id && unique("#" + CSS.escape(node.id))) {
        parts.unshift("#" + CSS.escape(node.id));
        break;
      }
      let part = node.tagName.toLowerCase();
      const classes = [...node.classList].filter((c) => /^[a-zA-Z_-][\w-]*$/.test(c)).slice(0, 2);
      if (classes.length) part += "." + classes.map((c) => CSS.escape(c)).join(".");
      const parent = node.parentElement;
      if (parent) {
        const same = [...parent.children].filter((c) => c.tagName === node.tagName);
        if (same.length > 1) part += `:nth-of-type(${same.indexOf(node) + 1})`;
      }
      parts.unshift(part);
      const s = parts.join(" > ");
      if (unique(s)) return s;
      node = parent;
    }
    return parts.join(" > ");
  }

  function describe(el) {
    const attributes = {};
    for (const a of [...el.attributes].slice(0, 24)) {
      attributes[a.name] = a.value.length > 200 ? a.value.slice(0, 199) + "…" : a.value;
    }
    let text = (el.innerText || el.value || el.getAttribute("aria-label") || el.getAttribute("alt") || "").trim();
    if (text.length > 2000) text = text.slice(0, 1999) + "…";
    return {
      id: nextId++,
      kind: "element",
      url: location.href,
      title: document.title,
      selector: selector(el),
      tag: el.tagName.toLowerCase(),
      text,
      attributes,
      note: "",
      sent: false,
    };
  }

  const find = (it) => {
    try {
      return document.querySelector(it.selector);
    } catch (e) {
      return null;
    }
  };

  // --- the numbers on the page ------------------------------------------

  let frame = 0;
  function place() {
    frame = 0;
    marks.textContent = "";
    items.forEach((it, i) => {
      const el = find(it);
      if (!el) return;
      const r = el.getBoundingClientRect();
      if (r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth) return;
      const ring = document.createElement("div");
      ring.className = "ring";
      Object.assign(ring.style, { left: r.left + "px", top: r.top + "px", width: r.width + "px", height: r.height + "px" });
      const badge = document.createElement("div");
      badge.className = "badge" + (it.sent ? " sent" : "");
      badge.textContent = String(i + 1);
      Object.assign(badge.style, { left: Math.max(r.left - 10, 2) + "px", top: Math.max(r.top - 10, 2) + "px" });
      marks.append(ring, badge);
    });
    if (editing) {
      const el = find(editing);
      if (el) placeNote(el);
    }
  }
  const replace = () => {
    if (!frame) frame = requestAnimationFrame(place);
  };
  addEventListener("scroll", replace, true);
  addEventListener("resize", replace);

  // --- the floating button and its panel --------------------------------

  function render() {
    mount();
    fabCount.textContent = String(items.length);
    fab.style.display = items.length || picking || message ? "flex" : "none";
    fabCount.style.display = items.length ? "block" : "none";
    doneBtn.style.display = picking ? "block" : "none";
    panel.style.display = panelOpen ? "flex" : "none";
    $(".n").textContent = String(items.length);
    if (messageBox.value !== message) messageBox.value = message;
    $('[data-act="clear"]').textContent = clearArmed ? "Clear all?" : "Clear";
    list.textContent = "";
    if (!items.length) {
      const empty = document.createElement("div");
      empty.className = "empty";
      empty.textContent = "Nothing picked yet. Pick more, then click the elements to write notes on.";
      list.append(empty);
    }
    items.forEach((it, i) => {
      const row = document.createElement("div");
      row.className = "item";
      row.innerHTML = `
        <div class="top"><span class="num"></span><span class="sel mono grow"></span></div>
        <div class="text"></div>
        <textarea placeholder="Note for the agent…"></textarea>
        <div class="row"><span class="hint grow"></span>
          <button data-act="remove" class="danger">Remove</button><button data-act="send" class="primary">Send to tend</button></div>`;
      row.querySelector(".num").textContent = String(i + 1);
      row.querySelector(".num").classList.toggle("sent", it.sent);
      row.querySelector(".sel").textContent = it.selector;
      row.querySelector(".text").textContent = it.text ? `“${it.text.replace(/\s+/g, " ")}”` : `<${it.tag}>`;
      row.querySelector(".hint").textContent = it.sent ? "sent" : "";
      const area = row.querySelector("textarea");
      area.value = it.note;
      area.addEventListener("input", () => {
        it.note = area.value;
        save();
      });
      row.querySelector(".top").addEventListener("click", () => {
        const el = find(it);
        if (el) {
          el.scrollIntoView({ block: "center", behavior: "smooth" });
          flash(el);
        }
      });
      row.querySelector('[data-act="remove"]').addEventListener("click", () => {
        items = items.filter((x) => x !== it);
        save();
        render();
        place();
      });
      row.querySelector('[data-act="send"]').addEventListener("click", () => send([it]));
      list.append(row);
    });
    place();
  }

  function flash(el) {
    const r = el.getBoundingClientRect();
    Object.assign(box.style, { display: "block", left: r.left + "px", top: r.top + "px", width: r.width + "px", height: r.height + "px" });
    setTimeout(() => !picking && (box.style.display = "none"), 900);
  }

  // format is the items as tend hands them to an agent (internal/capture,
  // FormatWith), for Copy all: the same text either way, the message first.
  function format(list, lead) {
    let out = lead && lead.trim() ? lead.trim() + "\n\n" : "";
    out += "Context captured in tend:\n";
    list.forEach((it, i) => {
      out += `\n[${i + 1}] element (from browser)\n`;
      const field = (name, value) => value && (out += `${name}: ${value}\n`);
      field("note", it.note.trim());
      field("title", it.title);
      field("url", it.url);
      field("selector", it.selector);
      field("tag", it.tag);
      const names = Object.keys(it.attributes || {}).sort();
      if (names.length) field("attributes", names.map((n) => `${n}=${JSON.stringify(it.attributes[n])}`).join(" "));
      if (it.text) out += it.text.includes("\n") ? `text:\n${it.text}\n` : `text: ${it.text}\n`;
    });
    return out;
  }

  // commit keeps what is being written when a copy or a send is asked for:
  // a note typed after a pick and not yet saved goes with it.
  function commit() {
    if (editing) finishNote(true);
    message = messageBox.value;
  }

  async function copyAll() {
    commit();
    if (!items.length && !message.trim()) return say("nothing to copy", true);
    const text = format(items, message);
    try {
      await navigator.clipboard.writeText(text);
    } catch (e) {
      const t = document.createElement("textarea");
      t.value = text;
      root.append(t);
      t.select();
      document.execCommand("copy");
      t.remove();
    }
    say(`copied ${items.length} element${items.length === 1 ? "" : "s"} with their notes${message.trim() ? " and your message" : ""}`);
  }

  // send puts items in tend's context; all of them go with the message, one
  // sent on its own without it.
  function send(list, withMessage) {
    commit();
    if (!list.length) return say("nothing to send", true);
    say("sending to tend…");
    chrome.runtime
      .sendMessage({ type: "tend-send", items: list, message: withMessage ? message : "" })
      .catch(() => say("tend is not reachable", true));
  }

  $('[data-act="pick"]').addEventListener("click", () => start());
  $('[data-act="copy"]').addEventListener("click", copyAll);
  $('[data-act="sendall"]').addEventListener("click", () => send(items, true));
  $('[data-act="clear"]').addEventListener("click", () => {
    if (!clearArmed) {
      clearArmed = true;
      render();
      setTimeout(() => {
        clearArmed = false;
        render();
      }, 3000);
      return;
    }
    clearArmed = false;
    items = [];
    save();
    render();
  });
  fab.addEventListener("click", () => {
    panelOpen = !panelOpen;
    render();
  });
  doneBtn.addEventListener("click", () => stop(true));

  // --- picking ---------------------------------------------------------------

  function ours(e) {
    return e.composedPath().includes(host);
  }

  function onMove(e) {
    if (!picking || editing || ours(e)) return;
    const el = e.target;
    if (!(el instanceof Element)) return;
    hover = el;
    const r = el.getBoundingClientRect();
    Object.assign(box.style, { display: "block", left: r.left + "px", top: r.top + "px", width: r.width + "px", height: r.height + "px" });
    label.textContent = selector(el);
    Object.assign(label.style, { display: "block", left: Math.max(r.left, 0) + "px", top: Math.max(r.top - 22, 0) + "px" });
  }

  function onClick(e) {
    if (!picking || ours(e)) return;
    e.preventDefault();
    e.stopPropagation();
    e.stopImmediatePropagation();
    if (editing) return;
    // The element clicked, when the pointer arrived without a move the page
    // saw — a click is enough to pick, whatever was outlined before it.
    const el = e.target instanceof Element ? e.target : hover;
    if (!el) return;
    const it = describe(el);
    items.push(it);
    save();
    render();
    ask(it, el);
  }

  // A press on the page is stopped too while picking, so a link or a
  // button taken is not also followed or pressed.
  function onPress(e) {
    if (!picking || ours(e)) return;
    e.preventDefault();
    e.stopPropagation();
  }

  function placeNote(el) {
    const r = el.getBoundingClientRect();
    const left = Math.min(Math.max(r.left, 8), innerWidth - 316);
    const below = r.bottom + 8 + 150 < innerHeight;
    Object.assign(note.style, { display: "block", left: left + "px", top: (below ? r.bottom + 8 : Math.max(r.top - 158, 8)) + "px" });
  }

  function ask(it, el) {
    editing = it;
    noteWhat.textContent = `${items.indexOf(it) + 1} · ${it.selector}`;
    noteText.value = it.note;
    box.style.display = label.style.display = "none";
    placeNote(el);
    setTimeout(() => noteText.focus(), 0);
  }

  function finishNote(keep) {
    if (!editing) return;
    if (keep) editing.note = noteText.value.trim();
    editing = null;
    note.style.display = "none";
    save();
    render();
  }

  noteText.addEventListener("keydown", (e) => {
    e.stopPropagation();
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      finishNote(true);
    } else if (e.key === "Escape") {
      e.preventDefault();
      finishNote(false);
    }
  });
  $('[data-act="save"]').addEventListener("click", () => finishNote(true));
  $('[data-act="skip"]').addEventListener("click", () => finishNote(false));

  function onKey(e) {
    if (e.key === "Escape" && picking && !editing) {
      e.preventDefault();
      stop(true);
    }
  }

  function start() {
    mount();
    if (!picking) {
      picking = true;
      document.addEventListener("mousemove", onMove, true);
      document.addEventListener("click", onClick, true);
      document.addEventListener("mousedown", onPress, true);
      document.addEventListener("keydown", onKey, true);
    }
    render();
    say("tend: click elements to pick them · Esc or Done stops");
  }

  function stop(open) {
    if (editing) finishNote(true);
    picking = false;
    hover = null;
    box.style.display = label.style.display = "none";
    document.removeEventListener("mousemove", onMove, true);
    document.removeEventListener("click", onClick, true);
    document.removeEventListener("mousedown", onPress, true);
    document.removeEventListener("keydown", onKey, true);
    if (open && items.length) panelOpen = true;
    render();
  }

  // --- from the extension ------------------------------------------------

  chrome.runtime.onMessage.addListener((msg) => {
    if (!msg) return;
    if (msg.type === "tend-select") (msg.on ? start : () => stop(false))();
    if (msg.type === "tend-result") {
      if (msg.ok) {
        for (const it of items) if (msg.ids.includes(it.id)) it.sent = true;
        save();
        render();
        say(`in tend's context — look it over in tend and send it to the agent`);
      } else {
        say("tend: " + msg.error, true);
      }
    }
  });

  // What was picked on this page before a reload.
  chrome.runtime
    .sendMessage({ type: "tend-load", url: pageKey() })
    .then((saved) => {
      // An array is what the first version kept: the items alone.
      const got = Array.isArray(saved) ? { items: saved, message: "" } : saved;
      if (got && (got.items.length || got.message)) {
        items = got.items;
        message = got.message || "";
        nextId = items.length ? Math.max(...items.map((it) => it.id)) + 1 : 1;
        render();
      }
    })
    .catch(() => {});

  window.__tendPicker = { start, stop, items: () => items, copyAll, send: () => send(items, true) };
})();
