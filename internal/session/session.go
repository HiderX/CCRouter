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
)

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// IsSessionID reports whether s is a full conversation UUID, as Claude names them.
func IsSessionID(s string) bool { return uuid.MatchString(s) }

type Plan struct {
	Binary         string
	Args, Env      []string
	CWD, SessionID string
	Account        store.Account
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
	var p Plan
	var resume, newID string
	var cont, fork bool
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, val, eq := strings.Cut(arg, "=")
		switch key {
		case "--resume", "-r", "--session-id":
			if !eq {
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return p, fmt.Errorf("%s 需要明确的对话 UUID；可用 ccrouter sessions 查看", key)
				}
				i++
				val = args[i]
			}
			if !uuid.MatchString(val) {
				return p, errors.New("自动账户绑定需要完整的对话 UUID")
			}
			if key == "--session-id" {
				newID = val
			} else {
				resume = val
			}
		case "--continue", "-c":
			cont = true
		case "--fork-session":
			fork = true
		case "--":
			rest = append(rest, args[i:]...)
			i = len(args)
		default:
			rest = append(rest, arg)
		}
	}
	if cont && resume != "" {
		return p, errors.New("--continue 与 --resume 不能同时使用")
	}
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
	var a store.Account
	var e error
	if resume != "" {
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
