package ui

import (
	"testing"
	"time"
)

func TestCompact(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 156: "156", 1234: "1.2K", 9_949: "9.9K", 9_950: "10K", 240_000: "240K", 999_499: "999K", 999_500: "1.00M", 2_400_000: "2.40M", 1_500_000_000: "1.50B"} {
		if got := compact(n); got != want {
			t.Errorf("compact(%d) = %q, want %q", n, got, want)
		}
	}
}
func TestRegionAndPlan(t *testing.T) {
	for code, want := range map[string]string{"JP": "日本", "US": "美国", "HK": "香港", "": "", "ZZ": "", "BT": "BT"} {
		if got := region(code); got != want {
			t.Errorf("region(%q) = %q, want %q", code, got, want)
		}
	}
	if planLabel("max") != "Max" || planLabel("") != "" {
		t.Error("plan label")
	}
}
func TestResetIn(t *testing.T) {
	now := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	for _, c := range []struct {
		at   *time.Time
		want string
	}{
		{nil, ""},
		{at(-time.Minute), "已重置"},
		{at(30 * time.Second), "即将重置"},
		{at(25*time.Minute + 30*time.Second), "25 分钟后重置"},
		{at(2 * time.Hour), "2 小时后重置"},
		{at(2*time.Hour + 15*time.Minute), "2 小时 15 分后重置"},
		{at(72 * time.Hour), "3 天后重置"},
		{at(76*time.Hour + 30*time.Minute), "3 天 4 小时后重置"},
	} {
		if got := resetIn(c.at, now); got != c.want {
			t.Errorf("resetIn(%v) = %q, want %q", c.at, got, c.want)
		}
	}
}
