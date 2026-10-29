// Package usage indexes only locally recorded usage; it is not a billing ledger.
package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ccrouter/internal/store"
)

type Indexer struct {
	Store *store.Store
	mu    sync.Mutex
}
type record struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		ID    string `json:"id"`
		Usage *struct {
			Input  int64 `json:"input_tokens"`
			Output int64 `json:"output_tokens"`
			Read   int64 `json:"cache_read_input_tokens"`
			Write  int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (x *Indexer) Scan() error {
	as, e := x.Store.Accounts()
	if e != nil {
		return e
	}
	var errs []error
	for _, a := range as {
		base := filepath.Join(a.Profile, "projects")
		e = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
				if err := x.File(a, path); err != nil {
					errs = append(errs, err)
				}
			}
			return nil
		})
		if e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Name, e))
		}
	}
	return errors.Join(errs...)
}
func (x *Indexer) File(a store.Account, path string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	f, e := os.Open(path)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return e
	}
	if !fi.Mode().IsRegular() {
		return nil
	}
	// The identity changes on replacement; truncation also restarts parsing. Usage is deduplicated in SQLite.
	identity := fileIdentity(fi)
	old, offset, e := x.Store.Cursor(path)
	if e != nil {
		return e
	}
	if old != identity || offset > fi.Size() {
		offset = 0
	}
	if offset == fi.Size() {
		return nil
	}
	if _, e = f.Seek(offset, io.SeekStart); e != nil {
		return e
	}
	r := bufio.NewReaderSize(f, 64*1024)
	var rows []store.Usage
	bindings := map[string]store.Session{}
	committed := offset
	flush := func() error {
		for _, v := range bindings {
			if e := x.Store.Bind(v); e != nil {
				return e
			}
		}
		if e := x.Store.CommitUsage(path, identity, committed, rows); e != nil {
			return e
		}
		rows = nil
		bindings = map[string]store.Session{}
		return nil
	}
	for {
		line, err := readLine(r)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		committed += int64(len(line))
		var v record
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if e = json.Unmarshal(line, &v); e != nil {
			return fmt.Errorf("%s，字节 %s: 会话记录格式错误", filepath.Base(path), strconv.FormatInt(committed-int64(len(line)), 10))
		}
		ts, e := time.Parse(time.RFC3339Nano, v.Timestamp)
		if e != nil {
			continue
		}
		if v.SessionID != "" {
			bindings[v.SessionID] = store.Session{ID: v.SessionID, AccountID: a.ID, CWD: v.CWD, Updated: ts.Unix()}
		}
		if v.Type == "assistant" && v.Message.ID != "" && v.Message.Usage != nil {
			u := v.Message.Usage
			if u.Input < 0 || u.Output < 0 || u.Read < 0 || u.Write < 0 {
				continue
			}
			rows = append(rows, store.Usage{AccountID: a.ID, MessageID: v.Message.ID, SessionID: v.SessionID, Time: ts.Unix(), Totals: store.Totals{Input: u.Input, Output: u.Output, CacheRead: u.Read, CacheWrite: u.Write}})
		}
		if len(rows) >= 256 || len(bindings) >= 128 {
			if e = flush(); e != nil {
				return e
			}
		}
	}
	return flush()
}

// A final partial line is retried on the next event. Bound memory even for large tool results.
func readLine(r *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		p, e := r.ReadSlice('\n')
		if len(out)+len(p) > 32*1024*1024 {
			return nil, errors.New("单条会话记录超过 32 MiB，停止索引此文件")
		}
		out = append(out, p...)
		if e == bufio.ErrBufferFull {
			continue
		}
		return out, e
	}
}
