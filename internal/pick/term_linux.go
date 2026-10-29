package pick

import "golang.org/x/sys/unix"

const ioctlGet, ioctlSet = unix.TCGETS, unix.TCSETS
