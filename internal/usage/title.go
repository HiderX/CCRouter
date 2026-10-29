package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// titleChunk bounds how much of a transcript is read from each end.
const titleChunk = 256 << 10

// Title returns Claude's own title for a conversation, falling back to its first prompt.
// The transcript is read on demand for display only; nothing is stored.
func Title(profile, cwd, id string) string {
	f, e := os.Open(filepath.Join(profile, "projects", projectDir(cwd), id+".jsonl"))
	if e != nil {
		matches, _ := filepath.Glob(filepath.Join(profile, "projects", "*", id+".jsonl"))
		if len(matches) == 0 {
			return ""
		}
		if f, e = os.Open(matches[0]); e != nil {
			return ""
		}
	}
	defer f.Close()
	// Claude appends a fresh ai-title as the conversation evolves; the newest is near the end.
	if st, e := f.Stat(); e == nil && st.Size() > titleChunk {
		if ai, _ := scanTitle(io.NewSectionReader(f, st.Size()-titleChunk, titleChunk)); ai != "" {
			return ai
		}
	}
	ai, prompt := scanTitle(io.NewSectionReader(f, 0, titleChunk))
	if ai != "" {
		return ai
	}
	return prompt
}

// When formats a conversation's last activity: 今天 15:04, 昨天 15:04, or 01-02 15:04.
func When(t time.Time) string {
	day := func(t time.Time) time.Time { y, m, d := t.Date(); return time.Date(y, m, d, 0, 0, 0, 0, t.Location()) }
	switch day(time.Now()).Sub(day(t)) / (24 * time.Hour) {
	case 0:
		return "今天 " + t.Format("15:04")
	case 1:
		return "昨天 " + t.Format("15:04")
	}
	return t.Format("01-02 15:04")
}

// projectDir mirrors Claude's directory naming: every character outside [A-Za-z0-9] becomes '-'.
func projectDir(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return r
		}
		return '-'
	}, cwd)
}

// scanTitle returns the last ai-title and the first prompt typed by the user in r.
func scanTitle(r io.Reader) (ai, prompt string) {
	br := bufio.NewReader(r)
	for {
		line, e := br.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"ai-title"`)) || (prompt == "" && bytes.Contains(line, []byte(`"user"`))) {
			var v struct {
				Type    string `json:"type"`
				AITitle string `json:"aiTitle"`
				Meta    bool   `json:"isMeta"`
				Side    bool   `json:"isSidechain"`
				Message struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			// A section can start mid-line; such fragments fail to decode and are skipped.
			if json.Unmarshal(line, &v) == nil {
				switch {
				case v.Type == "ai-title" && v.AITitle != "":
					ai = clean(v.AITitle)
				case v.Type == "user" && !v.Meta && !v.Side && prompt == "":
					prompt = clean(typed(v.Message.Content))
				}
			}
		}
		if e != nil {
			return ai, prompt
		}
	}
}

// typed extracts text the user wrote, skipping tool results and command or hook wrappers.
func typed(content json.RawMessage) string {
	var s string
	if json.Unmarshal(content, &s) != nil {
		var blocks []struct {
			Type, Text string
		}
		json.Unmarshal(content, &blocks)
		for _, b := range blocks {
			if b.Type == "text" {
				s = b.Text
				break
			}
		}
	}
	if strings.HasPrefix(strings.TrimSpace(s), "<") {
		return ""
	}
	return s
}
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 32 {
		s = string(r[:31]) + "…"
	}
	return s
}
