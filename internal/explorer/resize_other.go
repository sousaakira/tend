//go:build !unix

package explorer

// notifyResize has no signal to listen for off unix; the panel picks up a
// new size at its next refresh.
func notifyResize(ch chan struct{}) (stop func()) { return func() {} }
