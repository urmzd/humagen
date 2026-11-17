//go:build !unix

package vendorcli

import "errors"

// lockState cannot hold the state directory against another process here,
// so no session is continued or noted.
func lockState(string) (func(), error) {
	return nil, errors.New("vendorcli: the session state cannot be locked on this system")
}
