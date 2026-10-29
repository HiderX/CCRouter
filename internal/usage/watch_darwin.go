package usage

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsevents"
)

// Watch uses one FSEvents stream, not one open file descriptor per transcript.
func (x *Indexer) Watch(ctx context.Context, changed func(error)) error {
	as, e := x.Store.Accounts()
	if e != nil {
		return e
	}
	paths := []string{x.Store.Root}
	for _, a := range as {
		paths = append(paths, a.Profile)
	}
	es := &fsevents.EventStream{Paths: paths, Flags: fsevents.FileEvents | fsevents.WatchRoot, Latency: time.Second, Events: make(chan []fsevents.Event, 64)}
	if e = es.Start(); e != nil {
		return e
	}
	defer es.Stop()
	changed(x.Scan())
	for {
		select {
		case <-ctx.Done():
			return nil
		case events := <-es.Events:
			// External CLI account additions must also join the watch set.
			for _, ev := range events {
				if filepath.Base(ev.Path) == "state.db" {
					current, err := x.Store.Accounts()
					if err != nil {
						changed(err)
						break
					}
					different := len(current) != len(as)
					if !different {
						for i := range as {
							if current[i].ID != as[i].ID || current[i].Profile != as[i].Profile {
								different = true
								break
							}
						}
					}
					if different {
						as = current
						paths = []string{x.Store.Root}
						for _, a := range as {
							paths = append(paths, a.Profile)
						}
						es.Stop()
						es.Paths = paths
						es.Resume = false
						if err = es.Start(); err != nil {
							return err
						}
						changed(x.Scan())
					} else {
						changed(nil)
					}
					break
				}
			}
			seen := map[string]bool{}
			rescan := false
			var last error
			dirty := false
			for _, ev := range events {
				if ev.Flags&(fsevents.MustScanSubDirs|fsevents.UserDropped|fsevents.KernelDropped|fsevents.RootChanged) != 0 {
					rescan = true
				}
				path := filepath.Clean(ev.Path)
				if seen[path] || !strings.HasSuffix(path, ".jsonl") {
					continue
				}
				seen[path] = true
				for _, a := range as {
					if strings.HasPrefix(path, filepath.Join(a.Profile, "projects")+string(filepath.Separator)) {
						dirty = true
						if e = x.File(a, path); e != nil {
							last = e
						}
						break
					}
				}
			}
			if rescan {
				last = x.Scan()
				dirty = true
			}
			if dirty {
				changed(last)
			}
		}
	}
}
