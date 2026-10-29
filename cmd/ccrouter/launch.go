package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccrouter/internal/pick"
	"ccrouter/internal/session"
	"ccrouter/internal/store"
	"ccrouter/internal/usage"
)

// claudeCommands are Claude's own management subcommands. They are not conversations, so
// the launcher does not take a bare one as a prompt.
var claudeCommands = map[string]bool{
	"agents": true, "attach": true, "auth": true, "auto-mode": true, "doctor": true, "gateway": true,
	"import": true, "install": true, "kill": true, "logs": true, "mcp": true, "plugin": true,
	"plugins": true, "project": true, "respawn": true, "rm": true, "setup-token": true, "stop": true,
	"ultrareview": true, "update": true, "upgrade": true,
}

// launch behaves like `claude [args]`: in a terminal it asks which account a new
// conversation uses, and `--resume` without an ID lists conversations from every account.
func launch(s *store.Store, args []string) error {
	account, rest := splitAccount(args)
	if len(rest) > 0 && claudeCommands[rest[0]] {
		return fmt.Errorf("「%s」是 Claude 自己的管理命令，请直接运行 claude %s", rest[0], strings.Join(rest, " "))
	}
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	interactive := pick.Available() && !has(rest, "-p", "--print")
	if from, to, query, ok := resumeWithoutID(rest); ok {
		if !interactive {
			return errors.New("--resume 需要对话 UUID；在终端中直接运行 ccrouter --resume 即可选择")
		}
		v, e := chooseSession(s, account, query)
		if e != nil {
			return e
		}
		rest = append(append(append([]string{}, rest[:from]...), "--resume", v.ID), rest[to:]...)
		cwd = v.CWD
	} else if interactive && account == "" && !has(rest, "-c", "--continue", "-r", "--resume", "--from-pr") {
		a, e := chooseAccount(s)
		if e != nil {
			return e
		}
		account = a.ID
	}
	return start(s, account, rest, cwd)
}

func start(s *store.Store, account string, args []string, cwd string) error {
	p, e := session.Prepare(s, account, args, cwd)
	if e != nil {
		return e
	}
	if e = s.Close(); e != nil {
		return e
	}
	return session.Exec(p)
}

// splitAccount removes CCRouter's --account flag; Claude has no flag of that name.
func splitAccount(args []string) (account string, rest []string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--":
			return account, append(rest, args[i:]...)
		case strings.HasPrefix(a, "--account="):
			account = strings.TrimPrefix(a, "--account=")
		case a == "--account" && i+1 < len(args):
			i++
			account = args[i]
		default:
			rest = append(rest, a)
		}
	}
	return account, rest
}

// has reports whether any flag in names is present, in either `--flag` or `--flag=value` form.
func has(args []string, names ...string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		key, _, _ := strings.Cut(a, "=")
		for _, n := range names {
			if key == n {
				return true
			}
		}
	}
	return false
}

// resumeWithoutID finds `--resume` or `-r` with no session ID. As in Claude, a value that
// is not an ID is a search term. It returns the args span to replace.
func resumeWithoutID(args []string) (from, to int, query string, ok bool) {
	for i, a := range args {
		if a == "--" {
			break
		}
		key, val, eq := strings.Cut(a, "=")
		if key != "--resume" && key != "-r" {
			continue
		}
		switch {
		case eq:
			return i, i + 1, val, !session.IsSessionID(val)
		case i+1 < len(args) && !strings.HasPrefix(args[i+1], "-"):
			return i, i + 2, args[i+1], !session.IsSessionID(args[i+1])
		}
		return i, i + 1, "", true
	}
	return 0, 0, "", false
}

func chooseAccount(s *store.Store) (store.Account, error) {
	as, e := s.Accounts()
	if e != nil {
		return store.Account{}, e
	}
	switch len(as) {
	case 0:
		return store.Account{}, errors.New("还没有账户；运行 ccrouter account add 名称，或 ccrouter account import 登记当前 Claude 登录")
	case 1:
		return as[0], nil
	}
	items, selected := make([]pick.Item, len(as)), 0
	for i, a := range as {
		var tags []string
		if a.Email != "" {
			tags = append(tags, a.Email)
		}
		if a.Default {
			selected = i
			tags = append(tags, "默认")
		}
		in := session.LoggedIn(a.Profile)
		if !in {
			tags = append(tags, "未登录")
		}
		items[i] = pick.Item{Label: a.Name, Detail: strings.Join(tags, " · "), Dim: !in}
	}
	i, e := pick.Run(items, pick.Options{Title: "新对话使用哪个账户", Selected: selected})
	if e != nil {
		return store.Account{}, e
	}
	fmt.Fprintf(os.Stderr, "CCRouter · 使用「%s」\n", as[i].Name)
	return as[i], nil
}

func chooseSession(s *store.Store, account, query string) (store.Session, error) {
	// Keep the list current when the menu bar app is not running. A failed file only
	// leaves that conversation's newest activity out.
	(&usage.Indexer{Store: s}).Scan()
	as, e := s.Accounts()
	if e != nil {
		return store.Session{}, e
	}
	byID := map[string]store.Account{}
	for _, a := range as {
		byID[a.ID] = a
	}
	only := ""
	if account != "" {
		a, e := s.Account(account)
		if e != nil {
			return store.Session{}, e
		}
		only = a.ID
	}
	vs, e := s.Recent("", 300)
	if e != nil {
		return store.Session{}, e
	}
	var items []pick.Item
	var found []store.Session
	for _, v := range vs {
		a := byID[v.AccountID]
		if only != "" && a.ID != only {
			continue
		}
		// No title means no prompt was ever sent, so there is nothing to resume.
		title := usage.Title(a.Profile, v.CWD, v.ID)
		if title == "" {
			continue
		}
		project := filepath.Base(v.CWD)
		if v.CWD == "" {
			project = "未知目录"
		}
		items = append(items, pick.Item{Label: title, Detail: project + " · " + usage.When(time.Unix(v.Updated, 0)) + " · " + a.Name})
		found = append(found, v)
		if len(items) == 100 {
			break
		}
	}
	if len(items) == 0 {
		return store.Session{}, errors.New("没有可恢复的对话")
	}
	i, e := pick.Run(items, pick.Options{Title: "选择要恢复的对话", Query: query, Filter: true})
	if e != nil {
		return store.Session{}, e
	}
	fmt.Fprintf(os.Stderr, "CCRouter · 恢复「%s」· %s · %s\n", items[i].Label, byID[found[i].AccountID].Name, found[i].CWD)
	return found[i], nil
}
