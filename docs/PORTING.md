# Porting herdr to tend — status and work queue

tend is a 1:1 port of herdr (Rust, at `../herdr`) to Go. This file is the record
of what has crossed over, what has not, and where each missing piece lives in
herdr's source. Read `AGENTS.md` first for how to work.

**Keep this file true.** When you port something, move it from "Not ported" to
"Ported" in the same commit. When you diverge on purpose, record it under
"Different from herdr on purpose". A queue that lies is worse than none.

Sizes: herdr is about 258k lines of Rust; tend is about 18k lines of Go plus 11k
of tests. herdr's API has about 110 methods; tend's protocol has 18.

---

## How to port a feature

1. **Locate it in herdr.** Use the map below, then `grep -rn` in `../herdr/src`.
   herdr's user docs are in `../herdr/docs/next/website/src/content/docs/` and
   say what a feature is *for*, which the code often does not.
2. **Read the code, and note what it does not do.** Absences are the easy thing
   to miss and the expensive thing to get wrong.
3. **Classify the state** the feature needs: a shared session fact goes in
   `internal/session` and over the wire; what one viewer is looking at stays in
   `cmd/tend`. See `AGENTS.md`, "Architecture boundaries".
4. **Extend the protocol if needed.** A new method: add the constant, the params,
   the dispatch case in `internal/server/serve.go`, the entry in `server.Methods`,
   the entry in `proto.KnownMethods`, and a client wrapper. A new event or
   snapshot field: add it to `proto.KnownFeatures`.
5. **Write the test with the feature**, end-to-end in `cmd/tend` when it is
   something a user does.
6. **Verify with the real program**, not a stand-in, when the feature is about
   how agents behave.
7. `make check`, read the result, then commit. Update this file in that commit.

### Where herdr's code maps to

| herdr (`../herdr/src/`) | tend | notes |
|---|---|---|
| `ghostty/`, `terminal/`, `pane/terminal.rs` | `internal/vt` | herdr binds libghostty (C); tend's emulator is pure Go, written from scratch |
| `detect/`, `detect/manifests/` | `internal/detect`, `internal/agent` | manifests are byte-identical; `dialect.go` translates Rust regex syntax |
| `pty/` | `internal/pty` | |
| `workspace.rs`, `workspace/`, `layout.rs`, `session.rs` | `internal/session` | |
| `server/` | `internal/server` | |
| `protocol/`, `api/schema*` | `internal/proto` | tend's wire is much smaller |
| `client/` (the transport half) | `internal/client`, `internal/transport` | |
| `client/shell/` (the TUI) | `cmd/tend/tui*.go` | the largest gap is here |
| `ui/` | `internal/ui` | |
| `config/` | `internal/config` | |
| `cli/` | `cmd/tend/*.go` | |
| `selection.rs`, `client/shell/mouse.rs` | `internal/ui/selection.go`, `cmd/tend/tui_select.go`, `tui_view.go` | |
| `remote/` | `internal/transport/remote.go`, `cmd/tend/bridge.go` | |

---

## Ported, and checked

- **Terminal emulator** (`internal/vt`): DEC ANSI parser, grid with scrollback
  ring, reflow on width change, alternate screen, wide characters, SGR, mouse
  modes, OSC 0/2 (title), OSC 9 (progress), OSC 52 (clipboard write).
- **Agent detection**: all 22 manifests, identical to herdr's byte for byte
  (check with `diff -rq ../herdr/src/detect/manifests internal/detect/manifests`).
  Agents started inside a shell are adopted from the pty's foreground process.
- **Session structure**: spaces (herdr: workspaces), tabs, panes; split, close,
  zoom, resize by key and by dragging a divider, directional focus.
- **Server and client**: detached daemon on a Unix socket, started on demand by
  a bare `tend`; reconnect; version/feature mismatch notice with in-place
  server restart.
- **Sidebar**: spaces as a folding tree of groups, git branch and ahead/behind
  per space, agents list flat or grouped, draggable divider between the two
  lists, per-list scrolling with pinned headings, collapse/expand handles, a
  session-wide "N waiting" count.
- **Mouse**: clickable tabs and sidebar, context menus with hover highlight,
  rename in a modal, wheel scrolling.
- **Selection and copy**, on herdr's model: a program that asks for the mouse is
  given the whole gesture in its own coordinates and its OSC 52 copy reaches the
  clipboard (verified against the real Claude Code v2.1.278); in a plain pane
  tend selects, scrolling its own history under the drag; alt selects a block.
- **Remote sessions over ssh**: `tend attach -ssh user@host`, via `tend bridge`
  on the far side. Tested with a stand-in for ssh; not yet against a real sshd.
- **Shell completions**: bash, zsh, fish (`tend completion <shell>`).
- **Session persistence**: the arrangement is written to
  `$XDG_STATE_HOME/tend/<session>.json` (structure, checked every second) and
  `<session>.history.json` (scrollback, every 30s and at shutdown), and read
  back when a server starts. Spaces, groups, tabs, the split tree, each pane's
  *current* directory and its scrollback come back; programs do not — a
  restored pane is a new process. `[server] persist = false` turns it off.
  herdr's equivalent is `persist/`. Not ported from it: agent session resume
  (queue item 12).
- **Live handoff**: `tend handoff` (herdr: `server live-handoff`), and `r` on
  the mismatch notice when the server offers it. The server stops every pane's
  reader, writes a manifest (the session snapshot, plus per pane its pid, size
  and a `vt.RenderResume` blob that rebuilds scrollback, both screens, modes and
  cursor), starts the binary now on disk as `serve -inherit`, and waits for it
  to say it holds everything; then it lets go of the terminals without hanging
  them up and exits. The programs keep their pids. A replacement that fails to
  start or to answer is killed and the old server reads on, having lost
  nothing, since nobody read the terminals in between. Verified with the real
  Claude Code in a pane: same pid, same screen, detection still reporting its
  state afterwards. Also run on the owner's live session with Claude Code
  mid-session: the new server reported the pane's mouse modes (any-event, SGR)
  exactly as the old one had, which only `RenderResume` could have told it.
- **Automation API and CLI** (a large part of herdr's item 3): the socket
  answers `session.snapshot`, `workspace.list|get|create|rename|close`,
  `tab.list|get|create|rename|close`, `pane.list|get|read|send_text|send_keys|
  split|close|resize|wait_for_output`, `agent.list|get|read|prompt|send_keys|
  wait|start`, `events.wait`, `server.stop|live_handoff`, in herdr's names and
  shapes. The commands are `tend agent <list|get|read|prompt|send-keys|wait>`,
  `tend pane <list|get|read|send-text|send-keys|wait>` and `tend api <method>
  [json]` for anything without a command of its own. Keys are named as herdr
  names them (`internal/vt/keys.go`), encoded against the pane's own modes, and
  a prompt's text and its Enter are sent 300ms apart as herdr does. Verified
  against the real Claude Code with no client attached: prompted it, waited for
  it to come back idle, and read its answer out of the pane.
- **Plugin host** (the core of herdr's item 4): a plugin is a directory with a
  `tend-plugin.toml` — herdr's manifest with the name changed — declaring
  `build`, `startup`, `actions`, `events` and `panes`. `tend plugin
  list|link|unlink|enable|disable|reload|actions|run|open`, and over the socket
  `plugin.list|link|unlink|enable|disable|reload|action.list|action.invoke|
  pane.open`. The registry is `plugins.json` beside the settings file; linking
  records the directory rather than copying it, as herdr does. Commands run
  from the plugin's root with `TEND_PLUGIN_*` and the socket in their
  environment, in a process group of their own, killed after 30s. Verified end
  to end with a plugin written for the test (hook, action, pane); **not**
  verified with the owner's `herdr-sidebar`, which needs more than the host.
- **Copy mode** (herdr's `client/shell/copy_mode.rs`): `ctrl+b [` enters it,
  as in tmux and herdr — it replaces the plain scroll view, which it is with a
  cursor added. vi motions (`h j k l w b e W B E 0 ^ $ { } g G`, pages with
  ctrl+b/f/u/d), `v`/space and `V` to select, `y`/enter to copy (the cursor's
  line when nothing is selected), `/` `?` `n` `N` to search, escape clears a
  selection and then leaves. Motions and search run on the server
  (`pane.copy_motion`, `pane.copy_search`) with herdr's word classes and
  separator set; search is literal, smart-case, and matches across wrapped rows.
  Verified inside the real Claude Code: searched, selected and copied its text.
- **Worktrees** (herdr's `worktree.rs` and `app/api/worktrees.rs`): `tend
  worktree list|create|open|remove` and `worktree.list|create|open|remove`
  over the socket. A new worktree goes to `[worktrees] directory`
  (`~/.tend/worktrees`, herdr's is `~/.herdr/worktrees`) as
  `<repo>/<branch-slug>`; a branch nobody named gets herdr's generated
  `worktree/<adjective>-<noun>-<hex>`; an existing branch is checked out rather
  than recreated. It opens as a space rooted in the checkout and filed in a
  group named after the repository — herdr's "membership", which tend's groups
  already are. Removing follows herdr's order: forced, the space closes first;
  not forced, git is asked first and a dirty worktree keeps its space.
  Verified with real git end to end.
- **Moving and swapping** (herdr's `layout.rs` swap_panes, `workspace.rs`
  move_tab, `app/actions.rs` move_workspace): `ctrl+b H/J/K/L` swaps the focused
  pane with its neighbour — herdr's binding — keeping the split shape and its
  ratios; tabs and spaces move from their right-click menus; `pane.swap`,
  `tab.move`, `workspace.move` on both sockets. Resizing moved to herdr's
  resize mode, `ctrl+b r`, then h/j/k/l until escape; redraw moved to
  `ctrl+b R`. A new `session-changed` event (feature `session-changed`) tells
  other clients about swaps, moves, renames and regrouping, which before this
  a second client did not see until something unrelated made it look again.
- **Navigation keys**: `ctrl+b tab` / `shift+tab` cycle the tab's panes
  (herdr's `CyclePaneNext|Previous`), `ctrl+b ;` goes back to the pane focused
  before, `ctrl+b < >` step through the agents in sidebar order wherever they
  are, and `ctrl+b w` opens the space picker beside the `g` tend already had.
  herdr leaves `LastPane`, `PreviousAgent` and `NextAgent` unbound by default;
  `;`, `<` and `>` are tend's choice.
- **Notifications and sound** (herdr's `terminal_notify.rs`, `sound.rs`,
  `app/actions.rs` toast rules): an agent that becomes blocked "needs
  attention", one that was working and went idle "finished", and nothing else
  is said — herdr's rule. `[notify] toasts` is `"tend"` (status line, the
  default), `"terminal"` (OSC 9, or kitty's OSC 99, wrapped for tmux, with the
  same backend detection and text sanitising as herdr) or `"off"`; the focused
  pane says nothing unless `focused = true`. `[sound]` plays a file through
  paplay/aplay/afplay/ffplay/play, or rings the terminal bell when none is
  named.
- **Settings, live**: `ctrl+b s` opens a settings screen (herdr's key) that
  changes the file and applies it at once; `ctrl+b R` re-reads the file — the
  client applies its half, `server.reload_config` makes the server re-read
  its own (detection interval, scrollback for new panes). An edit keeps the
  file the user wrote: one line replaced in place, comments and order intact,
  refused outright if the result would not parse (herdr's
  `config/io.rs::upsert_section_raw`).
- **Rebindable keys** (herdr's `config/keybinds.rs`): `[keys.bind]` maps a
  command to a key — `detach = "q"` — over the defaults, `tend keys` lists
  every command and the key it is on, and the help shows the keys in effect
  rather than the defaults. Binding a key something else has is allowed and
  reported. One table feeds the parser, the help and the listing, so they
  cannot drift.
- **Agent session resume** (herdr's `agent_resume.rs`): when a hook has said
  which conversation an agent is in, that reference is written into the state
  file, and a restored pane starts with the flag that continues it —
  `claude --resume <id>`, `codex resume <id>`, `omp --resume=<path>`, one per
  agent, herdr's table. Only the integration tend ships for that agent may
  name one, and the reference is refused if it is empty, has control
  characters, or is a path where an id belongs: it goes onto a command line.
- **Kitty graphics** (herdr's `kitty_graphics.rs`): `internal/vt` keeps what a
  pane's program transmitted and where it placed it — direct transfers of RGB,
  RGBA or PNG, in chunks, placed at the cursor in absolute rows so an image
  travels with its text — and the client draws the visible ones on its own
  terminal after each paint, with ids of its own, removing what scrolled away.
  Kitty, Ghostty and WezTerm get images; other terminals get nothing, which is
  what they would have shown anyway (`TEND_GRAPHICS=off` turns it off). A file
  transfer (`t=f`) is refused: the path would be chosen by whatever is running
  in the pane.
- **Updater** (herdr's `update.rs`): `tend update [-check]` reads the manifest
  for the configured channel — herdr's shape, version, notes, assets and
  checksums by platform — downloads this platform's asset beside the binary it
  will replace, refuses it if the checksum does not match, and installs it by
  rename, keeping the old one until the new is in place. `tend channel
  [stable|preview]` shows or sets the channel. **There is no default manifest
  URL, and nothing checks or downloads on its own**: tend publishes no
  releases, and pointing an updater at a guess would install somebody else's
  binary. Tested against a local HTTP server.
- **Settings file** with validation, `tend config`.
- **Agent hooks**: `tend integration install|uninstall|status`, the automation
  socket methods `integration.*` and `pane.report_*`, and Unix assets for every
  herdr target. An installed hook reports over `$TEND_SOCKET_PATH` so the
  arbiter can prefer the agent's own account over screen detection.

## Different from herdr on purpose

| | herdr | tend | why |
|---|---|---|---|
| Terminal core | libghostty-vt via FFI | pure Go | owner's decision; no cgo |
| Agent state | screen detection **and** hooks installed into each agent | screen detection **and** hooks (`tend integration install`); arbitration + automation socket | — |
| Forced selection | none inside a mouse-holding program | alt+drag selects a block anywhere | fallback for programs that hold the mouse and do nothing with a drag |
| Clipboard | OSC 52 only | local tool (`wl-copy`/`xclip`/`xsel`/`pbcopy`) when not over ssh, plus OSC 52 always | the owner's terminal refuses OSC 52 |
| Handoff transport | pty descriptors sent as `SCM_RIGHTS` over a socket; the new server binds the socket afresh | descriptors inherited by the child (`exec.Cmd.ExtraFiles`) at fixed numbers, the **listening socket included** | inheritance needs no protocol, and handing the listener over means the socket file is never removed and recreated — there is no instant with nobody listening |
| Focus and scroll over the API | `pane.focus`, `agent.focus`, `pane.scroll`, `pane.current` are server methods | not offered | in tend these are client state (AGENTS.md: what one person is looking at stays in the client), so the server has no focus to set and would be answering for a client that may not be attached |
| Names | workspace | space (in the UI; `workspace` in code and on the wire) | matches herdr's own UI wording |
| Claude / JSONC settings | `jsonc_parser` preserves comments and compact layout | `encoding/json`; comments lost and **keys re-sorted alphabetically** on rewrite (content otherwise identical — checked against the owner's real 44 KB `settings.json`: install adds one `SessionStart` entry, a second install adds nothing, uninstall restores it exactly) | avoid a new dependency; invalid JSON is an error, not silently stripped |
| Integration assets | `.sh` and `.ps1` | Unix `.sh` / `.js` / `.ts` / Hermes plugin only | Windows PowerShell assets not ported yet; platform code is compile-gated when they are |

---

## Not ported — the work queue

Ordered by how much each changes daily use. Sizes are herdr's, as a guide to
effort, not a target.

### 1. Live handoff — what is left of it

The handoff itself is ported (see "Ported, and checked"). What herdr builds on
top of it is not:

- **Handoff as part of updating.** herdr's updater installs and then hands off
  (`update.rs`, `--handoff`). tend has no updater yet — queue item 14 — so the
  owner runs `make install` and then `tend handoff`.
- **Handoff on remote attach** (`remote/attach.rs`, `remote/restart_policy.rs`):
  replacing an outdated server on the far side of `-ssh` before attaching.
- A server from before this feature cannot hand off — it has no such method —
  and is replaced only by a restart. `tend handoff` says so rather than doing it.
- Known limit: a pane resized between the manifest being written and the
  replacement taking over keeps the old size until the client next sends one,
  which it does on reconnect.

### 2. Agent integrations (hooks) — mostly done

herdr installs hooks into each agent so the agent reports its own state, which
is more reliable than reading the screen.

**Ported in tend:**
- Arbitration (`internal/agent` Arbiter) and server report methods
- JSON automation socket (`internal/api`) at `$TEND_RUNTIME_DIR/api/<session>.sock`
- Pane env (`TEND_ENV`, `TEND_SOCKET_PATH`, `TEND_PANE_ID`, `TEND_BIN_PATH`)
- Handing that socket across `tend handoff`
- `tend integration install|uninstall|status` and API
  `integration.list|install|uninstall`
- Unix assets and installers for every herdr target (claude, codex, cursor,
  copilot, devin, droid, kimi, pi, omp, opencode, kilo, hermes, qodercli,
  qwen, letta, mastracode, antigravity-cli, grok)

**Verified, and how:** every asset is herdr's with the name translated (diffed
file by file; three differ only in the capital T). The Claude hook script was
run in a pane of a live server with a `SessionStart` payload in Claude's format
and the session came back over `pane.list` as `agent_session`. **Not verified:**
any hook fired by a real agent — that needs the integration installed in the
owner's real agent config, which is theirs to do (`tend integration install
claude`). Session references follow herdr's rule: only the official source for
an agent may name one, by id, except pi and omp which resume from a path.

**Still not ported:**
- The arbiter leaves out herdr's bookkeeping for suppressed and stale
  full-lifecycle sessions, its window after an observed process exit, agent
  names, and `pane.report_metadata` (`terminal/state.rs`, `terminal/metadata.rs`)
- The session reference is held in memory only: it is not in the state file or
  the handoff manifest yet, which queue item 12 needs
- Windows `.ps1` assets and Windows path branches
- Comment-preserving JSONC rewrites (see divergence table)
- herdr's kimi min-version gate (`enforce_agent_version`) and a few
  opencode/hermes edge cases around validity checks for "outdated"
- Note the versioning rule in `../herdr/CLAUDE.md` ("Integration asset
  versions") when bumping `TEND_INTEGRATION_VERSION` markers

### 3. Automation API and CLI — the core is done, the rest is open

What a script needs is ported (see "Ported, and checked"). What is left:

- **Streaming**: `events.subscribe` as a stream. `events.wait` returns one
  event; a caller that wants a feed calls again, which drops what happened in
  between. herdr: `api/subscriptions.rs` (842 lines).
- **Focus and scroll** (`pane.focus`, `agent.focus`, `pane.scroll`,
  `pane.current`): deliberately absent, see "Different from herdr on purpose".
- `layout.export|apply`, `pane.move|swap|neighbor|edges|zoom|process_info`,
  `agent.explain|rename|view.*`, `worktree.*`, `plugin.*`, `command.invoke`,
  `notification.show`, graphics, `pane.report_metadata`.
- A published schema (`herdr api schema`, `schemars`), which plugins read.
- herdr: `api/schema*` (9.4k), `cli/agent.rs`, `cli/pane.rs`, `cli/tab.rs`,
  `cli/workspace.rs`, `cli/api.rs`; user docs `socket-api.mdx`,
  `cli-reference.mdx`, `agent-automation.mdx`.

### 4. Plugins — the host is done; what the sidebar plugin needs is not

The host is ported (see "Ported, and checked"). Left:

- **Events herdr has and tend does not**: `pane.focused`, `tab.focused`,
  `tab.created`, `workspace.created`, `workspace.focused`. Focus is client
  state in tend, so the focus events need the client to report it; the
  `created` ones are a server change. A manifest naming them links with a
  warning and those hooks never run.
- **The owner's `herdr-sidebar`** (`~/.config/herdr/plugins/github/`) calls
  `pane.focus`, `pane.swap`, the focus events above and herdr's CLI by name.
  Running it needs those, plus its manifest renamed to `tend-plugin.toml` and
  its `herdr` calls pointed at `tend`. It is a separate piece of work.
- `build` steps are parsed and not run: nothing yet decides when to build.
  herdr builds on install.
- Installing from a URL or GitHub (`plugin install`), the marketplace, popups
  as a placement, `link_handlers`, `plugin.log.list`, `min_herdr_version`
  enforcement (tend's builds have no ordering to compare against).
- herdr: `plugin_command.rs`, `plugin_paths.rs`, `cli/plugin.rs`,
  `persist/plugin_registry.rs`, `app/api/plugins/`, `api/schema/plugins.rs`;
  user docs `plugins.mdx`, `marketplace.mdx`.

### 5. Copy mode and scrollback tools — copy mode done, the rest is open

Copy mode is ported (see "Ported, and checked"). Left:

- `EditScrollback`: the pane's history opened in `$EDITOR`
  (`server/client_commands.rs`).
- Double-click word selection with the mouse
  (`client/shell/word_selection.rs`, 217 lines) — the word rule it needs is
  now in `internal/copymode`.
- herdr refuses a motion when the pane's content changed since the client last
  looked (`stale_content`). tend does not: a pane printing while copy mode is
  up can shift the rows under the cursor.
- Highlighting every match while searching; herdr keeps a window of them.

### 6. Worktrees — done over the CLI and socket; no overlay yet

Ported (see "Ported, and checked"). Left:

- The overlays (`client/shell/worktrees.rs`, `worktree_overlays.rs`): picking
  and creating worktrees from inside the client. Today it is the command line.
- A forced removal that git then refuses leaves the space closed. herdr
  restores the panes it shut down (`restore_shutdown_worktree_panes`).
- `trust_repository` (`safe.directory`) for repositories owned by another user.
- The `worktree.*` events.

### 7. Moving and swapping — done, except moving a pane elsewhere

Ported (see "Ported, and checked"). Left:

- `pane.move`: a pane into another tab or space. The swap stays within a tab.
- `workspace.move_block`: moving a group of spaces as one.
- Dragging a tab or a space with the mouse.
- **The rest of herdr's default keys.** tend's prefix keys are tmux's where
  herdr's differ: herdr detaches on `q` (tend `d`), renames tabs on `shift+t`
  and spaces on `shift+w` (tend `,` and `.`), splits on `v` and `-`, cycles
  panes on `tab`, toggles the sidebar on `b`, reloads the config on `shift+r`.
  Moved to herdr's so far: `H J K L` (swap), `r` (resize mode), `s`
  (settings), `N` (new space), `G` (new worktree), `R` (reload). Still tmux's:
  `d` detach (herdr `q`), `,` and `.` rename (herdr `shift+t`, `shift+w`),
  `|` and `-` split (herdr `v` and `-`), `a` agents (herdr `b` sidebar).

### 8. Navigation extras — done, except what needs multiple machines

Ported (see "Ported, and checked"). Left:

- `OpenNavigator` / `aggregate_navigation.rs` (439 lines): one navigator over
  several servers at once. herdr's client attaches to many endpoints; tend's
  attaches to one, so this needs multi-endpoint first.
- `FocusAgent(index)`: jump to the nth agent. herdr binds no key to it either.
- Filtering by typing in the picker; tend's walks the list.

### 9. Notifications and sound — done, minus the parts that need assets

Ported (see "Ported, and checked"). Left, and deliberately:

- **Bundled sounds.** herdr ships two mp3s and decodes them itself (`sound.rs`
  is 482 lines mostly for that). tend carries no assets, so a sound is a file
  the user names and the bell otherwise. Changing this means bundling audio.
- **System notifications** (herdr's `ToastDelivery::System`): a desktop
  notification through the OS rather than the terminal.
- Per-agent sound overrides (`[sound.agents]`), notification queueing and
  dismissal, `notification.show` over the API, and `OpenNotificationTarget`
  (a key that jumps to whatever the last notification was about).
- herdr re-checks a "finished" notification against a later snapshot before
  showing it (`notification_policy.rs`); tend uses a cooldown instead, which
  is written down beside the rule.

### 10. Settings UI, onboarding, live reload — screen and reload done

Ported (see "Ported, and checked"). Left:

- **Onboarding** (`ui/onboarding.rs`) and release notes
  (`ui/release_notes.rs`): what herdr shows on a first run and after an
  update. tend has no updater yet (item 14), and nothing to announce.
- **Themes in the screen**: herdr ships named themes (`THEME_NAMES`) and
  previews them as you move; tend has five colours in `[ui.theme]` and no
  names to pick from. Named themes are their own piece of work.
- **Integrations in the screen**: herdr's settings has a section that installs
  them; tend has `tend integration install`.
- Live reload of the prefix key is applied, but a client started with one
  prefix keeps any pane input already bound elsewhere; herdr rebinds
  everything through its keybind table (item 11).

### 11. Configurable chrome — keys done, the rest of the chrome is not

Keys are rebindable (see "Ported, and checked"). Left:

- **Sidebar rows as tokens** (`config/sidebar.rs`, 729 lines, and
  `ui/sidebar/tokens.rs`): herdr lets the user say what each row shows and in
  what style. tend's rows are fixed.
- **Tab bar** (`config/tab_bar.rs`) and **window title**
  (`config/window_title.rs`): what goes in them, as templates.
- **Named themes** (`config/theme.rs`): herdr ships a set and names them; tend
  has five colours set individually.
- A key is one byte or one escape sequence after the prefix, so `ctrl+q` and
  `alt+x` cannot be bound: the terminal sends control bytes tend forwards to
  the pane. herdr reads key events with modifiers through crossterm.
- herdr's remaining default keys, listed under item 7.

### 12. Agent session resume — done for a restored pane

Ported (see "Ported, and checked"). Left:

- herdr's deduplication (`dedupe_key`): two panes restored into the same
  conversation. tend restores each pane with what its own hook reported.
- Resuming from the launch command (`persisted_session_from_launch_args`):
  herdr notices `codex resume <id>` typed by hand and remembers it.
- `agent.start` does not take a session to resume; a script that wants one
  passes the flag itself.

### 13. Kitty graphics — the path works; the hard parts of herdr's are not here

Ported (see "Ported, and checked"). Left:

- **Unicode placeholders**, animation, `a=q` queries, cropping (`x y w h`) and
  cell-precise offsets: parsed and ignored rather than honoured.
- **Pane layers and client surfaces** (`plugin`-drawn images, `pane.graphics.*`
  over the API, `api/server/pane_graphics_stream.rs`, 1115 lines): a plugin
  cannot draw an image into a pane.
- **Sizing against the cell size**: herdr asks the terminal how big a cell is
  and scales; tend passes on the sender's `c`/`r` and leaves the rest to the
  terminal.
- Images are fetched on the paint goroutine when a pane's revision moves. A
  slow server would show as a slow redraw for that frame; herdr streams them.
- Verified with a stand-in program, not with a real image viewer.

### 14. Updater with channels — the mechanism is done; publishing is not

Ported (see "Ported, and checked"). What is left is the owner's, not an
agent's:

- **Publishing releases.** The repository is private and there are no
  published builds. Until `[update] manifest` points at a real manifest, the
  updater says so and stops. Building and signing releases, and where they
  live, is a decision for the owner.
- **"Newer" versus "different".** herdr compares semantic versions; tend's
  version is the git description it was built from, and two of those have no
  order. `tend update` says the published build differs from this one.
- Background checks, the "an update is available" notice, release notes on
  first run after an update, Homebrew and the Windows installer path
  (`update.rs` covers all of those).

### 15. Windows — large, and cannot be run from Linux

- herdr: `platform/windows.rs`, `platform/windows/` (ConPTY, named pipes);
  herdr validates on a Windows VM.
- tend today: Unix only. The pty (`internal/pty/pty_unix.go`), terminal resize
  (`cmd/tend/resize_unix.go`) and server spawn (`cmd/tend/spawn_unix.go`) have no
  Windows counterpart, and the transport is a Unix socket. An agent on
  Linux can get it to cross-compile and no further; say so rather than claiming
  it works.

### Smaller gaps in what already exists

- Double-click does not select a word (see item 5).
- Claude Code asks for any-motion mouse reports; tend enables motion reporting
  on the outer terminal only while a menu is open, so hover never reaches it.
- `pane.rename` does not exist; the pane menu renames the tab instead.
- tend records that a program asked for focus events (mode 1004) and never
  sends any. herdr does (`terminal/runtime.rs`).
- `tend attach -ssh` has not been tried against a real sshd, and the mismatch
  notice's restart has not been exercised over it.

---

## Lessons already paid for

Each of these cost hours. They are here so they cost nobody else any.

- **Read herdr before designing.** See `AGENTS.md`. Eight attempts, then one
  hour.
- **A program that holds the mouse does its own selecting.** Forward the whole
  gesture — press, every drag, release — in the *pane's* coordinates and the
  encoding it asked for. Forwarding the raw report displaces every click by the
  sidebar and the border.
- **A full-screen agent keeps no scrollback in tend.** Its earlier output never
  reaches the terminal; it repaints from its own memory. Nothing client-side
  can select past its window.
- **Clients re-read the session only when told to.** A pane's agent, its state
  and its mouse mode live in the session, not in its output. Anything that
  changes them must publish an event, or every client shows the old answer.
- **A scroll view at offset zero is a frozen picture.** If the server answers a
  scroll request with the present, leave the scroll view.
- **`Fd()` on an `*os.File` is not free**: it detaches the descriptor from the
  runtime poller and races with a reader. Use `SyscallConn().Control`.
- **A timeout is not an answer.** Probing a Unix socket, `ECONNREFUSED` means
  nobody is there; a timeout means you do not know. Deleting a socket on "do
  not know" puts two servers on one session.
- **Staying put is a candidate.** Any "how far did it move" search that leaves
  out zero will find a move in a still picture.
- **The gate has been hollow before.** `make fmt` once called a `gofmt` that was
  not on PATH; empty output from a missing binary read as "already formatted".
