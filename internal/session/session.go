// Package session chooses credentials before Claude starts; it never handles model traffic.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ccrouter/internal/store"
	"ccrouter/internal/usage"
)

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsSessionID reports whether s is a full conversation UUID, as Claude names them.
func IsSessionID(s string) bool { return uuid.MatchString(s) }

type Plan struct {
	Binary         string
	Args, Env      []string
	CWD, SessionID string
	Account        store.Account
	// Unshared says why this account cannot switch conversations yet; nil once it can.
	Unshared error
}

func ClaudeBinary() (string, error) {
	if p := os.Getenv("CCROUTER_CLAUDE"); p != "" {
		return exec.LookPath(p)
	}
	if p, e := exec.LookPath("claude"); e == nil {
		return p, nil
	}
	h, _ := os.UserHomeDir()
	for _, p := range []string{filepath.Join(h, ".local/bin/claude"), "/opt/homebrew/bin/claude", "/usr/local/bin/claude"} {
		if f, e := os.Stat(p); e == nil && !f.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("未找到 Claude Code，请安装后重试，或设置 CCROUTER_CLAUDE")
}
func Environment(profile string, base []string) []string {
	// A shell API key must not silently override the selected OAuth account.
	blocked := map[string]bool{"CLAUDE_CONFIG_DIR": true, "CLAUDE_SECURESTORAGE_CONFIG_DIR": true, "ANTHROPIC_API_KEY": true, "ANTHROPIC_AUTH_TOKEN": true, "CLAUDE_CODE_OAUTH_TOKEN": true, "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR": true, "CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR": true}
	out := make([]string, 0, len(base)+1)
	for _, v := range base {
		k, _, _ := strings.Cut(v, "=")
		if !blocked[k] {
			out = append(out, v)
		}
	}
	// The imported default profile uses Claude's original, unhashed Keychain service.
	if IsDefaultProfile(profile) {
		return out
	}
	return append(out, "CLAUDE_CONFIG_DIR="+profile)
}
func hasCredentialsFile(profile string) bool {
	_, e := os.Stat(filepath.Join(profile, ".credentials.json"))
	return e == nil
}
func IsDefaultProfile(profile string) bool {
	h, _ := os.UserHomeDir()
	p := filepath.Join(h, ".claude")
	if r, e := filepath.EvalSymlinks(p); e == nil {
		p = r
	}
	return filepath.Clean(profile) == p
}

func Prepare(s *store.Store, account string, args []string, cwd string) (Plan, error) {
	return prepare(s, account, "", args, cwd)
}

// PrepareSwitch resumes a conversation under account target instead of its own. The
// conversation is bound to target from then on; no file is copied or moved.
func PrepareSwitch(s *store.Store, target string, args []string, cwd string) (Plan, error) {
	return prepare(s, "", target, args, cwd)
}

// request is what Claude's arguments ask of a launch; rest is everything else.
type request struct {
	resume, newID string
	cont, fork    bool
	rest          []string
}

func parse(args []string) (request, error) {
	var r request
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, eq := strings.Cut(arg, "=")
		switch key {
		case "--resume", "-r", "--session-id":
			if !eq {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return r, fmt.Errorf("%s 需要明确的对话 UUID；可用 ccrouter sessions 查看", key)
				}
				i++
				val = args[i]
			}
			if !uuid.MatchString(val) {
				return r, errors.New("自动账户绑定需要完整的对话 UUID")
			}
			if key == "--session-id" {
				r.newID = val
			} else {
				r.resume = val
			}
		case "--continue", "-c":
			r.cont = true
		case "--fork-session":
			r.fork = true
		case "--":
			r.rest = append(r.rest, args[i:]...)
			i = len(args)
		default:
			r.rest = append(r.rest, arg)
		}
	}
	if r.cont && r.resume != "" {
		return r, errors.New("--continue 与 --resume 不能同时使用")
	}
	return r, nil
}

// Resuming names the conversation args resume, without preparing a launch: an explicit
// ID, or for --continue the latest one in cwd. A fork starts a new conversation, so it
// is not a resume here.
func Resuming(s *store.Store, args []string, cwd string) (store.Session, bool) {
	r, e := parse(args)
	if e != nil || r.fork {
		return store.Session{}, false
	}
	id := r.resume
	if r.cont {
		vs, e := s.Recent(cwd, 1)
		if e != nil || len(vs) == 0 {
			return store.Session{}, false
		}
		id = vs[0].ID
	}
	if id == "" {
		return store.Session{}, false
	}
	v, e := s.Session(id)
	return v, e == nil
}

func prepare(s *store.Store, account, target string, args []string, cwd string) (Plan, error) {
	var p Plan
	r, e := parse(args)
	if e != nil {
		return p, e
	}
	resume, newID, cont, fork, rest := r.resume, r.newID, r.cont, r.fork, r.rest
	if cont {
		vs, e := s.Recent(cwd, 1)
		if e != nil {
			return p, e
		}
		if len(vs) == 0 {
			return p, errors.New("当前目录没有已绑定的对话")
		}
		resume = vs[0].ID
	}
	if fork && resume == "" {
		return p, errors.New("--fork-session 需要 --resume 或 --continue")
	}
	if resume != "" && !fork && newID != "" {
		return p, errors.New("恢复对话不能覆盖原对话 ID")
	}
	if target != "" && (resume == "" || fork) {
		return p, errors.New("--switch-account 只用于 --resume 或 -c，不能与 --fork-session 同用")
	}
	var a, from store.Account
	var source store.Session
	if target != "" {
		source, e = s.Session(resume)
		if e != nil {
			return p, fmt.Errorf("对话 %s 尚未索引；请先执行 ccrouter scan: %w", resume, e)
		}
		if from, e = s.Account(source.AccountID); e != nil {
			return p, e
		}
		if a, e = s.Account(target); e != nil {
			return p, e
		}
		if a.ID == from.ID {
			return p, fmt.Errorf("该对话已经属于账户「%s」", a.Name)
		}
		p.SessionID = resume
		p.Args = []string{"--resume", resume}
		// Claude looks for the conversation under its own project directory.
		if source.CWD != "" {
			cwd = source.CWD
		}
	} else if resume != "" {
		v, err := s.Session(resume)
		if err != nil {
			return p, fmt.Errorf("对话 %s 尚未索引；请先执行 ccrouter scan: %w", resume, err)
		}
		a, e = s.Account(v.AccountID)
		if e != nil {
			return p, e
		}
		if account != "" {
			want, err := s.Account(account)
			if err != nil {
				return p, err
			}
			if want.ID != a.ID {
				return p, errors.New("该对话已绑定其他账户；请使用原账户恢复")
			}
		}
		p.SessionID = resume
		p.Args = []string{"--resume", resume}
		if fork {
			if newID == "" {
				newID = store.NewID()
			}
			p.SessionID = newID
			p.Args = append(p.Args, "--fork-session", "--session-id", newID)
		}
	} else {
		a, e = s.Account(account)
		if e != nil {
			return p, e
		}
		if newID == "" {
			newID = store.NewID()
		}
		p.SessionID = newID
		p.Args = []string{"--session-id", newID}
	}
	// Resolve executable before committing a binding, so missing installations leave no phantom session.
	if e = checkAuthOverrides(a.Profile, cwd); e != nil {
		return p, e
	}
	p.Binary, e = ClaudeBinary()
	if e != nil {
		return p, e
	}
	// Otherwise Claude would open its own login flow inside the conversation.
	if !LoggedIn(a.Profile) {
		return p, fmt.Errorf("账户「%s」尚未登录，请先运行 ccrouter account login %q，或用 --account 选择其他账户", a.Name, a.Name)
	}
	// A failure here only means Claude shows its first-run setup; it does not block the launch.
	_ = EnsureOnboarded(a.Profile, p.Binary)
	// Sharing lets this account resume conversations from the others. A profile that
	// cannot share yet still launches; only switching needs it.
	p.Unshared = usage.Share(a.Profile)
	if target != "" {
		if e = handOff(s, from, a, source); e != nil {
			return p, e
		}
	}
	p.Account = a
	p.CWD = cwd
	p.Env = Environment(a.Profile, os.Environ())
	p.Args = append(p.Args, rest...)
	if e = s.Bind(store.Session{ID: p.SessionID, AccountID: a.ID, CWD: cwd}); e != nil {
		return p, e
	}
	return p, nil
}

// User/project settings can override environment authentication. Refuse known
// conflicts rather than launching an apparently pinned session under an API key.
func checkAuthOverrides(profile, cwd string) error {
	providers := []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"}
	for _, key := range providers {
		if v := os.Getenv(key); v != "" && v != "0" {
			return fmt.Errorf("%s 启用了第三方提供商；此入口只支持 Claude 账户", key)
		}
	}
	keys := append(providers, "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR")
	paths := []string{filepath.Join(profile, "settings.json"), filepath.Join(cwd, ".claude", "settings.json"), filepath.Join(cwd, ".claude", "settings.local.json")}
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		var v struct {
			Env          map[string]string `json:"env"`
			APIKeyHelper string            `json:"apiKeyHelper"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return fmt.Errorf("无法检查 %s 的认证配置", path)
		}
		if v.APIKeyHelper != "" {
			return fmt.Errorf("%s 设置了 apiKeyHelper，可能覆盖账户选择，请先移除此配置", path)
		}
		for _, key := range keys {
			if value := v.Env[key]; value != "" && value != "0" {
				return fmt.Errorf("%s 中的 %s 会覆盖账户隔离，请先移除此配置", path, key)
			}
		}
	}
	return nil
}

// ReadIdentity invokes Claude's local auth-status command, never printing token material.
func ReadIdentity(ctx context.Context, a store.Account) (email, plan string, err error) {
	bin, e := ClaudeBinary()
	if e != nil {
		return "", "", e
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "auth", "status")
	cmd.Env = Environment(a.Profile, os.Environ())
	data, e := cmd.Output()
	if e != nil {
		return "", "", errors.New("账户尚未登录，请完成 Claude 登录")
	}
	var v struct {
		LoggedIn         bool   `json:"loggedIn"`
		Email            string `json:"email"`
		SubscriptionType string `json:"subscriptionType"`
		AuthMethod       string `json:"authMethod"`
	}
	if e = json.Unmarshal(data, &v); e != nil {
		return "", "", e
	}
	if !v.LoggedIn {
		return "", "", errors.New("账户尚未登录")
	}
	if v.AuthMethod != "claude.ai" && v.AuthMethod != "oauth_token" {
		return "", "", fmt.Errorf("当前认证方式为 %s，需要 Claude 订阅账户", v.AuthMethod)
	}
	return v.Email, v.SubscriptionType, nil
}
func Login(s *store.Store, a store.Account) error {
	bin, e := ClaudeBinary()
	if e != nil {
		return e
	}
	cmd := exec.Command(bin, "auth", "login")
	cmd.Env = Environment(a.Profile, os.Environ())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e = cmd.Run(); e != nil {
		return e
	}
	email, plan, e := ReadIdentity(context.Background(), a)
	if e != nil {
		return e
	}
	if e = EnsureOnboarded(a.Profile, bin); e != nil {
		return e
	}
	return s.UpdateAccount(a.ID, email, plan)
}
