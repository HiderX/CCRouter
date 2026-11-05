package schedule

import (
	"strings"
	"testing"
	"time"

	"ccrouter/internal/status"
	"ccrouter/internal/store"
)

var now = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// account builds a logged-in account with a reading: 5-hour and weekly use, and the time
// until each window resets (0: the window is not running).
func account(id string, five float64, fiveIn time.Duration, week float64, weekIn time.Duration) State {
	w := func(used float64, in time.Duration) *status.Window {
		v := &status.Window{Utilization: used}
		if in != 0 {
			at := now.Add(in)
			v.ResetsAt = &at
		}
		return v
	}
	return State{Account: store.Account{ID: id, Name: id}, Quota: status.Quota{FiveHour: w(five, fiveIn), SevenDay: w(week, weekIn)}, Read: true, LoggedIn: true}
}

const day = 24 * time.Hour

// The panel's example: 个人 has reached its 5-hour threshold, 工作 is close to its weekly
// one, 备用 has room in both windows.
func example() (Config, []State) {
	c := Config{Mode: Auto}
	c.SetMember("工作", Member{Threshold: 90})
	return c, []State{
		account("个人", 96, 35*time.Minute, 71, 3*day),
		account("工作", 40, 70*time.Minute, 88, 4*day),
		account("备用", 12, 185*time.Minute, 22, 5*day),
	}
}

func TestOptimalKeepsWeeklyHeadroom(t *testing.T) {
	c, states := example()
	to := c.Pick(Optimal, states, "", "", now)
	if to.Account.ID != "备用" {
		t.Fatal(to)
	}
	for _, want := range []string{"两个窗口都充足", "「工作」本周已用 88%，先留着", "「个人」已达阈值"} {
		if !strings.Contains(to.Reason, want) {
			t.Errorf("reason %q lacks %q", to.Reason, want)
		}
	}
}

func TestOptimalSpendsAWindowAboutToReset(t *testing.T) {
	c, states := example()
	states[1] = account("工作", 40, 70*time.Minute, 50, 4*day)
	to := c.Pick(Optimal, states, "", "", now)
	if to.Account.ID != "工作" || !strings.Contains(to.Reason, "5 小时窗口 1 小时 10 分后重置，还剩 50%，先用掉") {
		t.Fatal(to)
	}
}

func TestOptimalStaysOnTheRunningWindow(t *testing.T) {
	// Two idle accounts and one already in its window: spending that window first keeps
	// the others' windows for later.
	c := Config{Mode: Auto}
	states := []State{account("a", 0, 0, 10, 6*day), account("b", 30, 4*time.Hour, 10, 6*day), account("c", 0, 0, 10, 6*day)}
	if to := c.Pick(Optimal, states, "", "", now); to.Account.ID != "b" {
		t.Fatal(to)
	}
}

func TestFiveHourUsesTheWindowThatResetsFirst(t *testing.T) {
	c, states := example()
	to := c.Pick(FiveHour, states, "", "", now)
	if to.Account.ID != "工作" || !strings.Contains(to.Reason, "最早重置（1 小时 10 分后），还剩 50%") {
		t.Fatal(to)
	}
	// With no window running, the most 5-hour headroom wins.
	idle := []State{account("a", 20, 0, 0, 0), account("b", 5, 0, 0, 0)}
	if to := c.Pick(FiveHour, idle, "", "", now); to.Account.ID != "b" {
		t.Fatal(to)
	}
}

func TestRoundRobinFollowsAccountOrder(t *testing.T) {
	c, states := example()
	cases := []struct{ last, want string }{{"", "工作"}, {"工作", "备用"}, {"备用", "工作"}}
	for _, k := range cases {
		to := c.Pick(RoundRobin, states, k.last, "", now)
		if to.Account.ID != k.want {
			t.Errorf("after %q: %v", k.last, to)
		}
	}
	if to := c.Pick(RoundRobin, states, "工作", "", now); !strings.Contains(to.Reason, "上一个对话用了「工作」，这次轮到它") {
		t.Fatal(to.Reason)
	}
	// Leaving an account out of scheduling applies to every strategy.
	c.SetMember("备用", Member{Off: true})
	for _, s := range Strategies {
		if to := c.Pick(s, states, "工作", "", now); to.Account.ID != "工作" {
			t.Errorf("%s picked %v", s, to)
		}
	}
}

func TestAWindowThatResetIsEmpty(t *testing.T) {
	c := Config{Mode: Auto}
	states := []State{account("a", 99, -time.Minute, 30, 2*day)}
	if c.Status(states[0], now) != "" {
		t.Fatal("a window past its reset still counted as full")
	}
	if to := c.Pick(FiveHour, states, "", "", now); to.Account.ID != "a" || !strings.Contains(to.Reason, "没有进行中的 5 小时窗口") {
		t.Fatal(to)
	}
}

func TestNothingUsable(t *testing.T) {
	c := Config{Mode: Auto}
	full := []State{account("a", 97, time.Hour, 10, day), account("b", 10, time.Hour, 99, day)}
	if to := c.Pick(Optimal, full, "", "", now); to.OK() || to.Reason != "参与调度的账户都已达阈值" {
		t.Fatal(to)
	}
	out := []State{account("a", 0, 0, 0, 0)}
	out[0].LoggedIn = false
	if to := c.Pick(Optimal, out, "", "", now); to.OK() || to.Reason != "没有参与调度且已登录的账户" {
		t.Fatal(to)
	}
	if c.Status(out[0], now) != "未登录" {
		t.Fatal(c.Status(out[0], now))
	}
}

func TestUnknownReadingIsSaid(t *testing.T) {
	c := Config{Mode: Auto}
	states := []State{{Account: store.Account{ID: "a", Name: "a"}, LoggedIn: true}}
	if to := c.Pick(Optimal, states, "", "", now); to.Account.ID != "a" || !strings.HasPrefix(to.Reason, "还没有它的额度数据") {
		t.Fatal(to)
	}
}

func TestResume(t *testing.T) {
	c, states := example()
	to, ok := c.Resume(states, "个人", "", now)
	if !ok || to.Account.ID != "备用" || !strings.HasPrefix(to.Reason, "「个人」已达阈值；") {
		t.Fatal(to, ok)
	}
	if _, ok = c.Resume(states, "备用", "", now); ok {
		t.Fatal("switched an account below its threshold")
	}
	for _, k := range []func(*Config){
		func(c *Config) { c.Mode = Manual },
		func(c *Config) { c.Sticky = true },
		func(c *Config) { c.SetMember("个人", Member{Off: true}) },
	} {
		c, _ := example()
		k(&c)
		if _, ok := c.Resume(states, "个人", "", now); ok {
			t.Fatal("switched although switching does not apply", c)
		}
	}
	// No other account can take it: keep the conversation where it is.
	c.SetMember("工作", Member{Off: true})
	c.SetMember("备用", Member{Off: true})
	if _, ok := c.Resume(states, "个人", "", now); ok {
		t.Fatal("switched to nothing")
	}
}

func TestMembers(t *testing.T) {
	var c Config
	if c.Member("x").Limit() != DefaultThreshold || c.Plan() != Optimal || c.Auto() {
		t.Fatal("zero config is not manual with the defaults")
	}
	c.SetMember("x", Member{Threshold: 30})
	if c.Member("x").Limit() != MinThreshold {
		t.Fatal(c.Member("x").Limit())
	}
	c.SetMember("x", Member{Threshold: DefaultThreshold})
	if len(c.Members) != 0 {
		t.Fatal("a member at the defaults was kept", c.Members)
	}
}

func TestLoadSave(t *testing.T) {
	s, e := store.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if c := Load(s); c.Auto() || len(c.Members) != 0 {
		t.Fatal("a fresh store is not in manual mode", c)
	}
	c := Config{Mode: Auto, Strategy: FiveHour, Sticky: true}
	c.SetMember("a", Member{Off: true, Threshold: 80})
	if e = Save(s, c); e != nil {
		t.Fatal(e)
	}
	got := Load(s)
	if !got.Auto() || got.Plan() != FiveHour || !got.Sticky || got.Member("a") != (Member{Off: true, Threshold: 80}) {
		t.Fatal(got)
	}
	if e = SetLast(s, "a"); e != nil || Last(s) != "a" {
		t.Fatal(e, Last(s))
	}
}
