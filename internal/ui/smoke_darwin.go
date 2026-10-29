package ui

import (
	"encoding/json"
	"fmt"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/objc"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// smoke exercises the real AppKit target/action callbacks. It is reachable only in demo mode.
func (a *application) smoke(dir string) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		fmt.Fprintln(os.Stderr, e)
		a.app.Terminate(nil)
		return
	}
	go func() {
		for i := 0; i < 24; i++ {
			done := make(chan struct{})
			mainThread(func() {
				// Click a disclosure control, letting its associated native action invoke Go.
				var walk func(appkit.View) bool
				walk = func(v appkit.View) bool {
					for _, child := range children(v) {
						if objc.Call[string](child, objc.Sel("accessibilityLabel")) == "展开或折叠账户" {
							appkit.ButtonFrom(child.Ptr()).PerformClick(nil)
							return true
						}
						if walk(child) {
							return true
						}
					}
					return false
				}
				walk(a.controller.View())
				if i%4 == 0 {
					a.pop.Close()
					a.show()
				}
				close(done)
			})
			<-done
			time.Sleep(60 * time.Millisecond)
		}
		done := make(chan struct{})
		mainThread(func() {
			a.expanded["0"] = true
			a.render()
			a.exportSnapshot(filepath.Join(dir, "expanded.png"))
			a.expanded["0"] = false
			a.render()
			a.exportSnapshot(filepath.Join(dir, "collapsed.png"))
			saved := a.data
			a.data = snapshot{}
			a.render()
			a.exportSnapshot(filepath.Join(dir, "empty.png"))
			a.data = saved
			a.render()
			a.pop.Close()
			close(done)
		})
		<-done
		// Let deferred native releases settle before measuring the idle phase.
		time.Sleep(3 * time.Second)
		var before, after syscall.Rusage
		syscall.Getrusage(syscall.RUSAGE_SELF, &before)
		start := time.Now()
		time.Sleep(10 * time.Second)
		syscall.Getrusage(syscall.RUSAGE_SELF, &after)
		cpu := func(v syscall.Rusage) float64 {
			return float64(v.Utime.Sec+v.Stime.Sec) + float64(v.Utime.Usec+v.Stime.Usec)/1e6
		}
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		rss, _ := exec.Command("/bin/ps", "-p", strconv.Itoa(os.Getpid()), "-o", "rss=").Output()
		kb, _ := strconv.ParseInt(strings.TrimSpace(string(rss)), 10, 64)
		report := map[string]any{"mode": "isolated demo; 24 disclosure actions and 6 reopen cycles, panel closed", "idle_seconds": time.Since(start).Seconds(), "idle_cpu_percent": (cpu(after) - cpu(before)) / time.Since(start).Seconds() * 100, "rss_bytes": kb * 1024, "go_heap_bytes": mem.HeapAlloc, "goroutines": runtime.NumGoroutine(), "go_version": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
		b, _ := json.MarshalIndent(report, "", "  ")
		os.WriteFile(filepath.Join(dir, "performance.json"), b, 0600)
		fmt.Println(string(b))
		mainThread(func() { a.app.Terminate(nil) })
	}()
}
