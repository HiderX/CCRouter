package usage

import (
	"bytes"
	"fmt"
	"os"
)

// processEnv reads one environment variable of a running process of the same user.
func processEnv(pid int, key string) (value string, found bool, err error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if e != nil {
		return "", false, e
	}
	prefix := []byte(key + "=")
	for _, kv := range bytes.Split(b, []byte{0}) {
		if bytes.HasPrefix(kv, prefix) {
			return string(kv[len(prefix):]), true, nil
		}
	}
	return "", false, nil
}
