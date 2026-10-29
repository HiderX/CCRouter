//go:build darwin || linux

package usage

import (
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(fi os.FileInfo) string {
	s := fi.Sys().(*syscall.Stat_t)
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino)
}
