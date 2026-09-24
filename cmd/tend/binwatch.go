package main

import (
	"os"
	"time"
)

// binaryWatch notices that the tend binary a long-running program was
// started from has been replaced on disk — by `make install`, `tend
// update`, the install script — so the program can start the new one.
//
// A change is acted on only once the file has held still between two
// looks: an install writes the file over some moments, and starting it
// half written would fail, or worse.
type binaryWatch struct {
	path    string
	started os.FileInfo
	seen    os.FileInfo
}

// watchBinary watches the binary this process runs, or returns nil when it
// cannot say which that is.
func watchBinary() *binaryWatch {
	path, err := os.Executable()
	if err != nil {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	return &binaryWatch{path: path, started: info}
}

func sameBuild(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

// changed reports whether a new binary is in place and has settled.
func (w *binaryWatch) changed() bool {
	info, err := os.Stat(w.path)
	if err != nil || info.Mode()&0o111 == 0 || sameBuild(info, w.started) {
		w.seen = nil
		return false
	}
	if w.seen != nil && sameBuild(info, w.seen) && time.Since(info.ModTime()) > time.Second {
		return true
	}
	w.seen = info
	return false
}
