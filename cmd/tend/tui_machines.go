package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/sousaakira/tend/internal/client"
	"github.com/sousaakira/tend/internal/machines"
	"github.com/sousaakira/tend/internal/proto"
	"github.com/sousaakira/tend/internal/transport"
	"github.com/sousaakira/tend/internal/ui"
	"github.com/sousaakira/tend/internal/vt"
)

// Saved machines in the sidebar: herdr's multi-endpoint client
// (client/shell/endpoint_sidebar.rs, endpoint_navigation.rs).
//
// Once there is a machine saved (`tend machine add`), the space list becomes a
// list of machines — this one first, as "Local" — each with its spaces beneath
// it. Every machine other than the one being shown is watched over its own
// connection, which asks for events and never for a pane's screen, so its
// spaces and what its agents are doing stay current at the cost of a snapshot
// now and then. Going to one of its spaces hands that connection to the
// client: the switch is immediate, as herdr's is, because the machine was
// already connected.

// endpoint is one machine the client can be on.
type endpoint struct {
	// id is "local" for this machine and the catalog's id for a saved one.
	id, label string
	// host and session are where it is, as openSessionOn takes them.
	host, session string
	enabled       bool

	// Under t.mu.
	status string
	snap   proto.SessionSnapshot
	have   bool
	// client is the watching connection, nil while there is none. It is
	// also the client's own connection while the machine is shown through
	// it, which link.forward says.
	client *client.Client
	link   *endpointLink
}

// localEndpoint is this machine's id, herdr's ClientEndpointId::Local.
const localEndpoint = "local"

// endpointLink is a watching connection's handler. While the machine is only
// watched it keeps the machine's snapshot fresh; once the client is shown
// through it, forward is set and everything goes to the client as it would
// from a connection the client had opened itself.
type endpointLink struct {
	t       *tui
	e       *endpoint
	forward atomic.Bool
	lost    chan struct{}
	// fetching coalesces snapshot reads: a burst of events is one read.
	fetching atomic.Bool
}

func (l *endpointLink) Disconnected() {
	close(l.lost)
	if l.forward.Load() {
		l.t.Disconnected()
	}
}

func (l *endpointLink) Event(ev proto.Event) {
	if l.forward.Load() {
		l.t.Event(ev)
		return
	}
	switch ev.Kind {
	case proto.EventPaneClipboard, proto.EventFocusRequest, proto.EventPaneFocused,
		proto.EventTabFocused, proto.EventWorkspaceFocused:
		// Nothing a list of spaces shows.
		return
	}
	l.fetch()
}

func (l *endpointLink) PaneOutput(pane uint64, data []byte) {
	if l.forward.Load() {
		l.t.PaneOutput(pane, data)
	}
}

// fetch reads the machine's snapshot off the reader goroutine — the reply to
// the call comes back through it.
func (l *endpointLink) fetch() {
	if !l.fetching.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer l.fetching.Store(false)
		l.t.mu.Lock()
		c := l.e.client
		l.t.mu.Unlock()
		if c == nil {
			return
		}
		snap, err := c.Snapshot()
		if err != nil {
			return
		}
		l.t.mu.Lock()
		l.e.snap, l.e.have = snap, true
		l.t.dirty = true
		l.t.mu.Unlock()
		l.t.wakeUp()
	}()
}

// machinesState is the client's list of machines.
type machinesState struct {
	endpoints []*endpoint
	// active is the id of the machine being shown.
	active string
	// folded are the machines whose spaces are put away, and remoteFolded
	// the groups folded on machines other than this one's session: a fold is
	// what one person is looking at, kept in the client as herdr keeps it.
	folded       map[string]bool
	remoteFolded map[string]map[string]bool
	stop         chan struct{}
	wg           sync.WaitGroup
}

// loadMachines reads the saved machines and starts watching every one that is
// on and not the one being shown. Without any saved, nothing changes: the
// sidebar is the space list it always was.
func (t *tui) loadMachines() {
	catalog, err := machines.Load()
	if err != nil {
		t.setMessage(err.Error(), true)
		return
	}
	if len(catalog.Machines) == 0 {
		return
	}
	local := &endpoint{id: localEndpoint, label: "Local", session: transport.DefaultSessionName, enabled: true, status: ui.MachineOnline}
	if t.host == "" {
		local.session = t.session
	}
	list := []*endpoint{local}
	active := ""
	if t.host == "" {
		active = localEndpoint
	}
	for _, m := range catalog.Machines {
		e := &endpoint{id: m.ID, label: m.Label, host: m.Target, session: m.Session, enabled: m.Enabled, status: ui.MachineDisabled}
		if m.Enabled {
			e.status = ui.MachineConnecting
		}
		if active == "" && m.Target == t.host && m.Session == t.session {
			active, e.enabled, e.status = m.ID, true, ui.MachineOnline
		}
		list = append(list, e)
	}
	if active == "" {
		// Attached with -host to a machine that is not saved: shown, as it
		// is where the client is, under the name it was reached by.
		list = append(list, &endpoint{id: "attached", label: t.host, host: t.host, session: t.session, enabled: true, status: ui.MachineOnline})
		active = "attached"
	}

	t.mu.Lock()
	t.machines = &machinesState{
		endpoints:    list,
		active:       active,
		folded:       make(map[string]bool),
		remoteFolded: make(map[string]map[string]bool),
		stop:         make(chan struct{}),
	}
	ms := t.machines
	t.mu.Unlock()
	for _, e := range list {
		if e.id != active && e.enabled {
			t.watchEndpoint(ms, e)
		}
	}
}

// stopMachines ends every watching connection.
func (t *tui) stopMachines() {
	t.mu.Lock()
	ms := t.machines
	t.mu.Unlock()
	if ms == nil {
		return
	}
	close(ms.stop)
	t.mu.Lock()
	var clients []*client.Client
	for _, e := range ms.endpoints {
		if e.client != nil {
			clients = append(clients, e.client)
		}
	}
	t.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
	ms.wg.Wait()
}

// watchEndpoint keeps a connection to a machine for as long as the client
// runs, reconnecting when it drops, with a backoff herdr's supervisor also
// caps at half a minute.
func (t *tui) watchEndpoint(ms *machinesState, e *endpoint) {
	ms.wg.Add(1)
	go func() {
		defer ms.wg.Done()
		delay := time.Second
		for {
			link := &endpointLink{t: t, e: e, lost: make(chan struct{})}
			c, err := openWatcher(e.host, e.session, link)
			if err != nil {
				t.setEndpointStatus(e, ui.MachineReconnecting)
				select {
				case <-ms.stop:
					return
				case <-time.After(delay):
				}
				delay = min(delay*2, 30*time.Second)
				continue
			}
			delay = time.Second
			t.mu.Lock()
			stopped := false
			select {
			case <-ms.stop:
				stopped = true
			default:
				e.client, e.link, e.status = c, link, ui.MachineOnline
			}
			t.dirty = true
			t.mu.Unlock()
			if stopped {
				_ = c.Close()
				return
			}
			link.fetch()
			t.wakeUp()

			select {
			case <-ms.stop:
				return // stopMachines closes the client
			case <-link.lost:
			}
			t.mu.Lock()
			if e.client == c {
				e.client, e.link = nil, nil
			}
			shown := ms.active == e.id
			if !shown {
				e.status = ui.MachineReconnecting
			}
			t.dirty = true
			t.mu.Unlock()
			if shown {
				// Shown through this connection when it dropped: the client's
				// own reconnect takes the machine back, with a connection of
				// its own, and this one goes on watching once it is left.
				return
			}
		}
	}()
}

// openWatcher connects to a machine for watching: without a question asked
// on the terminal when it is another one, and starting this one's server as
// attaching would, since herdr's Local is always there to be gone to.
func openWatcher(host, session string, handler client.Handler) (*client.Client, error) {
	if host == "" {
		return openSessionOn("", session, handler)
	}
	return client.DialRemote(transport.WatchArgv(host, session), handler)
}

func (t *tui) setEndpointStatus(e *endpoint, status string) {
	t.mu.Lock()
	if e.status != status {
		e.status = status
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// multiMachineLocked reports whether the sidebar lists machines.
func (t *tui) multiMachineLocked() bool {
	return t.machines != nil && len(t.machines.endpoints) > 1
}

// endpointLocked finds a machine by id.
func (t *tui) endpointLocked(id string) *endpoint {
	if t.machines == nil {
		return nil
	}
	for _, e := range t.machines.endpoints {
		if e.id == id {
			return e
		}
	}
	return nil
}

// activeLabelLocked is the name of the machine being shown.
func (t *tui) activeLabelLocked() string {
	if e := t.endpointLocked(t.machines.active); e != nil {
		return e.label
	}
	return "Local"
}

// machineRowsLocked is the space list with machines: each machine's row, and
// beneath it, unless it is folded, its spaces laid out as the plain list lays
// out this session's.
func (t *tui) machineRowsLocked() []ui.SidebarRow {
	ms := t.machines
	var rows []ui.SidebarRow
	for _, e := range ms.endpoints {
		shown := e.id == ms.active
		folded := ms.folded[e.id]
		signal := ""
		if e.id != localEndpoint {
			signal = ui.MachineSignal(e.status)
		}
		rows = append(rows, ui.SidebarRow{
			Kind:     ui.SidebarMachine,
			Label:    e.label,
			Trailing: signal,
			State:    e.status,
			Action:   ui.ActionMachine,
			Machine:  e.id,
			Folded:   folded,
			Active:   shown,
			Stale:    e.status == ui.MachineDisabled,
		})
		if folded {
			continue
		}
		if shown {
			rows = append(rows, t.spaceRowsFrom(spaceSource{snap: &t.snap, folded: t.folded, current: t.workspace, depth: 1})...)
			continue
		}
		if !e.have {
			continue
		}
		groups := ms.remoteFolded[e.id]
		if groups == nil {
			groups = make(map[string]bool)
			ms.remoteFolded[e.id] = groups
		}
		rows = append(rows, t.spaceRowsFrom(spaceSource{
			snap: &e.snap, machine: e.id, folded: groups, depth: 1,
			stale: e.status != ui.MachineOnline,
		})...)
	}
	return rows
}

// clickMachine is a machine row's click: the machine being shown folds and
// unfolds, as herdr's does; another one is gone to.
func (t *tui) clickMachine(id string) error {
	t.mu.Lock()
	if t.machines == nil {
		t.mu.Unlock()
		return nil
	}
	if id == t.machines.active {
		t.machines.folded[id] = !t.machines.folded[id]
		t.dirty = true
		t.mu.Unlock()
		t.wakeUp()
		return nil
	}
	t.mu.Unlock()
	return t.switchMachine(id, 0)
}

// toggleRemoteGroup folds a group on a machine that is not the one shown.
func (t *tui) toggleRemoteGroup(machine, group string) {
	t.mu.Lock()
	if t.machines != nil {
		groups := t.machines.remoteFolded[machine]
		if groups == nil {
			groups = make(map[string]bool)
			t.machines.remoteFolded[machine] = groups
		}
		groups[group] = !groups[group]
		t.dirty = true
	}
	t.mu.Unlock()
	t.wakeUp()
}

// switchMachine shows another machine, at one of its spaces when workspace is
// set: herdr's ActivateEndpoint with a focus target.
//
// The watching connection becomes the client's. The one the client had goes
// back to watching if it was a watcher, and is closed if the client opened it
// itself — at start, or on a reconnect — with the machine it showed watched
// afresh.
func (t *tui) switchMachine(id string, workspace uint64) error {
	t.mu.Lock()
	ms := t.machines
	e := t.endpointLocked(id)
	if ms == nil || e == nil || id == ms.active {
		t.mu.Unlock()
		return nil
	}
	if e.client == nil || e.status != ui.MachineOnline {
		t.mu.Unlock()
		t.setMessage(e.label+" is not ready", true)
		return nil
	}
	previous := t.endpointLocked(ms.active)
	old := t.client
	oldWatched := previous != nil && previous.client == old
	t.mu.Unlock()

	if !oldWatched && old != nil {
		// Closed first: closing it reports it lost, which must not land
		// after the swap and mark the machine now shown offline, or send
		// the client to reconnect to it.
		_ = old.Close()
		select {
		case <-t.lostConn:
		default:
		}
	}

	t.mu.Lock()
	if e.client == nil {
		// It dropped in between. The client stays where it was, and if its
		// connection was the one just closed, it reconnects there.
		t.mu.Unlock()
		if !oldWatched {
			t.Disconnected()
		}
		t.setMessage(e.label+" is not ready", true)
		return nil
	}
	// What the old machine was showing is kept for its rows: the snapshot is
	// the one the client just had.
	if previous != nil {
		previous.snap, previous.have = t.snap, true
		if previous.link != nil && oldWatched {
			previous.link.forward.Store(false)
		}
	}
	e.link.forward.Store(true)
	ms.active = id
	t.client = e.client
	t.host, t.session = e.host, e.session
	t.offline = false
	t.screens = make(map[uint64]*vt.Screen)
	t.sizes = make(map[uint64]ui.Rect)
	t.workspace, t.tab, t.focus, t.zoom = workspace, 0, 0, false
	t.dirty = true
	t.mu.Unlock()

	if oldWatched {
		// Its screens were for the client; watching needs none of them.
		_ = old.SubscribePanes(nil)
	} else if previous != nil && previous.enabled {
		t.watchEndpoint(ms, previous)
	}

	t.requestRepaint()
	if err := t.ensureSession(); err != nil {
		return err
	}
	if err := t.refresh(); err != nil {
		return err
	}
	t.setMessage("on "+e.label, false)
	return nil
}
