package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sousaakira/tend/internal/server"
)

// A replacement server is this binary started with what it needs already open.
//
// herdr sends the terminals over a socket as ancillary data. Here they are
// inherited instead, which needs no protocol at all: a child starts with the
// descriptors its parent chose for it, at numbers both sides know. The listening
// socket goes the same way, and that is the part worth having — the socket file
// never goes away and is never recreated, so there is no instant at which a
// client finds nobody listening. Whichever server accepts a connection is one
// that can serve it.
//
// Descriptors 0 to 2 are the usual three. What follows is fixed:
const (
	inheritManifestFD = 3 // the manifest, as JSON, until end of file
	inheritReadyFD    = 4 // where the replacement says it has everything
	inheritListenerFD = 5 // the session's listening socket
	inheritPaneFD     = 6 // the first pane's terminal; the rest follow in order
)

// readyWord is what the replacement writes once it holds every pane. Anything
// else, or nothing, is a replacement that did not make it.
const readyWord = "ready"

// replacementPatience is how long a replacement is given to say so. It reads
// a manifest and adopts descriptors; if that has not happened in this long it
// is not going to, and the panes have been paused for all of it.
const replacementPatience = 15 * time.Second

// replaceWith returns what a server calls to start its replacement: argv, run
// with the listener and the panes' terminals.
func replaceWith(ln net.Listener, argv []string) func(*server.Handoff) error {
	return func(h *server.Handoff) error {
		unix, ok := ln.(*net.UnixListener)
		if !ok {
			return errors.New("the session's socket cannot be handed to another process")
		}
		lnFile, err := unix.File()
		if err != nil {
			return fmt.Errorf("duplicating the socket: %w", err)
		}
		defer lnFile.Close()

		manifestR, manifestW, err := os.Pipe()
		if err != nil {
			return err
		}
		defer manifestR.Close()
		defer manifestW.Close()
		readyR, readyW, err := os.Pipe()
		if err != nil {
			return err
		}
		defer readyR.Close()
		defer readyW.Close()

		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin = nil
		// The old server's log is the new server's log: same session, same file.
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.ExtraFiles = append([]*os.File{manifestR, readyW, lnFile}, h.Files...)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("starting %s: %w", argv[0], err)
		}
		// Reaped whenever it ends, which when this works is long after this
		// process has gone.
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()

		// Our copies of the child's ends go now. While this process holds the
		// writing end of the ready pipe, a dead child reads as a silent one.
		_ = manifestR.Close()
		_ = readyW.Close()

		go func() {
			// A manifest is scrollback for every pane and outgrows a pipe's
			// buffer easily, so it is written while the child reads. A child
			// that dies first ends this with an error nobody needs: the ready
			// pipe tells the same story.
			_ = json.NewEncoder(manifestW).Encode(h.Manifest)
			_ = manifestW.Close()
		}()

		answer := make(chan string, 1)
		go func() {
			line, _ := bufio.NewReader(readyR).ReadString('\n')
			answer <- strings.TrimSpace(line)
		}()

		select {
		case word := <-answer:
			if word != readyWord {
				_ = cmd.Process.Kill()
				<-exited
				return errors.New("it exited before taking the panes; its reason is in the server log")
			}
		case <-time.After(replacementPatience):
			_ = cmd.Process.Kill()
			<-exited
			return fmt.Errorf("it did not answer within %s", replacementPatience)
		}

		// The socket file is the replacement's now. Closing a listener removes
		// the file it made, which here would cut off a server that is already
		// accepting on it.
		unix.SetUnlinkOnClose(false)
		return nil
	}
}

// inherit builds a server out of what a parent left open for it.
func inherit(cfg server.Config) (*server.Server, net.Listener, error) {
	manifestFile := os.NewFile(inheritManifestFD, "handoff-manifest")
	readyFile := os.NewFile(inheritReadyFD, "handoff-ready")
	lnFile := os.NewFile(inheritListenerFD, "handoff-listener")
	if manifestFile == nil || readyFile == nil || lnFile == nil {
		return nil, nil, errors.New("serve -inherit is started by a running server, not by hand")
	}
	defer readyFile.Close()

	var m server.HandoffManifest
	err := json.NewDecoder(manifestFile).Decode(&m)
	_ = manifestFile.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the handoff manifest: %w", err)
	}

	ln, err := net.FileListener(lnFile)
	_ = lnFile.Close() // FileListener works on its own duplicate
	if err != nil {
		return nil, nil, fmt.Errorf("taking over the socket: %w", err)
	}
	if unix, ok := ln.(*net.UnixListener); ok {
		// An inherited listener does not remove its file when closed, since it
		// did not make it. This one is the file's last owner, so it should.
		unix.SetUnlinkOnClose(true)
	}

	files := make([]*os.File, len(m.Panes))
	for i := range m.Panes {
		files[i] = os.NewFile(uintptr(inheritPaneFD+i), fmt.Sprintf("pane-%d", m.Panes[i].ID))
	}

	srv, err := server.NewFromHandoff(cfg, m, files, func() error {
		_, err := fmt.Fprintln(readyFile, readyWord)
		return err
	})
	if err != nil {
		if unix, ok := ln.(*net.UnixListener); ok {
			unix.SetUnlinkOnClose(false) // still the old server's
		}
		_ = ln.Close()
		return nil, nil, err
	}
	return srv, ln, nil
}
