//go:build unix

package pty

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartAndRead(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "printf hello"}, Options{Size: Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	var buf bytes.Buffer
	n, err := io.Copy(&buf, p)
	if err != nil {
		t.Fatalf("Copy: n=%d err=%#v", n, err)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("output = %q, want it to contain hello", buf.String())
	}
	if err := p.Wait(); err != nil {
		t.Errorf("Wait: %v", err)
	}
}

// TestReadEndsAtEOF is the behaviour the callers depend on: Linux fails the
// read with EIO when the child exits, and that must look like end of file.
func TestReadEndsAtEOF(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "printf one; printf two"}, Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, p)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Copy ended with %#v, want nil (EOF)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Copy did not finish")
	}
}

func TestWriteIsInput(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "read line; printf 'got:%s' \"$line\""}, Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()

	if _, err := p.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, p); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if !strings.Contains(buf.String(), "got:ping") {
		t.Errorf("output = %q, want it to contain got:ping", buf.String())
	}
}

func TestResize(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "sleep 0.2"}, Options{Size: Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()
	if err := p.Resize(Size{Cols: 100, Rows: 30}); err != nil {
		t.Errorf("Resize: %v", err)
	}
	// An invalid size is ignored rather than failing: a bad size is
	// recoverable, a dead pane is not.
	if err := p.Resize(Size{}); err != nil {
		t.Errorf("Resize with a zero size: %v", err)
	}
	_, _ = io.Copy(io.Discard, p)
	_ = p.Wait()
}

// TestAResizeReachesTheProgramAndLeavesTheReaderInterruptible: a program sees
// the size a pane was given, a pause — which a handoff is built on — still
// wakes the reader after a resize, and resizing while the pane closes does not
// race the close, as creack.Setsize's Fd() did under the race detector.
func TestAResizeReachesTheProgramAndLeavesTheReaderInterruptible(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "read go; stty size; sleep 30"}, Options{Size: Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	if err := p.Resize(Size{Cols: 101, Rows: 33}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if _, err := p.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	buf := make([]byte, 256)
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(seen.String(), "33 101"); {
		if time.Now().After(deadline) {
			t.Fatalf("stty size = %q, want 33 101", seen.String())
		}
		n, err := p.Read(buf)
		if err != nil {
			t.Fatalf("read: %v (seen %q)", err, seen.String())
		}
		seen.Write(buf[:n])
	}

	woke := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 256))
		woke <- err
	}()
	time.Sleep(100 * time.Millisecond)
	p.Pause()
	select {
	case err := <-woke:
		if !errors.Is(err, ErrPaused) {
			t.Fatalf("a paused read after a resize = %v, want ErrPaused", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("after a resize, Pause no longer wakes the reader")
	}

	// Resizing while the terminal closes is an error at worst, never a race.
	p.Resume()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = p.Resize(Size{Cols: uint16(80 + i%20), Rows: 24})
		}
	}()
	_ = p.Close()
	<-done
}

func TestStartRejectsEmptyCommand(t *testing.T) {
	if _, err := Start("", nil, Options{}); err == nil {
		t.Error("Start with no command should fail")
	}
}

func TestSizeValid(t *testing.T) {
	if (Size{Cols: 80, Rows: 24}).Valid() != true {
		t.Error("80x24 should be valid")
	}
	for _, s := range []Size{{}, {Cols: 80}, {Rows: 24}} {
		if s.Valid() {
			t.Errorf("%v should be invalid", s)
		}
	}
}

func TestDefaultSizeIsUsedWhenInvalid(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "stty size"}, Options{Size: Size{}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer p.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, p)
	// stty prints "rows cols".
	want := "40 120"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("stty size = %q, want it to contain %q", strings.TrimSpace(buf.String()), want)
	}
}

// TestCloseUnblocksAPendingRead is the contract the server depends on for
// shutdown, and it does not come for free.
//
// Closing the master alone does not interrupt a Read already blocked on it:
// the descriptor is not registered with Go's poller, so the read is a plain
// syscall and the goroutine would stay parked forever. Close sends SIGHUP
// first, which ends the process and so ends the read.
func TestCloseUnblocksAPendingRead(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "sleep 30"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, p)
		readDone <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the read block

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not unblock after Close")
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- p.Wait() }()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
}

// TestKillEndsAProcessThatIgnoresHangup is the escalation path.
//
// Close ends the read on its own now — the master is managed by the poller, so
// closing it wakes whoever is blocked on it — which is a separate matter from
// whether the process is gone. A process that ignores the hangup is still
// running with nothing attached to it, and Kill is what ends it.
func TestKillEndsAProcessThatIgnoresHangup(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "trap '' HUP; sleep 30"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Kill() }()

	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, p)
		readDone <- err
	}()
	time.Sleep(50 * time.Millisecond)

	_ = p.Close() // SIGHUP, which this process ignores
	select {
	case <-readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close should wake a reader blocked on the terminal")
	}

	// The reader is free, and the process is not dead: that is what Kill is for.
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case <-exited:
		t.Log("the shell exited on hangup anyway; the escalation is still exercised below")
	case <-time.After(300 * time.Millisecond):
	}

	if err := p.Kill(); err != nil && !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("Kill: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the process survived Kill")
	}
}

// TestPauseStopsAReaderWithoutLosingOutput is what a handoff stands on: the
// reader has to be stopped on purpose, and what arrives while it is stopped
// has to be there for whoever reads next.
func TestPauseStopsAReaderWithoutLosingOutput(t *testing.T) {
	p, err := Start("/bin/sh", []string{"-c", "echo FIRST; sleep 0.5; echo SECOND; sleep 30"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	buf := make([]byte, 256)
	n, err := p.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "FIRST") {
		t.Fatalf("first read = %q, %v", buf[:n], err)
	}

	// A reader parked in Read is woken by the pause, not left there.
	woke := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 256))
		woke <- err
	}()
	time.Sleep(100 * time.Millisecond)
	p.Pause()
	select {
	case err := <-woke:
		if !errors.Is(err, ErrPaused) {
			t.Fatalf("a paused read = %v, want ErrPaused", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Pause did not wake the reader")
	}
	if _, err := p.Read(buf); !errors.Is(err, ErrPaused) {
		t.Errorf("reading while paused = %v, want ErrPaused", err)
	}

	// SECOND is written while nobody is reading, and is still there after.
	time.Sleep(700 * time.Millisecond)
	p.Resume()
	n, err = p.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "SECOND") {
		t.Errorf("after resuming = %q, %v; what arrived while paused was lost", buf[:n], err)
	}
}

// TestAdoptTakesOverATerminal: the process is somebody else's child, so it can
// be read, written and signalled, and only waited for in the sense of being
// seen to go.
func TestAdoptTakesOverATerminal(t *testing.T) {
	first, err := Start("/bin/sh", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	pid := first.Pid()
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }()

	// What a handoff does: stop reading, pass the descriptor on, let go of it
	// without hanging the process up.
	first.Pause()
	handed, err := first.Dup()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Adopt(handed, pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("Release must not end the process: %v", err)
	}

	if _, err := second.Write([]byte("echo ADOPTED-$$\n")); err != nil {
		t.Fatal(err)
	}
	var seen strings.Builder
	buf := make([]byte, 256)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(seen.String(), "ADOPTED-"+strconv.Itoa(pid)) {
		n, err := second.Read(buf)
		seen.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(seen.String(), "ADOPTED-"+strconv.Itoa(pid)) {
		t.Fatalf("the same shell should answer through the adopted terminal, got %q", seen.String())
	}

	_ = second.Close()
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Wait on an adopted terminal should be bounded")
	}
}
