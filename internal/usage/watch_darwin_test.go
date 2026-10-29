package usage

import (
	"ccrouter/internal/store"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileEventsIndexAppendedUsage(t *testing.T) {
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a, e := s.AddAccount("A", "")
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(a.Profile, "projects", "p")
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan error, 32)
	done := make(chan error, 1)
	x := &Indexer{Store: s}
	go func() {
		done <- x.Watch(ctx, func(e error) {
			select {
			case ready <- e:
			default:
			}
		})
	}()
	select {
	case e := <-ready:
		if e != nil {
			t.Fatal(e)
		}
	case e := <-done:
		t.Fatal("watch failed", e)
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not start")
	}
	id := store.NewID()
	line := fmt.Sprintf(`{"type":"assistant","sessionId":%q,"cwd":"/p","timestamp":%q,"message":{"id":"watch","usage":{"input_tokens":3,"output_tokens":7}}}`+"\n", id, time.Now().Format(time.RFC3339Nano))
	if e = os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(line), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.After(8 * time.Second)
	for {
		select {
		case e := <-ready:
			if e != nil {
				t.Fatal(e)
			}
			total, e := s.Totals(a.ID, time.Unix(0, 0), time.Now().Add(time.Second))
			if e != nil {
				t.Fatal(e)
			}
			if total.Total() == 10 {
				cancel()
				if e = <-done; e != nil {
					t.Fatal(e)
				}
				return
			}
		case <-deadline:
			t.Fatal("FSEvents did not index the new transcript")
		}
	}
}
