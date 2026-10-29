//go:build darwin || linux

package pick

import (
	"os"

	"golang.org/x/sys/unix"
)

// Available reports whether both ends are an interactive terminal.
func Available() bool {
	_, in := unix.IoctlGetTermios(int(os.Stdin.Fd()), ioctlGet)
	_, out := unix.IoctlGetTermios(int(os.Stderr.Fd()), ioctlGet)
	return in == nil && out == nil
}

// raw switches stdin to byte-at-a-time input without echo. Output processing stays on.
func raw() (func(), error) {
	fd := int(os.Stdin.Fd())
	old, e := unix.IoctlGetTermios(fd, ioctlGet)
	if e != nil {
		return nil, e
	}
	t := *old
	t.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag |= unix.CS8
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if e = unix.IoctlSetTermios(fd, ioctlSet, &t); e != nil {
		return nil, e
	}
	return func() { unix.IoctlSetTermios(fd, ioctlSet, old) }, nil
}

func columns() int {
	if w, e := unix.IoctlGetWinsize(int(os.Stderr.Fd()), unix.TIOCGWINSZ); e == nil && w.Col > 0 {
		return int(w.Col)
	}
	return 80
}
