//go:build darwin || linux

package usage

import (
	"fmt"
	"os"
	"syscall"
)

// alive reports whether process pid exists; one owned by another user still counts.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	e := syscall.Kill(pid, 0)
	return e == nil || e == syscall.EPERM
}

func fileIdentity(fi os.FileInfo) string {
	s := fi.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino)
}
