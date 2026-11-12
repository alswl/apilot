//go:build darwin || linux

package server

import (
	"os"
	"syscall"
)

func socketOwnedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
