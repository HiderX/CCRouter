package usage

import (
	"ccrouter/internal/store"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPartialAppendAndRepeatedMessage(t *testing.T) {
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	dir := filepath.Join(a.Profile, "projects", "project")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "test.jsonl")
	id := store.NewID()
	now := time.Now()
	line := func(output int) string {
		return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":"/project","timestamp":%q,"message":{"id":"msg-one","usage":{"input_tokens":10,"output_tokens":%d,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}`, id, now.Format(time.RFC3339Nano), output)
	}
	x := &Indexer{Store: s}
	first := line(5)
	os.WriteFile(path, []byte(first[:len(first)/2]), 0600)
	if e = x.Scan(); e != nil {
		t.Fatal(e)
	}
	total, _ := s.Totals("", now.Add(-time.Second), now.Add(time.Second))
	if total.Requests != 0 {
		t.Fatal("partial line counted")
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprint(f, first[len(first)/2:]+"\n"+line(9)+"\n")
	f.Close()
	if e = x.Scan(); e != nil {
		t.Fatal(e)
	}
	if e = x.Scan(); e != nil {
		t.Fatal(e)
	}
	total, _ = s.Totals("", now.Add(-time.Second), now.Add(time.Second))
	if total.Requests != 1 || total.Total() != 139 {
		t.Fatalf("bad aggregate: %+v", total)
	}
	v, e := s.Session(id)
	if e != nil || v.AccountID != a.ID {
		t.Fatal(v, e)
	}
	// Replacing a transcript does not duplicate its existing messages.
	os.Rename(path, path+".old")
	os.WriteFile(path, []byte(line(9)+"\n"), 0600)
	if e = x.Scan(); e != nil {
		t.Fatal(e)
	}
	total, _ = s.Totals("", now.Add(-time.Second), now.Add(time.Second))
	if total.Requests != 1 {
		t.Fatal(total)
	}
}
func TestMalformedLineIsVisibleAndDoesNotAdvance(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	p := filepath.Join(a.Profile, "bad.jsonl")
	os.WriteFile(p, []byte("{bad}\n"), 0600)
	x := &Indexer{Store: s}
	if x.File(a, p) == nil {
		t.Fatal("malformed record hidden")
	}
	_, offset, _ := s.Cursor(p)
	if offset != 0 {
		t.Fatal("advanced past damaged data")
	}
}
func TestConversationFollowsRunningAccount(t *testing.T) {
	s, _ := store.Open(t.TempDir())
	defer s.Close()
	a, _ := s.AddAccount("A", "")
	b, _ := s.AddAccount("B", "")
	id := store.NewID()
	s.Bind(store.Session{ID: id, AccountID: a.ID, CWD: "/p"})
	path := filepath.Join(a.Profile, "projects", "p", id+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0700)
	now := time.Now()
	os.WriteFile(path, []byte(fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":"/p","timestamp":%q,"message":{"id":"m","usage":{"input_tokens":1,"output_tokens":1}}}`+"\n", id, now.Format(time.RFC3339Nano))), 0600)
	// B resumed it inside Claude: B's registry names this process.
	os.MkdirAll(filepath.Join(b.Profile, "sessions"), 0700)
	os.WriteFile(filepath.Join(b.Profile, "sessions", "1.json"), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":%q}`, os.Getpid(), id)), 0600)
	if e := (&Indexer{Store: s}).Scan(); e != nil {
		t.Fatal(e)
	}
	if v, _ := s.Session(id); v.AccountID != b.ID {
		t.Fatal("binding did not follow the running account")
	}
	if v, _ := s.Totals(b.ID, now.Add(-time.Second), now.Add(time.Second)); v.Requests != 1 {
		t.Fatal(v)
	}
}
