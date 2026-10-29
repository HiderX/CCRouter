package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureOnboarded(t *testing.T) {
	profile := t.TempDir()
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\necho '9.9.9 (Claude Code)'\n"), 0700)
	path := filepath.Join(profile, ".claude.json")
	os.WriteFile(path, []byte(`{"changelogLastFetched": 1793253223230, "oauthAccount": {"emailAddress": "a<b>@x"}, "migrationVersion": 14}`), 0600)
	if e := EnsureOnboarded(profile, bin); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{`"hasCompletedOnboarding": true`, `"lastOnboardingVersion": "9.9.9"`, `1793253223230`, `"a<b>@x"`, `"migrationVersion": 14`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s in\n%s", want, b)
		}
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0600 {
		t.Fatal("permissions changed", fi.Mode())
	}
	// Already done: the file is left untouched.
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
	EnsureOnboarded(profile, bin)
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(old) {
		t.Fatal("rewrote an onboarded profile")
	}
	// A fresh profile gets a minimal file; a broken one is reported, not replaced.
	fresh := t.TempDir()
	if e := EnsureOnboarded(fresh, bin); e != nil {
		t.Fatal(e)
	}
	var v map[string]any
	b, _ = os.ReadFile(filepath.Join(fresh, ".claude.json"))
	if json.Unmarshal(b, &v) != nil || v["hasCompletedOnboarding"] != true {
		t.Fatal("fresh profile", string(b))
	}
	broken := t.TempDir()
	os.WriteFile(filepath.Join(broken, ".claude.json"), []byte("{not json"), 0600)
	if EnsureOnboarded(broken, bin) == nil {
		t.Fatal("overwrote a file it could not parse")
	}
}
