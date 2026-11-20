package integrations

import "errors"

var ErrLocked = errors.New("file is already locked")
