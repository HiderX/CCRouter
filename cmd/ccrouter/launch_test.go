package main

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"ccrouter/internal/schedule"
	"ccrouter/internal/session"
	"ccrouter/internal/status"
	"ccrouter/internal/store"
)

func TestSplitAccount(t *testing.T) {
	a, _, rest := splitAccount([]string{"--model", "opus", "--account", "工作", "fix", "--", "--account", "x"})
	if a != "工作" || !reflect.DeepEqual(rest, []string{"--model", "opus", "fix", "--", "--account", "x"}) {
		t.Fatal(a, rest)
	}
	if a, _, _ = splitAccount([]string{"--account=个人"}); a != "个人" {
		t.Fatal(a)
	}
	a, to, rest := splitAccount([]string{"-c", "--switch-account", "备用", "--account=工作"})
	if a != "工作" || to != "备用" || !reflect.DeepEqual(rest, []string{"-c"}) {
		t.Fatal(a, to, rest)
	}
	if _, to, _ = splitAccount([]string{"--switch-account=备用"}); to != "备用" {
		t.Fatal(to)
	}
}
func TestResumeWithoutID(t *testing.T) {
	id := "550e8400-e29b-41d4-a716-446655440000"
	for _, c := range []struct {
		args     []string
		from, to int
		query    string
		ok       bool
	}{
		{[]string{"--resume"}, 0, 1, "", true},
		{[]string{"--model", "x", "-r", "--fork-session"}, 2, 3, "", true},
		{[]string{"--resume", "ip 显示"}, 0, 2, "ip 显示", true},
		{[]string{"--resume=账户"}, 0, 1, "账户", true},
		{[]string{"--resume", id}, 0, 0, "", false},
		{[]string{"-r=" + id}, 0, 0, "", false},
		{[]string{"-c"}, 0, 0, "", false},
		{[]string{"--", "--resume"}, 0, 0, "", false},
	} {
		from, to, q, ok := resumeWithoutID(c.args)
		if ok != c.ok || (ok && (from != c.from || to != c.to || q != c.query)) {
			t.Errorf("%v: got %d %d %q %v", c.args, from, to, q, ok)
		}
	}
}
func TestHas(t *testing.T) {
	if !has([]string{"--print=x"}, "-p", "--print") || has([]string{"--", "-p"}, "-p") || has([]string{"--model"}, "-c") {
		t.Fatal("has")
	}
}

func TestDecide(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	b, _ := s.AddAccount("B", "")
	soon := time.Now().Add(time.Hour)
	quotas := map[string]status.Quota{
		a.ID: {FiveHour: &status.Window{Utilization: 97, ResetsAt: &soon}, SevenDay: &status.Window{Utilization: 10}},
		b.ID: {FiveHour: &status.Window{Utilization: 10, ResetsAt: &soon}, SevenDay: &status.Window{Utilization: 10}},
	}
	cachedQuota = func(x store.Account) status.Result { return status.Result{Quota: quotas[x.ID], Updated: time.Now()} }
	loggedIn = func(string) bool { return true }
	t.Cleanup(func() { cachedQuota, loggedIn = status.Cached, session.LoggedIn })
	c := schedule.Config{Mode: schedule.Auto}

	notice, account, target, assigned := decide(s, c, []string{"fix login"}, "/p")
	if account != b.ID || target != "" || !assigned || !strings.Contains(notice, "自动选择「B」") {
		t.Fatal("new conversation:", notice, account, target, assigned)
	}
	onA := store.NewID()
	s.Bind(store.Session{ID: onA, AccountID: a.ID, CWD: "/p"})
	for _, args := range [][]string{{"--resume", onA}, {"-c"}} {
		notice, account, target, assigned = decide(s, c, args, "/p")
		if target != b.ID || account != "" || !assigned || !strings.Contains(notice, "「A」已达阈值") {
			t.Fatal(args, notice, account, target, assigned)
		}
	}
	// Below its threshold, or with switching on resume off, a conversation stays put.
	onB := store.NewID()
	s.Bind(store.Session{ID: onB, AccountID: b.ID, CWD: "/q"})
	if _, _, target, _ = decide(s, c, []string{"--resume", onB}, "/q"); target != "" {
		t.Fatal("moved a conversation whose account has room")
	}
	sticky := c
	sticky.Sticky = true
	if _, _, target, _ = decide(s, sticky, []string{"--resume", onA}, "/p"); target != "" {
		t.Fatal("moved a conversation with switching on resume off")
	}
	// A fork, or a resume CCRouter cannot name, gets no decision.
	for _, args := range [][]string{{"--resume", onA, "--fork-session"}, {"--resume", store.NewID()}} {
		if n, acc, tgt, _ := decide(s, c, args, "/p"); n != "" || acc != "" || tgt != "" {
			t.Fatal(args, n, acc, tgt)
		}
	}
	// With every account at its threshold the default account is used.
	quotas[b.ID] = quotas[a.ID]
	if notice, account, _, assigned = decide(s, c, nil, "/p"); account != "" || assigned || !strings.Contains(notice, "使用默认账户") {
		t.Fatal(notice, account, assigned)
	}
}

func TestResumeChoices(t *testing.T) {
	now := time.Now()
	soon := now.Add(time.Hour)
	cachedQuota = func(a store.Account) status.Result {
		if a.ID == "c" {
			return status.Result{} // no reading yet
		}
		return status.Result{Quota: status.Quota{FiveHour: &status.Window{Utilization: 33, ResetsAt: &soon}, SevenDay: &status.Window{Utilization: 9}}, Updated: now}
	}
	loggedIn = func(p string) bool { return p != "/c" }
	t.Cleanup(func() { cachedQuota, loggedIn = status.Cached, session.LoggedIn })
	as := []store.Account{{ID: "a", Name: "A", Profile: "/a"}, {ID: "b", Name: "B", Profile: "/b"}, {ID: "c", Name: "C", Profile: "/c"}}
	v := store.Session{ID: "550e8400-e29b-41d4-a716-446655440000", AccountID: "b", CWD: "/p"}

	items, selected, title := resumeChoices(as, v, "", now)
	if selected != 1 || items[1].Detail != "上次使用 · 5 小时 33% · 本周 9%" || title != "这个对话" {
		t.Fatalf("keep: selected %d, %q, %q", selected, items[1].Detail, title)
	}
	if items[2].Detail != "未登录" || !items[2].Dim || items[0].Detail != "5 小时 33% · 本周 9%" {
		t.Fatalf("other rows: %+v", items)
	}
	if _, selected, _ = resumeChoices(as, v, "a", now); selected != 0 {
		t.Fatal("the scheduler's suggestion should be preselected")
	}
	if items, _, _ = resumeChoices(as, v, "a", now); items[0].Detail != "自动调度推荐 · 5 小时 33% · 本周 9%" {
		t.Fatal(items[0].Detail)
	}
}

func TestNewChoices(t *testing.T) {
	now := time.Now()
	cachedQuota = func(a store.Account) status.Result {
		return status.Result{Quota: status.Quota{FiveHour: &status.Window{Utilization: 12}, SevenDay: &status.Window{Utilization: 5}}, Updated: now}
	}
	loggedIn = func(string) bool { return true }
	t.Cleanup(func() { cachedQuota, loggedIn = status.Cached, session.LoggedIn })
	as := []store.Account{{ID: "a", Name: "A", Email: "a@x"}, {ID: "b", Name: "B", Default: true}}

	items, selected := newChoices(as, "", now)
	if selected != 1 || items[1].Detail != "默认 · 5 小时 12% · 本周 5%" || items[0].Detail != "a@x · 5 小时 12% · 本周 5%" {
		t.Fatalf("manual: %d %+v", selected, items)
	}
	items, selected = newChoices(as, "a", now)
	if selected != 0 || items[0].Detail != "a@x · 自动调度推荐 · 5 小时 12% · 本周 5%" {
		t.Fatalf("auto: %d %+v", selected, items)
	}
}
