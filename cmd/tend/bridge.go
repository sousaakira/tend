package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/auth-com-br/tend/internal/transport"
)

// runBridge joins stdin and stdout to a session's socket.
//
// It is the far end of a remote session: `tend attach -ssh host` runs this
// over ssh and speaks the ordinary protocol through it. Nothing here
// understands the protocol — it copies bytes — which is what keeps a remote
// session from being a second kind of session with its own bugs.
func runBridge(args []string) error {
	fs := flag.NewFlagSet("bridge", flag.ExitOnError)
	name := sessionFlag(fs)
	apiSocket := fs.Bool("api", false, "join the session's automation socket instead, for a script's -ssh")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(),
			"usage: tend bridge [-s session]\n\n"+
				"joins stdin and stdout to a session, starting its server if there is\n"+
				"none. this is what \"tend attach -ssh host\" runs on the other machine.\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *apiSocket {
		// No server is started for this: a script asking about a session
		// that is not running should be told so, not given an empty one.
		path, err := transport.APISocketPath(*name)
		if err != nil {
			return err
		}
		conn, err := net.Dial("unix", path)
		if err != nil {
			return fmt.Errorf("session %q is not running on this machine: %w", sessionName(*name), err)
		}
		defer conn.Close()
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(conn, os.Stdin); done <- struct{}{} }()
		go func() { _, _ = io.Copy(os.Stdout, conn); done <- struct{}{} }()
		<-done
		return nil
	}

	path, err := transport.SocketPath(*name)
	if err != nil {
		return err
	}
	conn, err := transport.Dial(path)
	if err != nil {
		if !notRunning(err) {
			return err
		}
		// The same courtesy a local attach gets: asking for a session is
		// enough to have one.
		if err := startServer(*name); err != nil {
			return err
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if conn, err = transport.Dial(path); err == nil {
				break
			}
			if !notRunning(err) || time.Now().After(deadline) {
				return fmt.Errorf("started a server for %q but it never came up; see %s", *name, serverLog(path))
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	defer conn.Close()

	// Either direction ending ends the bridge: the client going away closes
	// stdin, and the server going away closes the socket.
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(conn, os.Stdin); done <- struct{}{} }()
	go func() { _, _ = io.Copy(os.Stdout, conn); done <- struct{}{} }()
	<-done
	return nil
}
