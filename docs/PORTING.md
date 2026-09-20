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
- **Settings file** with validation, `tend config`.

## Different from herdr on purpose

| | herdr | tend | why |
|---|---|---|---|
| Terminal core | libghostty-vt via FFI | pure Go | owner's decision; no cgo |
| Agent state | screen detection **and** hooks installed into each agent | screen detection only | hooks not ported yet — see queue item 2 |
| Forced selection | none inside a mouse-holding program | alt+drag selects a block anywhere | fallback for programs that hold the mouse and do nothing with a drag |
| Clipboard | OSC 52 only | local tool (`wl-copy`/`xclip`/`xsel`/`pbcopy`) when not over ssh, plus OSC 52 always | the owner's terminal refuses OSC 52 |
| Names | workspace | space (in the UI; `workspace` in code and on the wire) | matches herdr's own UI wording |

---

## Not ported — the work queue

Ordered by how much each changes daily use. Sizes are herdr's, as a guide to
effort, not a target.

### 1. Session persistence and live handoff — large

Restarting tend's server loses every pane. herdr restores the layout after a
restart and can replace a running server without dropping its panes.

- herdr: `persist/snapshot.rs` (1266), `persist/restore.rs` (1722),
  `persist/io.rs`; handoff in `server/headless/lifecycle.rs`, `update.rs`
  (`server.live_handoff`); user docs `session-state.mdx`,
  `persistence-remote.mdx`.
- tend today: nothing. `make restart` and the `r` key on the mismatch notice
  both close the session's panes.
- This is the feature whose absence has cost the owner the most time.

### 2. Agent integrations (hooks) — large

herdr installs hooks into each agent so the agent reports its own state, which
is more reliable than reading the screen.

- herdr: `integration/` (10.9k) with assets for 19 agents under
  `integration/assets/`; API `integration.install|list|uninstall`,
  `pane.report_agent`, `pane.report_agent_session`, `pane.release_agent`,
  `pane.clear_agent_authority`; user docs `integrations.mdx`.
- Note the versioning rule in `../herdr/CLAUDE.md` ("Integration asset
  versions") before porting the assets.

### 3. Automation API and CLI — large, and a prerequisite

The half of herdr built for scripts and for other agents. **Plugins depend on
it**: in herdr "the entire CLI is the plugin API".

- herdr: `api/schema*` (9.4k), `cli/agent.rs`, `cli/pane.rs`, `cli/tab.rs`,
  `cli/workspace.rs`, `cli/api.rs`; methods such as `agent.list|get|read|
  prompt|send_keys|wait|start|explain`, `pane.read|send_text|send_keys|
  wait_for_output|scroll|get|list|current`, `events.subscribe|wait`,
  `layout.export|apply`; user docs `socket-api.mdx`, `cli-reference.mdx`,
  `agent-automation.mdx`.
- tend today: `tend screen`, `tend watch`, `tend follow`, `tend ls`, `tend new`.

### 4. Plugins — large; needs item 3 first

- herdr: `plugin_command.rs`, `plugin_paths.rs`, `cli/plugin.rs`,
  `persist/plugin_registry.rs`, `app/api/plugins/`, `api/schema/plugins.rs`;
  manifest file `herdr-plugin.toml`; API `plugin.list|enable|disable|link|
  unlink`; user docs `plugins.mdx`, `marketplace.mdx`.
- **The right-hand file explorer panel in the owner's herdr is a plugin**, not
  part of herdr: `herdr-sidebar`, installed under
  `~/.config/herdr/plugins/github/`. It is not in `../herdr/src`. Porting the
  plugin host is what makes that panel possible; the panel itself is separate.

### 5. Copy mode and scrollback tools — medium

- herdr: `client/shell/copy_mode.rs` (873) — keyboard selection and search in
  scrollback; `pane.copy_motion`, `pane.copy_search`; `EditScrollback` opens the
  history in `$EDITOR` (`server/client_commands.rs`); double-click word
  selection in `client/shell/word_selection.rs` (217).
- tend today: `ctrl+b [` scrolls; selection is mouse-only.

### 6. Worktrees — medium

- herdr: `worktree.rs` (954), `workspace/git/`, `client/shell/worktrees.rs`,
  `worktree_overlays.rs`, `cli/worktree.rs`; API `worktree.create|list|open|
  remove`.

### 7. Moving and swapping — medium

- herdr: `pane.swap`, `pane.move`, `tab.move`, `workspace.move`,
  `workspace.move_block`; keys `SwapPane*`, `MoveTabPrevious|Next`.
- tend today: nothing can be reordered.

### 8. Navigation extras — medium

- herdr: `WorkspacePicker`, `OpenNavigator`
  (`client/shell/workspace_navigation.rs`, `aggregate_navigation.rs`),
  `LastPane`, `CyclePaneNext|Previous`, `EnterResizeMode`, `PreviousAgent`,
  `NextAgent`, `FocusAgent`. The full list of key actions is the
  `KeybindAction` enum in `input/keybindings.rs`.
- tend today: `ctrl+b g` walks the sidebar; focus by direction and `o`.

### 9. Notifications and sound — medium

- herdr: `client/shell/notifications.rs`, `notification_policy.rs`, `sound.rs`
  (482), `config/sound.rs`; API `notification.show`; key
  `OpenNotificationTarget`.
- tend today: the "N waiting" count on the status bar, and nothing else.

### 10. Settings UI, onboarding, live reload — medium

- herdr: `client/shell/settings.rs`, `settings_overlay.rs`, `ui/onboarding.rs`,
  `ui/release_notes.rs`; `server.reload_config`; key `ReloadConfig`.
- tend today: a TOML file read once at start.

### 11. Configurable chrome — medium

- herdr: `config/sidebar.rs` and `ui/sidebar/tokens.rs` (what each sidebar row
  shows, as a list of styled tokens), `config/tab_bar.rs`,
  `config/window_title.rs`, `config/keybinds.rs` (every key rebindable),
  `config/theme.rs`.
- tend today: the sidebar rows, the tab bar and the keys are fixed; only the
  prefix key and a few colours can be set.

### 12. Agent session resume — medium

- herdr: `agent_resume.rs` (859) — resumes the agent's own session when a pane
  is restored. Depends on item 1.

### 13. Kitty graphics — medium

- herdr: `kitty_graphics.rs` (1509), `kitty_graphics/surface.rs`,
  `server/headless/pane_graphics.rs`, `client/shell/graphics.rs`.
- tend today: `internal/vt` parses APC sequences and discards them
  (`Screen.APCDispatch` is empty), so an image never reaches the outer terminal.

### 14. Updater with channels — medium

- herdr: `update.rs` (3.8k); stable and preview channels, manifests
  `distribution/latest.json` and `distribution/preview.json`; `herdr channel
  set`, `herdr update`.
- tend today: `make dist` cross-compiles; there are no published releases (the
  repository is private) and no updater. Publishing releases is the owner's
  decision, not an agent's.

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
