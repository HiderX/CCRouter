package session

import (
	"ccrouter/internal/store"
	"ccrouter/internal/usage"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) (*store.Store, store.Account, store.Account) {
	t.Helper()
	// Launches share profiles with ~/.claude; keep that inside the test.
	t.Setenv("HOME", t.TempDir())
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
func TestSwitchAccountRebindsWithoutTouchingFiles(t *testing.T) {
	s, a, b := setup(t)
	cwd := t.TempDir()
	p, e := Prepare(s, a.ID, nil, cwd)
	if e != nil {
		t.Fatal(e)
	}
	id := p.SessionID
	src := usage.TranscriptPath(a.Profile, cwd, id)
	os.MkdirAll(filepath.Dir(src), 0700)
	body := fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":%q,"timestamp":%q,"message":{"id":"msg-1","usage":{"input_tokens":10,"output_tokens":5}}}`+"\n", id, cwd, time.Now().Format(time.RFC3339Nano))
	os.WriteFile(src, []byte(body), 0600)
	before, _ := os.Stat(src)

	q, e := PrepareSwitch(s, b.Name, []string{"--resume", id, "--model", "opus"}, "/elsewhere")
	if e != nil {
		t.Fatal(e)
	}
	if q.Account.ID != b.ID || q.SessionID != id || q.CWD != cwd || strings.Join(q.Args, " ") != "--resume "+id+" --model opus" {
		t.Fatal(q)
	}
	if v, _ := s.Session(id); v.AccountID != b.ID {
		t.Fatal("binding did not follow the switch")
	}
	// Both accounts see the same file: same inode, unchanged.
	for _, ac := range []store.Account{a, b} {
		fi, e := os.Stat(usage.TranscriptPath(ac.Profile, cwd, id))
		if e != nil || !os.SameFile(fi, before) || fi.Size() != before.Size() {
			t.Fatalf("%s does not see the one transcript: %v", ac.Name, e)
		}
	}
	if c, e := Prepare(s, "", []string{"-c"}, cwd); e != nil || c.Account.ID != b.ID {
		t.Fatal("--continue did not follow the switch", c, e)
	}
	if e = (&usage.Indexer{Store: s}).Scan(); e != nil {
		t.Fatal(e)
	}
	span := func(id string) store.Totals {
		v, _ := s.Totals(id, time.Now().Add(-time.Minute), time.Now().Add(time.Minute))
		return v
	}
	if span(a.ID).Requests != 1 || span(b.ID).Requests != 0 {
		t.Fatal(span(a.ID), span(b.ID))
	}
	// Later replies count toward the account now running the conversation.
	f, _ := os.OpenFile(src, os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprintf(f, `{"type":"assistant","sessionId":%q,"cwd":%q,"timestamp":%q,"message":{"id":"msg-2","usage":{"input_tokens":1,"output_tokens":1}}}`+"\n", id, cwd, time.Now().Format(time.RFC3339Nano))
	f.Close()
	if e = (&usage.Indexer{Store: s}).Scan(); e != nil {
		t.Fatal(e)
	}
	if span(a.ID).Requests != 1 || span(b.ID).Requests != 1 {
		t.Fatal(span(a.ID), span(b.ID))
	}
	for _, args := range [][]string{nil, {"--resume", id, "--fork-session"}} {
		if _, e = PrepareSwitch(s, a.ID, args, cwd); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, e = PrepareSwitch(s, b.ID, []string{"--resume", id}, cwd); e == nil {
		t.Fatal("switched to the same account")
	}
}
func TestShareFoldsExistingConversations(t *testing.T) {
	s, a, b := setup(t)
	// Recorded before sharing existed: a real directory in the profile.
	old := filepath.Join(b.Profile, "projects", "-p", "x.jsonl")
	os.MkdirAll(filepath.Dir(old), 0700)
	os.WriteFile(old, []byte("{}\n"), 0600)
	clash := filepath.Join(b.Profile, "file-history", "same")
	os.MkdirAll(clash, 0700)
	os.WriteFile(filepath.Join(clash, "1"), []byte("b"), 0600)
	hub := filepath.Join(os.Getenv("HOME"), ".claude", "file-history", "same")
	os.MkdirAll(hub, 0700)
	os.WriteFile(filepath.Join(hub, "1"), []byte("hub"), 0600)
	q, e := Prepare(s, b.ID, nil, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if q.Unshared == nil || !strings.Contains(q.Unshared.Error(), "内容不同") {
		t.Fatal("launch did not say why the account is not shared:", q.Unshared)
	}
	if fi, e := os.Lstat(filepath.Join(b.Profile, "projects")); e != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("projects not shared")
	}
	if got, _ := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-p", "x.jsonl")); string(got) != "{}\n" {
		t.Fatal("conversation lost while folding")
	}
	// A clash is left alone, and switching refuses rather than guessing.
	if got, _ := os.ReadFile(filepath.Join(clash, "1")); string(got) != "b" {
		t.Fatal("clashing file overwritten")
	}
	if usage.Shared(b.Profile) {
		t.Fatal("reported shared despite a clash")
	}
	// Folding waits while a Claude of that account is running.
	c, _ := s.AddAccount("C", "")
	os.MkdirAll(filepath.Join(c.Profile, "projects"), 0700)
	os.MkdirAll(filepath.Join(c.Profile, "sessions"), 0700)
	os.WriteFile(filepath.Join(c.Profile, "sessions", "1.json"), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":"x","cwd":"/work/Hetero-Edge-Ops"}`, os.Getpid())), 0600)
	if e := usage.Share(c.Profile); e == nil || usage.Shared(c.Profile) || !strings.Contains(e.Error(), "（Hetero-Edge-Ops）") {
		t.Fatal("folded a profile with a running Claude, or did not name it:", e)
	}
	if usage.Pending(c.Profile) == nil {
		t.Fatal("menu would offer switching from a busy account")
	}
	// Once that Claude exits, the next launch folds.
	os.Remove(filepath.Join(c.Profile, "sessions", "1.json"))
	os.WriteFile(filepath.Join(c.Profile, ".credentials.json"), []byte("{}"), 0600)
	if q, e := Prepare(s, c.ID, nil, t.TempDir()); e != nil || q.Unshared != nil || !usage.Shared(c.Profile) {
		t.Fatal("did not fold after Claude exited:", e, q.Unshared)
	}
	p, _ := Prepare(s, a.ID, nil, t.TempDir())
	if _, e := PrepareSwitch(s, b.ID, []string{"--resume", p.SessionID}, t.TempDir()); e == nil || !strings.Contains(e.Error(), "共享") {
		t.Fatal(e)
	}
}
func TestSwitchAcrossSeveralAccounts(t *testing.T) {
	s, a, b := setup(t)
	c, e := s.AddAccount("C", "")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(c.Profile, ".credentials.json"), []byte("{}"), 0600)
	cwd := t.TempDir()
	p, e := Prepare(s, a.ID, nil, cwd)
	if e != nil {
		t.Fatal(e)
	}
	// A → B → C → A: one conversation and one binding, always the latest account.
	for _, to := range []store.Account{b, c, a} {
		q, e := PrepareSwitch(s, to.Name, []string{"--resume", p.SessionID}, cwd)
		if e != nil {
			t.Fatal(to.Name, e)
		}
		if v, _ := s.Session(p.SessionID); q.Account.ID != to.ID || q.SessionID != p.SessionID || v.AccountID != to.ID {
			t.Fatal(q, v)
		}
	}
	if vs, _ := s.Recent("", 10); len(vs) != 1 {
		t.Fatal(vs)
	}
}
