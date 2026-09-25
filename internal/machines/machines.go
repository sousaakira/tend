// Package machines is the list of other machines a client keeps an eye on:
// herdr's saved SSH endpoints (`client/endpoint/catalog.rs`, `herdr
// machine`). Each is a label, an ssh target and a session there, and
// whether it is on. Nothing else is kept — keys and passwords stay
// OpenSSH's.
package machines

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auth-com-br/tend/internal/transport"
)

// Limits, herdr's.
const (
	maxMachines  = 64
	maxLabelLen  = 128
	maxTargetLen = 1024
	maxFileBytes = 64 * 1024
	version      = 1
)

// Machine is one saved machine.
type Machine struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Session string `json:"session"`
	Enabled bool   `json:"enabled"`
}

// Catalog is the saved machines.
type Catalog struct {
	Version  int       `json:"version"`
	Machines []Machine `json:"machines"`
}

// ErrNotFound is a machine id that is not saved.
var ErrNotFound = errors.New("machines: no such machine")

// Path is where the catalog is kept: beside the sessions' state.
func Path() (string, error) {
	dir, err := transport.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "machines.json"), nil
}

// Load reads the catalog, empty when there is none.
func Load() (Catalog, error) {
	path, err := Path()
	if err != nil {
		return Catalog{}, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return Catalog{Version: version}, nil
	}
	if err != nil {
		return Catalog{}, err
	}
	if info.Size() > maxFileBytes {
		return Catalog{}, fmt.Errorf("machines: %s is larger than %d bytes", path, maxFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return Catalog{}, fmt.Errorf("machines: %s: %w", path, err)
	}
	for _, m := range c.Machines {
		if err := m.validate(); err != nil {
			return Catalog{}, fmt.Errorf("machines: %s: %s: %w", path, m.ID, err)
		}
	}
	return c, nil
}

// Save writes the catalog, whole or not at all: to a file beside it, then
// renamed over it.
func (c Catalog) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	c.Version = version
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".machines-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Add saves a machine, on, and returns its id.
func (c *Catalog) Add(label, target, session string) (Machine, error) {
	if len(c.Machines) >= maxMachines {
		return Machine{}, fmt.Errorf("machines: at most %d can be saved", maxMachines)
	}
	if session == "" {
		session = transport.DefaultSessionName
	}
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Machine{}, err
	}
	m := Machine{ID: "m_" + hex.EncodeToString(raw[:]), Label: strings.TrimSpace(label), Target: target, Session: session, Enabled: true}
	if err := m.validate(); err != nil {
		return Machine{}, err
	}
	for _, other := range c.Machines {
		if other.Target == m.Target && other.Session == m.Session {
			return Machine{}, fmt.Errorf("machines: %s, session %s, is saved already as %s", m.Target, m.Session, other.ID)
		}
	}
	c.Machines = append(c.Machines, m)
	return m, nil
}

// Find is a saved machine by id.
func (c *Catalog) Find(id string) (*Machine, error) {
	for i := range c.Machines {
		if c.Machines[i].ID == id {
			return &c.Machines[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Remove forgets a machine; its sessions there keep running.
func (c *Catalog) Remove(id string) error {
	for i := range c.Machines {
		if c.Machines[i].ID == id {
			c.Machines = append(c.Machines[:i], c.Machines[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Rename relabels a machine.
func (c *Catalog) Rename(id, label string) error {
	m, err := c.Find(id)
	if err != nil {
		return err
	}
	next := *m
	next.Label = strings.TrimSpace(label)
	if err := next.validate(); err != nil {
		return err
	}
	*m = next
	return nil
}

// Enabled is the machines that are on.
func (c Catalog) Enabled() []Machine {
	var out []Machine
	for _, m := range c.Machines {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}

// validate is herdr's SavedSshEndpoint::validate.
func (m Machine) validate() error {
	hasControl := func(s string) bool {
		return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
	}
	switch {
	case m.Label == "":
		return errors.New("a machine needs a label")
	case len(m.Label) > maxLabelLen || hasControl(m.Label):
		return fmt.Errorf("a label is at most %d bytes, with no control characters", maxLabelLen)
	case m.Target == "" || strings.HasPrefix(m.Target, "-"):
		return errors.New("an ssh target is user@host or host, and does not start with -")
	case len(m.Target) > maxTargetLen || hasControl(m.Target) || strings.ContainsAny(m.Target, " \t"):
		return fmt.Errorf("an ssh target is at most %d bytes, with no spaces or control characters", maxTargetLen)
	}
	authority := strings.TrimPrefix(m.Target, "ssh://")
	if user, _, ok := strings.Cut(authority, "@"); ok && strings.Contains(user, ":") {
		return errors.New("an ssh target must not hold a password")
	}
	return transport.ValidSessionName(m.Session)
}
