package ui

import "testing"

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
