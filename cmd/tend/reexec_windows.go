//go:build windows

package main

import "errors"

// reexec has no equivalent here: Windows has no exec that keeps the
// process. The panel goes on with the build it has.
func reexec(string, []string, []string) error {
	return errors.New("replacing a running program is not supported on windows")
}
