// Package store owns the on-disk index. It never stores OAuth credentials or prompts.
package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	DB   *sql.DB
	Root string
}
type Account struct {
	ID, Name, Profile, Email, Plan string
	Default                        bool
}
type Session struct {
	ID, AccountID, CWD string
	Updated            int64
}
type Totals struct{ Input, Output, CacheRead, CacheWrite, Requests int64 }

func (t Totals) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }
func (t Totals) HitRate() float64 {
	n := t.Input + t.CacheRead + t.CacheWrite
	if n == 0 {
		return 0
	}
	return float64(t.CacheRead) * 100 / float64(n)
}

type Usage struct {
	AccountID, MessageID, SessionID string
	Time                            int64
	Totals
}

func DefaultRoot() (string, error) {
	if p := os.Getenv("CCROUTER_HOME"); p != "" {
		return filepath.Abs(p)
	}
	p, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(p, "CCRouter"), nil
}
func Open(root string) (*Store, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	path := filepath.Join(root, "state.db")
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	db, e := sql.Open("sqlite3", u.String()+"?_busy_timeout=5000&_foreign_keys=on&_journal_mode=DELETE")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db, root}
	_, e = db.Exec(`
CREATE TABLE IF NOT EXISTS accounts(id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,profile TEXT NOT NULL UNIQUE,email TEXT NOT NULL DEFAULT '',plan TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,account_id TEXT NOT NULL REFERENCES accounts(id),cwd TEXT NOT NULL,updated INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS sessions_cwd ON sessions(cwd,updated DESC);
CREATE TABLE IF NOT EXISTS usage(account_id TEXT NOT NULL REFERENCES accounts(id),message_id TEXT NOT NULL,session_id TEXT NOT NULL,time INTEGER NOT NULL,input INTEGER NOT NULL,output INTEGER NOT NULL,cache_read INTEGER NOT NULL,cache_write INTEGER NOT NULL,PRIMARY KEY(account_id,message_id));
CREATE INDEX IF NOT EXISTS usage_time ON usage(time,account_id);
CREATE TABLE IF NOT EXISTS cursors(path TEXT PRIMARY KEY,identity TEXT NOT NULL,offset INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS snapshots(key TEXT PRIMARY KEY,value BLOB NOT NULL,updated INTEGER NOT NULL);
PRAGMA user_version=1;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func NewID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func (s *Store) Accounts() ([]Account, error) {
	r, e := s.DB.Query(`SELECT a.id,a.name,a.profile,a.email,a.plan,a.id=COALESCE((SELECT value FROM settings WHERE key='default_account'),'') FROM accounts a ORDER BY rowid`)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out []Account
	for r.Next() {
		var a Account
		if e = r.Scan(&a.ID, &a.Name, &a.Profile, &a.Email, &a.Plan, &a.Default); e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, r.Err()
}
func (s *Store) Account(id string) (Account, error) {
	as, e := s.Accounts()
	if e != nil {
		return Account{}, e
	}
	for _, a := range as {
		if (id == "" && a.Default) || a.ID == id || a.Name == id {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("未找到账户 %q，请先添加账户", id)
}
func (s *Store) AddAccount(name, profile string) (Account, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Account{}, errors.New("账户名称不能为空")
	}
	a := Account{ID: NewID(), Name: name}
	if profile == "" {
		profile = filepath.Join(s.Root, "profiles", a.ID)
	}
	var e error
	a.Profile, e = filepath.Abs(profile)
	if e != nil {
		return a, e
	}
	if e = os.MkdirAll(a.Profile, 0700); e != nil {
		return a, e
	}
	if resolved, err := filepath.EvalSymlinks(a.Profile); err == nil {
		a.Profile = resolved
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return a, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO accounts(id,name,profile) VALUES(?,?,?)`, a.ID, a.Name, a.Profile); e != nil {
		return a, e
	}
	_, e = tx.Exec(`INSERT OR IGNORE INTO settings(key,value) VALUES('default_account',?)`, a.ID)
	if e != nil {
		return a, e
	}
	e = tx.Commit()
	return a, e
}
func (s *Store) SetDefault(id string) error {
	a, e := s.Account(id)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec(`INSERT INTO settings(key,value) VALUES('default_account',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, a.ID)
	return e
}

// RenameAccount changes only the display name; profiles, credentials and bindings use the ID.
func (s *Store) RenameAccount(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("账户名称不能为空")
	}
	a, e := s.Account(id)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec(`UPDATE accounts SET name=? WHERE id=?`, name, a.ID)
	if e != nil && strings.Contains(e.Error(), "UNIQUE") {
		return fmt.Errorf("已有名为「%s」的账户", name)
	}
	return e
}
func (s *Store) UpdateAccount(id, email, plan string) error {
	_, e := s.DB.Exec(`UPDATE accounts SET email=?,plan=? WHERE id=?`, email, plan, id)
	return e
}

// Bind is immutable across accounts, including when two launchers race.
func (s *Store) Bind(v Session) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var account string
	e = tx.QueryRow(`SELECT account_id FROM sessions WHERE id=?`, v.ID).Scan(&account)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if e == nil && account != v.AccountID {
		return fmt.Errorf("对话 %s 已绑定其他账户，拒绝自动换号", v.ID)
	}
	if v.Updated == 0 {
		v.Updated = time.Now().Unix()
	}
	_, e = tx.Exec(`INSERT INTO sessions(id,account_id,cwd,updated) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET updated=MAX(sessions.updated,excluded.updated),cwd=CASE WHEN excluded.cwd='' THEN sessions.cwd ELSE excluded.cwd END`, v.ID, v.AccountID, v.CWD, v.Updated)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Session(id string) (Session, error) {
	var v Session
	e := s.DB.QueryRow(`SELECT id,account_id,cwd,updated FROM sessions WHERE id=?`, id).Scan(&v.ID, &v.AccountID, &v.CWD, &v.Updated)
	return v, e
}
func (s *Store) Recent(cwd string, limit int) ([]Session, error) {
	r, e := s.DB.Query(`SELECT id,account_id,cwd,updated FROM sessions WHERE (?='' OR cwd=?) ORDER BY updated DESC LIMIT ?`, cwd, cwd, limit)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	var out []Session
	for r.Next() {
		var v Session
		if e = r.Scan(&v.ID, &v.AccountID, &v.CWD, &v.Updated); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, r.Err()
}
func (s *Store) Totals(account string, start, end time.Time) (Totals, error) {
	var t Totals
	e := s.DB.QueryRow(`SELECT COALESCE(SUM(input),0),COALESCE(SUM(output),0),COALESCE(SUM(cache_read),0),COALESCE(SUM(cache_write),0),COUNT(*) FROM usage WHERE time>=? AND time<? AND (?='' OR account_id=?)`, start.Unix(), end.Unix(), account, account).Scan(&t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &t.Requests)
	return t, e
}
func (s *Store) Trend(start time.Time, buckets int, step time.Duration) ([]Totals, error) {
	out := make([]Totals, buckets)
	r, e := s.DB.Query(`SELECT (time-?)/?,SUM(input),SUM(output),SUM(cache_read),SUM(cache_write),COUNT(*) FROM usage WHERE time>=? AND time<? GROUP BY 1`, start.Unix(), int64(step.Seconds()), start.Unix(), start.Add(time.Duration(buckets)*step).Unix())
	if e != nil {
		return nil, e
	}
	defer r.Close()
	for r.Next() {
		var i int
		var t Totals
		if e = r.Scan(&i, &t.Input, &t.Output, &t.CacheRead, &t.CacheWrite, &t.Requests); e != nil {
			return nil, e
		}
		if i >= 0 && i < len(out) {
			out[i] = t
		}
	}
	return out, r.Err()
}
func (s *Store) Cursor(path string) (identity string, offset int64, err error) {
	err = s.DB.QueryRow(`SELECT identity,offset FROM cursors WHERE path=?`, path).Scan(&identity, &offset)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return
}

// CommitUsage commits both records and the byte offset, so crashes cannot lose data.
func (s *Store) CommitUsage(path, identity string, offset int64, rows []Usage) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	stmt, e := tx.Prepare(`INSERT INTO usage(account_id,message_id,session_id,time,input,output,cache_read,cache_write) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(account_id,message_id) DO UPDATE SET input=MAX(usage.input,excluded.input),output=MAX(usage.output,excluded.output),cache_read=MAX(usage.cache_read,excluded.cache_read),cache_write=MAX(usage.cache_write,excluded.cache_write)`)
	if e != nil {
		return e
	}
	defer stmt.Close()
	for _, u := range rows {
		if _, e = stmt.Exec(u.AccountID, u.MessageID, u.SessionID, u.Time, u.Input, u.Output, u.CacheRead, u.CacheWrite); e != nil {
			return e
		}
	}
	_, e = tx.Exec(`INSERT INTO cursors(path,identity,offset) VALUES(?,?,?) ON CONFLICT(path) DO UPDATE SET identity=excluded.identity,offset=excluded.offset`, path, identity, offset)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Snapshot(key string) ([]byte, time.Time, error) {
	var b []byte
	var ts int64
	e := s.DB.QueryRow(`SELECT value,updated FROM snapshots WHERE key=?`, key).Scan(&b, &ts)
	return b, time.Unix(ts, 0), e
}
func (s *Store) SaveSnapshot(key string, b []byte) error {
	_, e := s.DB.Exec(`INSERT INTO snapshots(key,value,updated) VALUES(?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated=excluded.updated`, key, b, time.Now().Unix())
	return e
}
