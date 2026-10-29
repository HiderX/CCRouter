//go:build !darwin && !linux

package pick

import "errors"

// Available is false: this build has no raw-mode terminal support.
func Available() bool { return false }

func raw() (func(), error) { return nil, errors.New("此平台不支持交互选择") }
func columns() int         { return 80 }
