package usage

import "testing"

func TestProcArgsEnv(t *testing.T) {
	b := append([]byte{2, 0, 0, 0}, []byte("/usr/bin/claude\x00\x00\x00claude\x00--resume\x00HOME=/u\x00CLAUDE_CONFIG_DIR=/p/x\x00\x00junk=1\x00")...)
	if v, ok, e := procArgsEnv(b, "CLAUDE_CONFIG_DIR"); e != nil || !ok || v != "/p/x" {
		t.Fatal(v, ok, e)
	}
	// An argument that looks like a variable is not one.
	b = append([]byte{1, 0, 0, 0}, []byte("/bin/sh\x00CLAUDE_CONFIG_DIR=/arg\x00HOME=/u\x00")...)
	if _, ok, _ := procArgsEnv(b, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatal("read argv as the environment")
	}
	if _, _, e := procArgsEnv([]byte{1}, "X"); e == nil {
		t.Fatal("accepted a short buffer")
	}
}
