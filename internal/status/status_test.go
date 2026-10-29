package status

import (
	"ccrouter/internal/store"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeClaude answers `claude -p /usage` like Claude Code: it writes the usage cache into
// the profile's .claude.json and records each run.
func fakeClaude(t *testing.T, uuid, cacheUUID string) (runs func() int) {
	t.Helper()
	dir := t.TempDir()
	count := filepath.Join(dir, "runs")
	script := "#!/bin/sh\n" +
		"echo run >> '" + count + "'\n" +
		"now=$(($(date +%s) * 1000))\n" +
		"printf '{\"oauthAccount\":{\"accountUuid\":\"" + uuid + "\"},\"cachedUsageUtilization\":{\"accountUuid\":\"" + cacheUUID + "\",\"fetchedAtMs\":%s,\"utilization\":{\"five_hour\":{\"utilization\":32,\"resets_at\":\"2026-10-29T11:49:59.574619+00:00\"},\"seven_day\":{\"utilization\":54,\"resets_at\":null}}}}' \"$now\" > \"$CLAUDE_CONFIG_DIR/.claude.json\"\n" +
		"echo '{\"is_error\":false,\"num_turns\":0,\"result\":\"ok\"}'\n"
	bin := filepath.Join(dir, "claude")
	if e := os.WriteFile(bin, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("CCROUTER_CLAUDE", bin)
	return func() int {
		b, _ := os.ReadFile(count)
		return strings.Count(string(b), "run")
	}
}

func TestQuotaComesFromClaudeCache(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	os.WriteFile(filepath.Join(a.Profile, ".credentials.json"), []byte("{}"), 0600)
	runs := fakeClaude(t, "acc-1", "acc-1")
	r := New(s)

	q := r.Quota(context.Background(), a, false)
	if q.Error != "" || q.Quota.FiveHour == nil || q.Quota.FiveHour.Utilization != 32 || q.Quota.FiveHour.ResetsAt == nil || q.Quota.SevenDay.Utilization != 54 || runs() != 1 {
		t.Fatalf("first read: %+v runs=%d", q, runs())
	}
	if time.Since(q.Updated) > time.Minute {
		t.Fatal("updated time should be the cache's fetchedAtMs", q.Updated)
	}
	// A fresh cache is read without running Claude; a manual refresh waits forcedRun.
	r.Quota(context.Background(), a, false)
	r.Quota(context.Background(), a, true)
	if runs() != 1 {
		t.Fatal("ran claude again too soon:", runs())
	}
	r.lastRun[a.ID] = time.Now().Add(-2 * forcedRun)
	r.Quota(context.Background(), a, true)
	if runs() != 2 {
		t.Fatal("manual refresh did not run claude:", runs())
	}
}

func TestQuotaIgnoresOtherAccountsCacheAndLoggedOut(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	os.WriteFile(filepath.Join(a.Profile, ".credentials.json"), []byte("{}"), 0600)
	fakeClaude(t, "acc-1", "acc-OTHER")
	if q := New(s).Quota(context.Background(), a, false); q.Error == "" || q.Quota.FiveHour != nil {
		t.Fatal("used a cache written for another account", q)
	}

	b, _ := s.AddAccount("B", "") // never logged in
	runs := fakeClaude(t, "acc-2", "acc-2")
	if q := New(s).Quota(context.Background(), b, false); !strings.Contains(q.Error, "尚未登录") || runs() != 0 {
		t.Fatal("logged-out account", q, runs())
	}
}
func TestIPRejectsInvalidAndCaches(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	r := New(s)
	body, country := `{"ip":"bad"}`, "JP\n"
	calls := 0
	r.Client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host == "ipinfo.io" {
			if req.URL.Path != "/203.0.113.24/country" {
				t.Fatal("unexpected country lookup", req.URL)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(country))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if _, _, e := r.IP(context.Background(), false); e == nil {
		t.Fatal("accepted invalid IP")
	}
	body = `{"ip":"203.0.113.24"}`
	x, _, e := r.IP(context.Background(), false)
	if e != nil || x.IP != "203.0.113.24" || x.Country != "JP" {
		t.Fatal(x, e)
	}
	if x, _, _ = r.IP(context.Background(), false); x.Country != "JP" || calls != 3 {
		t.Fatal("cache missed", x, calls)
	}
	country = "<html>"
	if x, _, e = r.IP(context.Background(), true); e != nil || x.IP != "203.0.113.24" || x.Country != "" {
		t.Fatal("country failure should only hide the country", x, e)
	}
}
func TestIPReadsLegacySnapshot(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	s.SaveSnapshot("ip", []byte("203.0.113.24"))
	r := New(s)
	r.Client.Transport = transport(func(*http.Request) (*http.Response, error) {
		t.Fatal("should use cached snapshot")
		return nil, nil
	})
	if x, _, e := r.IP(context.Background(), false); e != nil || x.IP != "203.0.113.24" {
		t.Fatal(x, e)
	}
}
