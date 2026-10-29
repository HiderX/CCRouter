package main

import (
	"ccrouter/internal/store"
	"ccrouter/internal/ui"
	"fmt"
	"os"
	"runtime"
)

func init() { runtime.LockOSThread() }
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "CCRouter:", e)
		os.Exit(1)
	}
}
func run() error {
	args := os.Args[1:]
	demo := len(args) > 0 && args[0] == "demo"
	root, e := store.DefaultRoot()
	if e != nil {
		return e
	}
	if demo {
		if len(args) == 3 && args[1] == "--snapshot" {
			os.Setenv("CCROUTER_SNAPSHOT", args[2])
		}
		if len(args) == 3 && args[1] == "--smoke" {
			os.Setenv("CCROUTER_SMOKE", args[2])
		}
		root, e = os.MkdirTemp("", "ccrouter-demo-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(root)
	}
	s, e := store.Open(root)
	if e != nil {
		return e
	}
	defer s.Close()
	return ui.Run(s, demo)
}
