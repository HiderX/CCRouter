package store

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBindingSurvivesDefaultChangesAndReopen(t *testing.T) {
	root := t.TempDir()
	s, e := Open(root)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := s.AddAccount("A", "")
	b, _ := s.AddAccount("B", "")
	id := NewID()
	if e = s.Bind(Session{ID: id, AccountID: a.ID, CWD: "/project"}); e != nil {
		t.Fatal(e)
	}
	s.SetDefault(b.ID)
	s.Close()
	s, e = Open(root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	v, e := s.Session(id)
	if e != nil || v.AccountID != a.ID {
		t.Fatalf("binding lost: %+v %v", v, e)
	}
	if e = s.Bind(Session{ID: id, AccountID: b.ID}); e == nil {
		t.Fatal("rebound to different account")
	}
}
func TestConcurrentBinding(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	b, _ := s.AddAccount("B", "")
	id := NewID()
	var ok atomic.Int32
	var wg sync.WaitGroup
	for _, ac := range []Account{a, b} {
		wg.Go(func() {
			if s.Bind(Session{ID: id, AccountID: ac.ID}) == nil {
				ok.Add(1)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d accounts bound", ok.Load())
	}
}
func TestUsageDedupAndAccountTotals(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	b, _ := s.AddAccount("B", "")
	now := time.Now()
	rows := []Usage{{AccountID: a.ID, MessageID: "m1", Time: now.Unix(), Totals: Totals{Input: 10, Output: 20, CacheRead: 100, CacheWrite: 30}}, {AccountID: b.ID, MessageID: "m1", Time: now.Unix(), Totals: Totals{Input: 5, Output: 5}}}
	if e = s.CommitUsage("a", "1", 100, rows); e != nil {
		t.Fatal(e)
	}
	rows[0].Output = 40
	if e = s.CommitUsage("a", "1", 200, rows[:1]); e != nil {
		t.Fatal(e)
	}
	total, e := s.Totals("", now.Add(-time.Second), now.Add(time.Second))
	if e != nil {
		t.Fatal(e)
	}
	if total.Total() != 190 || total.Requests != 2 {
		t.Fatalf("double-counted: %+v", total)
	}
	one, _ := s.Totals(a.ID, now.Add(-time.Second), now.Add(time.Second))
	if one.Total() != 180 || one.Requests != 1 {
		t.Fatal(one)
	}
}
func TestRenameAccount(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	s.AddAccount("B", "")
	if e = s.RenameAccount("A", "  工作  "); e != nil {
		t.Fatal(e)
	}
	if got, _ := s.Account(a.ID); got.Name != "工作" || got.Profile != a.Profile {
		t.Fatal("rename changed more than the name", got)
	}
	if e = s.RenameAccount(a.ID, "B"); e == nil || !strings.Contains(e.Error(), "已有") {
		t.Fatal("duplicate name accepted:", e)
	}
	if e = s.RenameAccount(a.ID, " "); e == nil {
		t.Fatal("empty name accepted")
	}
}
