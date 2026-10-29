package status

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ccrouter/internal/session"
	"ccrouter/internal/store"
)

// Quota windows come from Claude Code itself. `claude -p /usage` is a local command: no
// model turn and no tokens. Claude refreshes its login under its own lock, asks Anthropic
// with its own client, and caches the answer in the profile's config file. CCRouter only
// reads that cache, so it never handles OAuth tokens or sends a request of its own.
const (
	quotaFresh  = 5 * time.Minute  // a cache this young is used as is
	runInterval = 5 * time.Minute  // at most one /usage run per account in this time,
	forcedRun   = time.Minute      // or in this time after a manual refresh
	runTimeout  = 45 * time.Second // the same bound CC Switch uses
)

// usageArgs ask only for Claude's own /usage answer: no tools, MCP servers or settings
// sources, and no saved session.
var usageArgs = []string{"-p", "/usage", "--output-format", "json", "--tools", "", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence"}

func (r *Reader) Quota(ctx context.Context, a store.Account, force bool) Result {
	r.mu.Lock()
	defer r.mu.Unlock() // one claude process at a time
	q, at, ok := readCache(a.Profile)
	gap := runInterval
	if force {
		gap = forcedRun
	}
	if (force || !ok || time.Since(at) > quotaFresh || q.passed()) && time.Since(r.lastRun[a.ID]) > gap {
		if !session.LoggedIn(a.Profile) {
			return Result{Quota: q, Updated: at, Error: "该账户尚未登录"}
		}
		r.lastRun[a.ID] = time.Now()
		r.lastError[a.ID] = r.refresh(ctx, a)
		q, at, ok = readCache(a.Profile)
	}
	out := Result{Quota: q, Updated: at}
	switch {
	case !ok && r.lastError[a.ID] != "":
		out.Error = r.lastError[a.ID]
	case !ok:
		out.Error = "额度暂不可用"
	case time.Since(at) > quotaFresh:
		out.Error = r.lastError[a.ID] // a stale reading says why it could not be renewed
	}
	return out
}

// passed reports whether a window has reset since the reading, which makes it out of date.
func (q Quota) passed() bool {
	for _, w := range []*Window{q.FiveHour, q.SevenDay} {
		if w != nil && w.ResetsAt != nil && !w.ResetsAt.After(time.Now()) {
			return true
		}
	}
	return false
}

// configFile is where Claude keeps a profile's account state and usage cache.
func configFile(profile string) string {
	if session.IsDefaultProfile(profile) {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, ".claude.json")
	}
	return filepath.Join(profile, ".claude.json")
}

// readCache accepts only a cache written for the account currently logged in to the
// profile, as Claude Code itself does.
func readCache(profile string) (Quota, time.Time, bool) {
	b, e := os.ReadFile(configFile(profile))
	if e != nil {
		return Quota{}, time.Time{}, false
	}
	var v struct {
		OAuth struct {
			UUID string `json:"accountUuid"`
		} `json:"oauthAccount"`
		Cache *struct {
			UUID        string  `json:"accountUuid"`
			FetchedAtMs float64 `json:"fetchedAtMs"`
			Utilization Quota   `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
	if json.Unmarshal(b, &v) != nil || v.Cache == nil || v.Cache.UUID == "" || v.Cache.UUID != v.OAuth.UUID {
		return Quota{}, time.Time{}, false
	}
	q := v.Cache.Utilization
	if q.FiveHour == nil && q.SevenDay == nil {
		return Quota{}, time.Time{}, false
	}
	return q, time.UnixMilli(int64(v.Cache.FetchedAtMs)), true
}

// refresh runs /usage in the account's environment and returns a short error, if any.
func (r *Reader) refresh(ctx context.Context, a store.Account) string {
	bin, e := session.ClaudeBinary()
	if e != nil {
		return e.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, usageArgs...)
	var env []string
	for _, kv := range session.Environment(a.Profile, os.Environ()) {
		// With it set, /usage stays offline and only repeats an earlier reading.
		if !strings.HasPrefix(kv, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=") {
			env = append(env, kv)
		}
	}
	// No memory folder for the working directory, no telemetry or update check for this run.
	cmd.Env = append(env, "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1", "DISABLE_AUTOUPDATER=1")
	cmd.Dir, _ = os.UserHomeDir()
	out, e := cmd.Output()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "额度刷新超时"
	case e != nil:
		return "额度刷新失败"
	}
	var v struct {
		IsError bool   `json:"is_error"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(out, &v) == nil && v.IsError {
		msg := strings.Join(strings.Fields(v.Result), " ")
		if r := []rune(msg); len(r) > 60 {
			msg = string(r[:60]) + "…"
		}
		return "额度刷新失败：" + msg
	}
	return ""
}
