package ui

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"golang.org/x/sys/unix"
)

func supportsLoginItem() bool {
	version, e := unix.Sysctl("kern.osproductversion")
	if e != nil {
		return false
	}
	n, e := strconv.Atoi(strings.Split(version, ".")[0])
	return e == nil && n >= 13 && mygo.App.IsPackaged()
}
