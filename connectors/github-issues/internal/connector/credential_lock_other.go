//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package connector

import (
	"context"
	"errors"
)

func lockCredentialFile(context.Context, string) (func(), error) {
	return nil, errors.New("GitHub credential locking is unavailable on this platform")
}
