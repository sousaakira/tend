//go:build unix

package explorer

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize sends on ch when the terminal changes size. The pane is
// resized whenever the tab's layout changes, and the panel must draw to the
// new size at once rather than at the next refresh.
func notifyResize(ch chan struct{}) (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				select {
				case ch <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
	}
}
