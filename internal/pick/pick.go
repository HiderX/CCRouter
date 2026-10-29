// Package pick is a small interactive list for the terminal, drawn on stderr.
package pick

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrCancelled = errors.New("已取消")

type Item struct {
	Label, Detail string
	Dim           bool // drawn faint, such as an account that is not logged in
}

type Options struct {
	Title    string
	Selected int    // initially highlighted item
	Query    string // initial filter text
	Filter   bool   // typing filters the list; otherwise digits choose an item
	Rows     int    // visible rows; the list scrolls past this
}

// Run lets the user choose one item and returns its index.
// Available must be true; the terminal is restored before Run returns.
func Run(items []Item, o Options) (int, error) {
	restore, e := raw()
	if e != nil {
		return 0, e
	}
	defer restore()
	if o.Rows <= 0 {
		o.Rows = 10
	}
	l := &list{items: items, opt: o, query: []rune(o.Query)}
	l.refilter(o.Selected)
	out := os.Stderr
	fmt.Fprint(out, "\x1b[?25l")
	defer fmt.Fprint(out, "\x1b[?25h")
	drawn := 0
	draw := func(final bool) {
		if drawn > 1 {
			fmt.Fprintf(out, "\x1b[%dA", drawn-1)
		}
		fmt.Fprint(out, "\r\x1b[J")
		if final {
			return
		}
		lines := l.lines(columns())
		fmt.Fprint(out, strings.Join(lines, "\r\n"))
		drawn = len(lines)
	}
	defer draw(true)
	buf := make([]byte, 64)
	for {
		draw(false)
		n, e := os.Stdin.Read(buf)
		if e != nil {
			return 0, e
		}
		switch k := string(buf[:n]); {
		case k == "\x03" || k == "\x1b":
			return 0, ErrCancelled
		case k == "\r" || k == "\n":
			if len(l.shown) > 0 {
				return l.shown[l.cursor], nil
			}
		case k == "\x1b[A" || k == "\x1bOA" || k == "\x10":
			l.move(-1)
		case k == "\x1b[B" || k == "\x1bOB" || k == "\x0e":
			l.move(1)
		case !o.Filter && len(k) == 1 && k[0] >= '1' && k[0] <= '9':
			if i := int(k[0] - '1'); i < len(l.shown) {
				return l.shown[i], nil
			}
		case o.Filter && (k == "\x7f" || k == "\b"):
			if len(l.query) > 0 {
				l.query = l.query[:len(l.query)-1]
				l.refilter(-1)
			}
		case o.Filter && k == "\x15":
			l.query = nil
			l.refilter(-1)
		case o.Filter && k[0] >= ' ' && k[0] != 0x7f && utf8.ValidString(k):
			l.query = append(l.query, []rune(k)...)
			l.refilter(-1)
		}
	}
}

type list struct {
	items          []Item
	opt            Options
	query          []rune
	shown          []int // indexes into items that match the query
	cursor, offset int   // cursor is a position in shown; offset is the first visible row
}

func (l *list) refilter(selected int) {
	terms := strings.Fields(strings.ToLower(string(l.query)))
	l.shown = l.shown[:0]
	l.cursor, l.offset = 0, 0
	for i, it := range l.items {
		text := strings.ToLower(it.Label + " " + it.Detail)
		match := true
		for _, t := range terms {
			match = match && strings.Contains(text, t)
		}
		if match {
			if i == selected {
				l.cursor = len(l.shown)
			}
			l.shown = append(l.shown, i)
		}
	}
	l.move(0)
}
func (l *list) move(d int) {
	if len(l.shown) == 0 {
		return
	}
	l.cursor = (l.cursor + d + len(l.shown)) % len(l.shown)
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+l.opt.Rows {
		l.offset = l.cursor - l.opt.Rows + 1
	}
}

const (
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	accent = "\x1b[36m"
	reset  = "\x1b[0m"
)

func (l *list) lines(cols int) []string {
	hint := "↑↓ 选择 · 回车确认 · Esc 取消"
	if l.opt.Filter {
		hint = "输入以筛选 · " + hint
	} else {
		hint = "数字或 " + hint
	}
	title := fit(l.opt.Title, cols-1)
	lines := []string{bold + title + reset + "  " + dim + fit(hint, cols-width(title)-3) + reset}
	if l.opt.Filter {
		lines = append(lines, accent+"› "+reset+fit(string(l.query), cols-4)+dim+"▏"+reset)
	}
	if len(l.shown) == 0 {
		return append(lines, dim+"  没有匹配的项"+reset)
	}
	// Details (project, time, account) keep their full width when they can; labels get the rest.
	label, detail := 0, 0
	for _, i := range l.shown {
		label = max(label, width(l.items[i].Label))
		detail = max(detail, width(l.items[i].Detail))
	}
	label = min(label, max(12, cols-10-min(detail, cols/2)))
	end := min(len(l.shown), l.offset+l.opt.Rows)
	for row := l.offset; row < end; row++ {
		it := l.items[l.shown[row]]
		mark, style := "  ", ""
		if row == l.cursor {
			mark, style = accent+"❯ "+reset, bold
		}
		if it.Dim {
			style = dim
		}
		if !l.opt.Filter && row < 9 {
			mark += dim + fmt.Sprint(row+1) + " " + reset
		}
		name := fit(it.Label, label)
		rest := max(0, cols-4-label-6)
		lines = append(lines, mark+style+name+reset+strings.Repeat(" ", label-width(name))+"  "+dim+fit(it.Detail, rest)+reset)
	}
	if more := len(l.shown) - end; more > 0 || l.offset > 0 {
		lines = append(lines, dim+fmt.Sprintf("  第 %d–%d 项，共 %d 项", l.offset+1, end, len(l.shown))+reset)
	}
	return lines
}

// width counts terminal columns: East Asian wide characters take two.
func width(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x1100 && (unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) ||
			(r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) || (r >= 0x1f300 && r <= 0x1faff)) {
			n++
		}
	}
	return n
}

// fit truncates s to at most w columns, so no line wraps and the redraw stays aligned.
func fit(s string, w int) string {
	if width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	out := []rune{}
	for _, r := range s {
		if width(string(out))+width(string(r)) > w-1 {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}
