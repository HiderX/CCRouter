//go:build darwin || linux

package usage

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestHelperProcess is the stand-in claudeLike starts. A copy of the test binary, unlike
// a system binary such as sleep, lets macOS read its environment, as it does Claude's.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("CCROUTER_HELPER_PROCESS") != "1" {
		t.Skip("helper process")
	}
	time.Sleep(30 * time.Second)
}

// claudeLike starts a process that stands in for Claude running with config dir, or with
// none when dir is "".
func claudeLike(t *testing.T, dir string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "CCROUTER_HELPER_PROCESS=1"}
	if dir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+dir)
	}
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd.Process.Pid
}

func writeRecord(t *testing.T, dir string, pid int, session string) {
	t.Helper()
	os.MkdirAll(dir, 0700)
	if e := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.json", pid)), []byte(fmt.Sprintf(`{"pid":%d,"sessionId":%q}`, pid, session)), 0644); e != nil {
		t.Fatal(e)
	}
}

func sessions(ps []process) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.SessionID)
	}
	slices.Sort(out)
	return out
}

func TestShareRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := filepath.Join(os.Getenv("HOME"), ".claude")
	p := t.TempDir()
	const dead = 999999 // above any PID macOS or Linux hands out by default
	writeRecord(t, filepath.Join(hub, registryDir), dead-1, "hub-stale")
	writeRecord(t, filepath.Join(p, registryDir), dead, "stale")
	os.WriteFile(filepath.Join(p, registryDir, fmt.Sprintf("%d.abc.key", dead)), []byte("k"), 0600)

	if e := ShareRegistry(p); e != nil {
		t.Fatal(e)
	}
	if !same(filepath.Join(p, registryDir), filepath.Join(hub, registryDir)) {
		t.Fatal("registry not linked to the hub")
	}
	es, _ := os.ReadDir(filepath.Join(hub, registryDir))
	if len(es) != 1 || es[0].Name() != fmt.Sprintf("%d.json", dead-1) {
		t.Fatalf("stale records should be dropped and the hub's kept: %v", es)
	}
	if e := ShareRegistry(p); e != nil {
		t.Fatal("not idempotent:", e)
	}

	// A record of a live process, even one without a conversation, makes it wait.
	q := t.TempDir()
	writeRecord(t, filepath.Join(q, registryDir), claudeLike(t, q), "")
	if e := ShareRegistry(q); e == nil {
		t.Fatal("shared a registry a live process still uses")
	}
	if fi, _ := os.Lstat(filepath.Join(q, registryDir)); fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("linked while waiting")
	}
	if e := ShareRegistry(hub); e != nil {
		t.Fatal("the hub shares with itself:", e)
	}
}

func TestRunningAttributesASharedRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := filepath.Join(os.Getenv("HOME"), ".claude")
	reg := filepath.Join(hub, registryDir)
	p, q := t.TempDir(), t.TempDir()
	for _, d := range []string{p, q} {
		if e := ShareRegistry(d); e != nil {
			t.Fatal(e)
		}
	}
	writeRecord(t, reg, claudeLike(t, p), "on-p")
	writeRecord(t, reg, claudeLike(t, q), "on-q")
	writeRecord(t, reg, claudeLike(t, ""), "on-hub")
	writeRecord(t, reg, claudeLike(t, hub), "on-hub-explicit")
	for _, c := range []struct {
		profile string
		want    []string
	}{{p, []string{"on-p"}}, {q, []string{"on-q"}}, {hub, []string{"on-hub", "on-hub-explicit"}}} {
		if got := sessions(running(c.profile)); !slices.Equal(got, c.want) {
			t.Errorf("running(%s) = %v, want %v", c.profile, got, c.want)
		}
	}
}
