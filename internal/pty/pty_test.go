//go:build unix

package pty

import (
	"bytes"
	"io"
	"strings"
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
