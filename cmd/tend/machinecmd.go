package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/auth-com-br/tend/internal/machines"
	"github.com/auth-com-br/tend/internal/transport"
)

const machineUsage = `usage:
  tend machine list [-json]
  tend machine add <ssh-target> -label <label> [-session <name>]
  tend machine rename <id> -label <label>
  tend machine remove <id>
  tend machine enable <id>
  tend machine disable <id>

A saved machine is kept an eye on by every client: its spaces, tabs and
panes are in the navigator (ctrl+b g), and choosing one there moves the
client to that machine. Add prepares tend on the machine and starts its
session before saving it. Removing or disabling one leaves its sessions
running. Only a label, the ssh target, the session and whether it is on are
saved; keys and passwords stay OpenSSH's.
`

// runMachine is herdr's `herdr machine`.
func runMachine(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, machineUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		return machineList(args[1:])
	case "add":
		return machineAdd(args[1:])
	case "rename":
		return machineRename(args[1:])
	case "remove":
		return machineEdit(args[1:], "remove", func(c *machines.Catalog, id string) error { return c.Remove(id) })
	case "enable", "disable":
		on := args[0] == "enable"
		return machineEdit(args[1:], args[0], func(c *machines.Catalog, id string) error {
			m, err := c.Find(id)
			if err == nil {
				m.Enabled = on
			}
			return err
		})
	case "help", "-h", "--help":
		fmt.Print(machineUsage)
		return nil
	}
	fmt.Fprint(os.Stderr, machineUsage)
	return fmt.Errorf("unknown machine command %q", args[0])
}

func machineList(args []string) error {
	fs := flag.NewFlagSet("machine list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print the machines as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := machines.Load()
	if err != nil {
		return err
	}
	if *asJSON {
		data, err := json.MarshalIndent(c.Machines, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	if len(c.Machines) == 0 {
		fmt.Println("No saved machines.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tLABEL\tTARGET\tSESSION\tSTATE")
	for _, m := range c.Machines {
		state := "on"
		if !m.Enabled {
			state = "off"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", m.ID, m.Label, m.Target, m.Session, state)
	}
	return w.Flush()
}

// flagsAfter parses flags written after positional arguments too, as
// "machine add host -label x" is written.
func flagsAfter(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	return positional, nil
}

func machineAdd(args []string) error {
	fs := flag.NewFlagSet("machine add", flag.ExitOnError)
	label := fs.String("label", "", "what to call the machine (required)")
	session := fs.String("session", transport.DefaultSessionName, "the session on that machine")
	positional, err := flagsAfter(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 || *label == "" {
		return errors.New("usage: tend machine add <ssh-target> -label <label> [-session <name>]")
	}
	c, err := machines.Load()
	if err != nil {
		return err
	}
	m, err := c.Add(*label, positional[0], *session)
	if err != nil {
		return err
	}
	// Ready before saved, as herdr's add is: a machine saved but unusable
	// would be a row that fails every time it is chosen.
	if err := prepareRemote(m.Target, m.Session); err != nil {
		return fmt.Errorf("%w; the machine was not saved", err)
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Printf("saved %s as %s (%s)\n", m.Target, m.Label, m.ID)
	return nil
}

func machineRename(args []string) error {
	fs := flag.NewFlagSet("machine rename", flag.ExitOnError)
	label := fs.String("label", "", "the new label (required)")
	positional, err := flagsAfter(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 || *label == "" {
		return errors.New("usage: tend machine rename <id> -label <label>")
	}
	return machineEdit(positional, "rename", func(c *machines.Catalog, id string) error { return c.Rename(id, *label) })
}

// machineEdit changes one saved machine and saves the catalog.
func machineEdit(args []string, what string, change func(*machines.Catalog, string) error) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tend machine %s <id>", what)
	}
	c, err := machines.Load()
	if err != nil {
		return err
	}
	if err := change(&c, args[0]); err != nil {
		return err
	}
	return c.Save()
}
