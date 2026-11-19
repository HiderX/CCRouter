package usage

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// processEnv reads one environment variable of a running process of the same user.
func processEnv(pid int, key string) (value string, found bool, err error) {
	b, e := unix.SysctlRaw("kern.procargs2", pid)
	if e != nil {
		return "", false, e
	}
	return procArgsEnv(b, key)
}

// procArgsEnv parses KERN_PROCARGS2: argc, the executable path and its padding, argv,
// then the environment, each string NUL-terminated.
func procArgsEnv(b []byte, key string) (string, bool, error) {
	if len(b) < 4 {
		return "", false, errors.New("short procargs")
	}
	argc := int(binary.LittleEndian.Uint32(b))
	parts := bytes.Split(b[4:], []byte{0})
	i := 1 // past the executable path
	for i < len(parts) && len(parts[i]) == 0 {
		i++
	}
	i += argc
	prefix := []byte(key + "=")
	for ; i < len(parts) && len(parts[i]) > 0; i++ {
		if bytes.HasPrefix(parts[i], prefix) {
			return string(parts[i][len(prefix):]), true, nil
		}
	}
	return "", false, nil
}
