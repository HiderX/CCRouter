//go:build darwin || linux

package session

import (
	"os"
	"syscall"
)

// Exec replaces the launcher. Claude retains the terminal, signals and exit status.
// A resumed conversation runs in its own project directory, wherever it was picked from.
func Exec(p Plan) error {
	if p.CWD != "" {
		if e := os.Chdir(p.CWD); e != nil {
			return e
		}
	}
	return syscall.Exec(p.Binary, append([]string{p.Binary}, p.Args...), p.Env)
}
