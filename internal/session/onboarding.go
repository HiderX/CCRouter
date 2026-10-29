package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnsureOnboarded marks Claude's first-run setup as done in a separate profile that has a
// login. `claude auth login` stores credentials but leaves the setup pending, so the first
// interactive start would show it again, including a login step, as if the account had
// never signed in. Only two keys are added; the rest of Claude's file is kept as is.
func EnsureOnboarded(profile, binary string) error {
	if IsDefaultProfile(profile) {
		return nil // ~/.claude keeps its setup state in ~/.claude.json, which Claude manages
	}
	path := filepath.Join(profile, ".claude.json")
	v := map[string]any{}
	b, e := os.ReadFile(path)
	switch {
	case errors.Is(e, fs.ErrNotExist):
	case e != nil:
		return e
	default:
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber() // keep large integers exact
		if e = dec.Decode(&v); e != nil {
			return e
		}
	}
	if done, _ := v["hasCompletedOnboarding"].(bool); done {
		return nil
	}
	v["hasCompletedOnboarding"] = true
	if out, e := exec.Command(binary, "--version").Output(); e == nil {
		if f := strings.Fields(string(out)); len(f) > 0 {
			v["lastOnboardingVersion"] = f[0]
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if e = enc.Encode(v); e != nil {
		return e
	}
	tmp := path + ".ccrouter-tmp"
	if e = os.WriteFile(tmp, buf.Bytes(), 0600); e != nil {
		return e
	}
	return os.Rename(tmp, path)
}
