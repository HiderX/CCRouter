//go:build !darwin

package ui

import (
	"ccrouter/internal/store"
	"errors"
)

func Run(_ *store.Store, _ bool) error {
	return errors.New("此版本提供 macOS 菜单栏；当前平台可使用 ccrouter 的命令行功能")
}
