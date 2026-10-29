package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTitle(t *testing.T) {
	profile := t.TempDir()
	dir := filepath.Join(profile, "projects", "-work-my-app")
	os.MkdirAll(dir, 0700)
	write := func(id string, lines ...string) {
		os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0600)
	}
	write("prompt",
		`{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>x</local-command-caveat>"}}`,
		`{"type":"user","message":{"role":"user","content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"user","message":{"role":"user","content":"  修复 IP\n显示不全  "}}`,
		`{"type":"user","message":{"role":"user","content":"later prompt"}}`)
	write("titled",
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"ai-title","aiTitle":"Old title","sessionId":"titled"}`,
		`{"type":"ai-title","aiTitle":"Fix account switching","sessionId":"titled"}`)
	if got := Title(profile, "/work/my_app", "prompt"); got != "修复 IP 显示不全" {
		t.Errorf("prompt title = %q", got)
	}
	if got := Title(profile, "/elsewhere", "titled"); got != "Fix account switching" {
		t.Errorf("ai title via fallback lookup = %q", got)
	}
	if got := Title(profile, "/work/my_app", "missing"); got != "" {
		t.Errorf("missing transcript = %q", got)
	}

	// Large transcripts: the newest ai-title is read from the tail.
	big := strings.Repeat(`{"type":"assistant","message":{"content":"`+strings.Repeat("x", 1000)+`"}}`+"\n", 600)
	os.WriteFile(filepath.Join(dir, "big.jsonl"), []byte(`{"type":"user","message":{"content":"start"}}`+"\n"+`{"type":"ai-title","aiTitle":"Early"}`+"\n"+big+`{"type":"ai-title","aiTitle":"Late"}`+"\n"), 0600)
	if got := Title(profile, "/work/my_app", "big"); got != "Late" {
		t.Errorf("tail ai title = %q", got)
	}
	if got := clean(strings.Repeat("长", 40)); len([]rune(got)) != 32 || !strings.HasSuffix(got, "…") {
		t.Errorf("clean did not truncate: %q", got)
	}
}
