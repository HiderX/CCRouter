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
	"slices"
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

// root is one real projects directory. Shared profiles all link to the hub's, so it is
// walked once. owner is the account whose profile holds it as a real directory, if any.
type root struct {
	dir   string
	owner store.Account
}

func roots(as []store.Account) []root {
	var out []root
	index := map[string]int{}
	for _, a := range as {
		p := filepath.Join(a.Profile, "projects")
		dir := p
		if r, e := filepath.EvalSymlinks(p); e == nil {
			dir = r
		}
		i, ok := index[dir]
		if !ok {
			i = len(out)
			index[dir] = i
			out = append(out, root{dir: dir})
		}
		if fi, e := os.Lstat(p); e != nil || fi.Mode()&os.ModeSymlink == 0 {
			out[i].owner = a
		}
	}
	return out
}

func (x *Indexer) Scan() error {
	as, e := x.Store.Accounts()
	if e != nil {
		return e
	}
	var errs []error
	for _, r := range roots(as) {
		a := r.owner
		e = filepath.WalkDir(r.dir, func(path string, d fs.DirEntry, err error) error {
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
			errs = append(errs, fmt.Errorf("%s: %w", r.dir, e))
		}
	}
	return errors.Join(errs...)
}

// File indexes new records in one transcript. A conversation counts toward the account
// running it now, else the one it is bound to, else owner, the account whose directory
// holds the file. With no owner, an unbound conversation is not CCRouter's and is skipped.
func (x *Indexer) File(owner store.Account, path string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if r, e := filepath.EvalSymlinks(path); e == nil {
		path = r
	}
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
	as, e := x.Store.Accounts()
	if e != nil {
		return e
	}
	live := map[string][]string{}
	for _, a := range as {
		for _, p := range running(a.Profile) {
			live[p.SessionID] = append(live[p.SessionID], a.ID)
		}
	}
	// A conversation follows the account that last ran it, so its next resume finds that
	// account's prompt cache. While its bound account still runs it, the binding stays.
	owners := map[string]string{}
	follows := map[string]bool{}
	ownerOf := func(id string) string {
		if v, ok := owners[id]; ok {
			return v
		}
		v := owner.ID
		if bound, e := x.Store.Session(id); e == nil {
			v = bound.AccountID
		}
		if ids := live[id]; len(ids) > 0 && !slices.Contains(ids, v) {
			v, follows[id] = ids[0], true
		}
		owners[id] = v
		return v
	}
	r := bufio.NewReaderSize(f, 64*1024)
	var rows []store.Usage
	bindings := map[string]store.Session{}
	committed := offset
	flush := func() error {
		for id, v := range bindings {
			bind := x.Store.Bind
			if follows[id] {
				bind = x.Store.Follow
			}
			if e := bind(v); e != nil {
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
		account := owner.ID
		if v.SessionID != "" {
			account = ownerOf(v.SessionID)
		}
		if account == "" {
			continue
		}
		if v.SessionID != "" {
			bindings[v.SessionID] = store.Session{ID: v.SessionID, AccountID: account, CWD: v.CWD, Updated: ts.Unix()}
		}
		if v.Type == "assistant" && v.Message.ID != "" && v.Message.Usage != nil {
			u := v.Message.Usage
			if u.Input < 0 || u.Output < 0 || u.Read < 0 || u.Write < 0 {
				continue
			}
			rows = append(rows, store.Usage{AccountID: account, MessageID: v.Message.ID, SessionID: v.SessionID, Time: ts.Unix(), Totals: store.Totals{Input: u.Input, Output: u.Output, CacheRead: u.Read, CacheWrite: u.Write}})
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
