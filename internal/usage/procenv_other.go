//go:build !darwin && !linux

package usage

import "errors"

func processEnv(int, string) (string, bool, error) {
	return "", false, errors.New("reading another process's environment is not supported here")
}
