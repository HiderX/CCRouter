package session

import (
	"ccrouter/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (*store.Store, store.Account, store.Account) {
	t.Helper()
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	a, e := s.AddAccount("A", "")
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.AddAccount("B", "")
	if e != nil {
		t.Fatal(e)
	}
	for _, ac := range []store.Account{a, b} {
		if e = os.WriteFile(filepath.Join(ac.Profile, ".credentials.json"), []byte("{}"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	bin := filepath.Join(t.TempDir(), "claude")
	if e = os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CCROUTER_CLAUDE", bin)
	return s, a, b
}
func TestResumeUsesOriginalAccount(t *testing.T) {
	s, a, b := setup(t)
	p, e := Prepare(s, "", []string{"hello"}, "/project")
	if e != nil {
		t.Fatal(e)
	}
	s.SetDefault(b.ID)
	q, e := Prepare(s, "", []string{"--resume", p.SessionID}, "/project")
	if e != nil {
		t.Fatal(e)
	}
	if q.Account.ID != a.ID || !strings.Contains(strings.Join(q.Env, "\n"), a.Profile) {
		t.Fatal("wrong account selected")
	}
	if _, e = Prepare(s, b.ID, []string{"--resume", p.SessionID}, "/project"); e == nil {
		t.Fatal("accepted cross-account resume")
	}
}
func TestContinueAndFork(t *testing.T) {
	s, a, b := setup(t)
	p, e := Prepare(s, a.ID, nil, "/one")
	if e != nil {
		t.Fatal(e)
	}
	Prepare(s, b.ID, nil, "/two")
	s.SetDefault(b.ID)
	q, e := Prepare(s, "", []string{"-c", "--fork-session"}, "/one")
	if e != nil {
		t.Fatal(e)
	}
	if q.Account.ID != a.ID || q.SessionID == p.SessionID {
		t.Fatal(q)
	}
	if !strings.Contains(strings.Join(q.Args, " "), "--resume "+p.SessionID) {
		t.Fatal(q.Args)
	}
}
func TestEnvironmentIsolatesOnlyAuth(t *testing.T) {
	env := Environment("/profiles/a", []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/bad", "CLAUDE_SECURESTORAGE_CONFIG_DIR=/bad", "ANTHROPIC_API_KEY=secret", "HTTPS_PROXY=http://proxy:7890", "ANTHROPIC_BASE_URL=https://existing.example"})
	all := strings.Join(env, "\n")
	if strings.Contains(all, "secret") || strings.Contains(all, "/bad") {
		t.Fatal("authentication override retained")
	}
	if !strings.Contains(all, "HTTPS_PROXY=http://proxy:7890") || !strings.Contains(all, "ANTHROPIC_BASE_URL=https://existing.example") {
		t.Fatal("existing networking changed")
	}
	if !strings.Contains(all, "CLAUDE_CONFIG_DIR=/profiles/a") {
		t.Fatal(env)
	}
}
func TestRejectAmbiguousResume(t *testing.T) {
	s, _, _ := setup(t)
	for _, args := range [][]string{{"--resume"}, {"--resume", "name"}, {"--continue", "--resume", store.NewID()}, {"--fork-session"}} {
		if _, e := Prepare(s, "", args, "/project"); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestProfileAuthOverrideRejected(t *testing.T) {
	s, a, _ := setup(t)
	if e := os.WriteFile(filepath.Join(a.Profile, "settings.json"), []byte(`{"env":{"ANTHROPIC_API_KEY":"fixture-key"}}`), 0600); e != nil {
		t.Fatal(e)
	}
	_, e := Prepare(s, a.ID, nil, t.TempDir())
	if e == nil {
		t.Fatal("settings silently overrode selected account")
	}
	if strings.Contains(e.Error(), "fixture-key") {
		t.Fatal("credential leaked in error")
	}
}
func TestRefuseAccountWithoutCredentials(t *testing.T) {
	s, _, _ := setup(t)
	c, e := s.AddAccount("C", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Prepare(s, c.ID, nil, "/project"); e == nil || !strings.Contains(e.Error(), "尚未登录") {
		t.Fatal("launched an account that never logged in:", e)
	}
	if v, _ := s.Recent("", 10); len(v) != 0 {
		t.Fatal("refused launch left a session binding", v)
	}
}
