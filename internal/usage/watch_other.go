//go:build !darwin

package usage

import (
	"context"
	"github.com/fsnotify/fsnotify"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (x *Indexer) Watch(ctx context.Context, changed func(error)) error {
	w, e := fsnotify.NewWatcher()
	if e != nil {
		return e
	}
	defer w.Close()
	add := func(root string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				w.Add(p)
			}
			return nil
		})
	}
	as, e := x.Store.Accounts()
	if e != nil {
		return e
	}
	for _, a := range as {
		add(a.Profile)
	}
	changed(x.Scan())
	for {
		select {
		case <-ctx.Done():
			return nil
		case e := <-w.Errors:
			changed(e)
		case ev := <-w.Events:
			if ev.Has(fsnotify.Create) {
				if f, e := os.Stat(ev.Name); e == nil && f.IsDir() {
					add(ev.Name)
					changed(x.Scan())
					continue
				}
			}
			if !strings.HasSuffix(ev.Name, ".jsonl") {
				continue
			}
			for _, a := range as {
				if strings.HasPrefix(ev.Name, filepath.Join(a.Profile, "projects")+string(filepath.Separator)) {
					changed(x.File(a, ev.Name))
					break
				}
			}
		}
	}
}
