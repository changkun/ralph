//go:build !unix

package session

import "errors"

// Launch starts the engine for folder. It is not supported on this system.
func Launch(exe, folder string) (pid int, err error) {
	return 0, errors.New("starting a background session is not supported on this system")
}
