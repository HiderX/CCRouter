package session

import (
	"os"
	"os/exec"
)

func Exec(p Plan) error {
	cmd := exec.Command(p.Binary, p.Args...)
	cmd.Env = p.Env
	cmd.Dir = p.CWD
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
