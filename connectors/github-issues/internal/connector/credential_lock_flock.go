//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package connector

import (
	"context"
	"os"
	"syscall"
	"time"
)

func lockCredentialFile(ctx context.Context, filename string) (func(), error) {
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}
