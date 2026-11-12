package server

import (
	"net"

	"golang.org/x/sys/unix"
)

func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid := -1
	var credentialError error
	err = raw.Control(func(fd uintptr) {
		credential, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		credentialError = e
		if e == nil {
			uid = int(credential.Uid)
		}
	})
	if err != nil {
		return -1, err
	}
	return uid, credentialError
}
