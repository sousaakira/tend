# tend and a browser

tend does not render pages. A real browser does, and talks to the tend
server, which stands between it and the agents: what the browser picks goes
into the session's context buffer, and from there to whichever agent the
user sends it to. No browser talks to an agent directly.

```
browser + extension ──(bridge)──► tend server ──► context buffer ──► agent pane
                     ◄── commands ──┘
```

What exists today is the server's side and a stand-in for the browser's:
everything below works against `tend browser attach`, which prints the
commands a browser would follow. The extension and the bridge it needs are
the next step (see "Still to build").

## The protocol

Everything is on the session's automation socket (`tend api schema` lists
it), one JSON object a line, as every other method there.

### A browser attaches

```json
{"id":"1","method":"browser.attach","params":{"name":"chrome"}}
```

The reply is `{"type":"browser_attached"}`, and from then on the connection
carries the commands the browser is to follow, one a line, until it hangs up:

```json
{"type":"browser_command","action":"open","url":"https://example.com/login"}
{"type":"browser_command","action":"navigate","url":"https://example.com/login"}
{"type":"browser_command","action":"select","on":true}
```

- `open`: the page in a new tab.
- `navigate`: the tab in front goes to the page.
- `select`: element picking on or off — the page highlights what is under
  the pointer, and a click captures it.

Only `http://` and `https://` pages are ever sent.

### Telling it what to do

Any tool, or a person with `tend browser`, sends these; each answers
`{"type":"browser_sent","browsers":N}`, or the error `no_browser` when none
is attached:

| method | params |
|---|---|
| `browser.open` | `url` |
| `browser.navigate` | `url` |
| `browser.select` | `on` (bool) |
| `browser.status` | — (answers the names of the browsers attached) |

The client's own "open a page" (the toolbar's Browser, prefix+B) sends
`browser.open` first, and opens the page on the user's own machine — in
`[browser] command`, or the desktop's browser — when no browser is attached.

### What it captures

The browser hands back what was picked:

```json
{"id":"2","method":"browser.context","params":{
  "url":"https://example.com/login","title":"Login",
  "selector":"#email","tag":"input","text":"",
  "attributes":{"id":"email","name":"email","type":"email"}}}
```

It goes into the context buffer as an `element` from `browser` (a `kind` of
`url` or `text` can be given instead), where the context panel (prefix+C)
lists it and sends it to an agent. `browser.send_to_agent` takes the same
params and a `pane_id`, and goes straight on: the item is kept, and typed
into that pane — or, with no `pane_id`, the one the user was last in —
without being submitted.

## Trying it without a browser

```bash
tend browser attach -name stand-in      # in one terminal: prints commands
tend browser status                     # stand-in
tend browser open https://example.com   # the stand-in prints it
tend api browser.context '{"url":"https://example.com","selector":"h1","text":"Example Domain"}'
```

## Still to build

- **The extension** (`tend-browser-extension`): highlights the element under
  the pointer while `select` is on, builds a selector for the one clicked,
  and sends `browser.context` with its tag, text and attributes; follows
  `open` and `navigate`.
- **The bridge**: an extension cannot open a Unix socket. Chromium-family
  browsers and Firefox both give an extension native messaging instead — a
  program the browser starts, speaking length-prefixed JSON on its standard
  streams — so a `tend browser bridge` would be that program, relaying
  between the extension and `browser.attach` / `browser.context` on the
  socket, plus the host manifest that tells the browser where it is.
- **The TEND Browser**: `[browser] command` is where a browser started with
  its own profile and the extension loaded goes, so pages tend opens land in
  one that can pick elements.
