package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// sharedDirs hold Claude's per-conversation state. Every account's profile links them to
// the default profile, so any account can resume any conversation without moving files;
// credentials, settings and .claude.json stay per account.
var sharedDirs = []string{"projects", "file-history", "session-env"}

// Hub is the default profile, ~/.claude, where the shared directories live.
func Hub() string {
	h, _ := os.UserHomeDir()
	p := filepath.Join(h, ".claude")
	if r, e := filepath.EvalSymlinks(p); e == nil {
		p = r
	}
	return p
}

// Share links profile's conversation directories to the hub. The first time, what the
// profile recorded on its own is folded into the hub by rename; a name both already hold
// with different content is left in place, and that directory stays unshared.
func Share(profile string) error {
	hub := Hub()
	if same(profile, hub) {
		return nil
	}
	if e := Pending(profile); e != nil {
		return e
	}
	var errs []error
	for _, name := range sharedDirs {
		p, h := filepath.Join(profile, name), filepath.Join(hub, name)
		if t, e := os.Readlink(p); e == nil {
			if !same(t, h) {
				errs = append(errs, fmt.Errorf("%s 已链接到 %s", p, t))
			}
			continue
		}
		if e := os.MkdirAll(h, 0700); e != nil {
			errs = append(errs, e)
			continue
		}
		if _, e := os.Lstat(p); e == nil {
			if e = merge(p, h); e != nil {
				errs = append(errs, e)
				continue
			}
			if e = os.Remove(p); e != nil {
				errs = append(errs, e)
				continue
			}
		}
		if e := os.Symlink(h, p); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}

// Pending explains why Share must wait: a Claude of that account is running and may be
// writing into a directory that still has to be folded. It names their projects.
func Pending(profile string) error {
	if same(profile, Hub()) {
		return nil
	}
	fold := false
	for _, name := range sharedDirs {
		if fi, e := os.Lstat(filepath.Join(profile, name)); e == nil && fi.Mode()&os.ModeSymlink == 0 {
			fold = true
		}
	}
	ps := running(profile)
	if !fold || len(ps) == 0 {
		return nil
	}
	var names []string
	for _, p := range ps {
		if n := filepath.Base(p.CWD); p.CWD != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	which := ""
	if len(names) > 0 {
		which = "（" + strings.Join(names, "、") + "）"
	}
	return fmt.Errorf("还有 %d 个 Claude 在运行%s；全部退出后，下次用 ccrouter 启动时自动合并", len(ps), which)
}

// Shared reports whether every conversation directory of profile is the hub's.
func Shared(profile string) bool {
	hub := Hub()
	for _, name := range sharedDirs {
		if r, e := filepath.EvalSymlinks(filepath.Join(profile, name)); e != nil || !same(r, filepath.Join(hub, name)) {
			return false
		}
	}
	return true
}

func merge(src, dst string) error {
	es, e := os.ReadDir(src)
	if e != nil {
		return e
	}
	var errs []error
	for _, en := range es {
		s, d := filepath.Join(src, en.Name()), filepath.Join(dst, en.Name())
		di, e := os.Lstat(d)
		switch {
		case errors.Is(e, os.ErrNotExist):
			e = os.Rename(s, d)
		case e != nil:
		case en.IsDir() && di.IsDir():
			if e = merge(s, d); e == nil {
				e = os.Remove(s)
			}
		case !en.IsDir() && di.Mode().IsRegular() && sameContent(s, d):
			e = os.Remove(s)
		default:
			e = fmt.Errorf("%s 与 %s 同名但内容不同，未合并", s, d)
		}
		if e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
func sameContent(a, b string) bool {
	x, e := os.ReadFile(a)
	if e != nil {
		return false
	}
	y, e := os.ReadFile(b)
	return e == nil && bytes.Equal(x, y)
}
func same(a, b string) bool {
	if r, e := filepath.EvalSymlinks(a); e == nil {
		a = r
	}
	if r, e := filepath.EvalSymlinks(b); e == nil {
		b = r
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// registryDir is where each running Claude records itself: its conversation, working
// directory and messaging socket. Claude finds the sessions it can message there.
const registryDir = "sessions"

// registrySupported is whether a process's config directory can be read here, which
// telling accounts apart in a shared registry needs.
const registrySupported = runtime.GOOS == "darwin" || runtime.GOOS == "linux"

// ShareRegistry links profile's session registry to the hub's, so the sessions of every
// account can find and message each other. Switching conversations does not need it, so
// callers may ignore its error. It waits, with an error, while any process still keeps
// a record there; with none, the records left are stale and are dropped, as Claude itself
// would.
func ShareRegistry(profile string) error {
	hub := Hub()
	if !registrySupported || same(profile, hub) {
		return nil
	}
	p, h := filepath.Join(profile, registryDir), filepath.Join(hub, registryDir)
	if t, e := os.Readlink(p); e == nil {
		if !same(t, h) {
			return fmt.Errorf("%s 已链接到 %s", p, t)
		}
		return nil
	}
	if e := os.MkdirAll(h, 0700); e != nil {
		return e
	}
	es, e := os.ReadDir(p)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	// A process without a conversation, such as a background host, also keeps a record.
	for _, en := range es {
		if pid, ok := recordPID(en.Name()); ok && alive(pid) {
			return fmt.Errorf("进程 %d 仍在使用 %s；它退出后，下次用 ccrouter 启动时自动共享会话登记", pid, p)
		}
	}
	for _, en := range es {
		if e = os.RemoveAll(filepath.Join(p, en.Name())); e != nil {
			return e
		}
	}
	if e = os.Remove(p); e != nil && !errors.Is(e, os.ErrNotExist) {
		return e
	}
	return os.Symlink(h, p)
}

// recordPID reads the process ID a registry file is named for: <pid>.json, or
// <pid>.<hash>.key for its messaging key.
func recordPID(name string) (int, bool) {
	head, _, _ := strings.Cut(name, ".")
	pid, e := strconv.Atoi(head)
	return pid, e == nil && pid > 0
}

// process is a running Claude, as its registry entry names it.
type process struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

// configDirOf names the config directory a Claude process runs with: CLAUDE_CONFIG_DIR,
// or ~/.claude without it. ok is false when the process's environment cannot be read.
var configDirOf = func(pid int) (dir string, ok bool) {
	v, found, e := processEnv(pid, "CLAUDE_CONFIG_DIR")
	switch {
	case e != nil:
		return "", false
	case !found || v == "":
		return Hub(), true
	}
	return v, true
}

// running lists the Claude processes of profile that are alive now. A profile whose
// registry is still its own folder holds only its own processes. A shared registry, the
// hub's, holds every account's, so each process is attributed by its own config
// directory; that is how CCRouter tells which account runs a conversation.
func running(profile string) []process {
	dir := filepath.Join(profile, registryDir)
	fi, e := os.Lstat(dir)
	own := e == nil && fi.IsDir() && !same(profile, Hub())
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var out []process
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			continue
		}
		var v struct {
			PID int `json:"pid"`
			process
		}
		if json.Unmarshal(b, &v) != nil || v.SessionID == "" || !alive(v.PID) {
			continue
		}
		if !own {
			if d, ok := configDirOf(v.PID); ok && !same(d, profile) {
				continue
			}
		}
		out = append(out, v.process)
	}
	return out
}
