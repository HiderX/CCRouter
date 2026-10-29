package pick

import (
	"strings"
	"testing"
)

func TestFilterAndScroll(t *testing.T) {
	items := []Item{{Label: "修复 IP 显示", Detail: "CCRouter · 今天"}, {Label: "审计实施", Detail: "AutoResearch"}, {Label: "Overleaf 密码", Detail: "ubuntu_server"}}
	l := &list{items: items, opt: Options{Rows: 2, Filter: true}}
	l.refilter(2)
	if l.shown[l.cursor] != 2 || l.offset != 1 {
		t.Fatal("initial selection not visible", l.cursor, l.offset)
	}
	l.query = []rune("ccrouter ip")
	l.refilter(-1)
	if len(l.shown) != 1 || l.shown[0] != 0 {
		t.Fatal("multi-term filter", l.shown)
	}
	l.query = []rune("nothing")
	l.refilter(-1)
	l.move(1) // must not panic on an empty list
	if !strings.Contains(strings.Join(l.lines(80), "\n"), "没有匹配") {
		t.Fatal("empty state not drawn")
	}
}
func TestWidthAndFit(t *testing.T) {
	if width("审计 ab") != 7 {
		t.Fatal(width("审计 ab"))
	}
	if got := fit("修复显示不全的问题", 9); width(got) > 9 || !strings.HasSuffix(got, "…") {
		t.Fatalf("fit = %q (%d)", got, width(got))
	}
	for _, line := range (&list{items: []Item{{Label: strings.Repeat("长", 80), Detail: strings.Repeat("x", 200)}}, opt: Options{Title: "选择要恢复的对话", Rows: 5, Filter: true}, query: []rune(strings.Repeat("q", 60))}).lines(40) {
		if w := width(stripANSI(line)); w > 40 {
			t.Fatalf("line wraps a 40-column terminal: %d", w)
		}
	}
}
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && !(s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
