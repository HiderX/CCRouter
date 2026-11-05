package usage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// process is a running Claude, as its registry entry names it.
type process struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
}

// running lists the Claude processes of profile that are alive now, from the registry
// each process keeps under the profile. That directory is not shared, so it tells which
// account a conversation is running under.
func running(profile string) []process {
	files, _ := filepath.Glob(filepath.Join(profile, "sessions", "*.json"))
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
		if json.Unmarshal(b, &v) == nil && v.SessionID != "" && alive(v.PID) {
			out = append(out, v.process)
		}
	}
	return out
}
