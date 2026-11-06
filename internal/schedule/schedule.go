// Package schedule decides which account a conversation runs under when CCRouter picks
// automatically. It reads only quota readings Claude already cached; it starts nothing.
package schedule

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"ccrouter/internal/status"
	"ccrouter/internal/store"
)

const (
	Manual = "manual"
	Auto   = "auto"

	Optimal    = "optimal"
	RoundRobin = "roundrobin"
	FiveHour   = "fivehour"

	// Thresholds are percentages of a quota window, set in steps of Step.
	DefaultThreshold = 95
	MinThreshold     = 50
	MaxThreshold     = 100
	Step             = 5
)

// Strategies lists the automatic strategies in the order the panel shows them.
var Strategies = []string{Optimal, RoundRobin, FiveHour}

// Name is a strategy's short label.
func Name(strategy string) string {
	switch strategy {
	case RoundRobin:
		return "轮询"
	case FiveHour:
		return "5h 最大利用"
	}
	return "5h/7d 最优"
}

// Describe says in one sentence what a strategy does.
func Describe(strategy string) string {
	switch strategy {
	case RoundRobin:
		return "新对话在参与调度的账户之间轮流，跳过已达阈值的"
	case FiveHour:
		return "只看 5 小时窗口：先用最早重置、还有余量的账户，用满再换"
	}
	return "先用快过期的额度：周额度优先，5 小时窗口其次"
}

// Member is one account's part in automatic scheduling.
type Member struct {
	Off       bool `json:"off,omitempty"`       // left out of scheduling
	Threshold int  `json:"threshold,omitempty"` // percent of either window that counts as full; 0 is the default
}

// Limit is the member's threshold, the default when unset.
func (m Member) Limit() int {
	if m.Threshold == 0 {
		return DefaultThreshold
	}
	return min(MaxThreshold, max(MinThreshold, m.Threshold))
}

// Config is the scheduling setup. Its zero value is manual mode.
type Config struct {
	Mode     string            `json:"mode,omitempty"`
	Strategy string            `json:"strategy,omitempty"`
	Sticky   bool              `json:"sticky,omitempty"` // a resumed conversation keeps its account past its threshold
	Members  map[string]Member `json:"members,omitempty"`
}

func (c Config) Auto() bool { return c.Mode == Auto }

// Plan is the strategy in effect; an unknown one falls back to Optimal.
func (c Config) Plan() string {
	if slices.Contains(Strategies, c.Strategy) {
		return c.Strategy
	}
	return Optimal
}
func (c Config) Member(id string) Member { return c.Members[id] }

// SetMember stores m for account id, dropping the entry when it says nothing.
func (c *Config) SetMember(id string, m Member) {
	if m.Threshold == DefaultThreshold {
		m.Threshold = 0
	}
	if c.Members == nil {
		c.Members = map[string]Member{}
	}
	if m == (Member{}) {
		delete(c.Members, id)
		return
	}
	c.Members[id] = m
}

const (
	configKey = "schedule"
	lastKey   = "schedule_last"
)

// Load reads the saved setup. A missing or unreadable one is manual mode.
func Load(s *store.Store) Config {
	var c Config
	if v, e := s.Setting(configKey); e == nil && v != "" {
		if json.Unmarshal([]byte(v), &c) != nil {
			c = Config{}
		}
	}
	return c
}
func Save(s *store.Store, c Config) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	return s.SetSetting(configKey, string(b))
}

// Last is the account the scheduler gave the latest conversation; round robin goes on
// from there.
func Last(s *store.Store) string {
	v, _ := s.Setting(lastKey)
	return v
}
func SetLast(s *store.Store, id string) error { return s.SetSetting(lastKey, id) }

// State is what the scheduler knows about an account.
type State struct {
	Account  store.Account
	Quota    status.Quota // the latest reading Claude cached
	Read     bool         // whether there is a reading at all
	LoggedIn bool
}

// window is a quota window as of now. A window whose reset has passed is empty again,
// and has no reset time until it is used.
type window struct {
	used  float64
	reset time.Duration // 0: no window running
}

func current(w *status.Window, now time.Time) window {
	if w == nil {
		return window{}
	}
	if w.ResetsAt == nil {
		return window{used: w.Utilization}
	}
	d := w.ResetsAt.Sub(now)
	if d <= 0 {
		return window{}
	}
	return window{used: w.Utilization, reset: d}
}

// candidate is an account as one decision sees it.
type candidate struct {
	State
	order  int
	limit  float64
	five   window
	week   window
	reason string // why it cannot be used; "" when it can
}

func (c Config) candidates(states []State, now time.Time) []candidate {
	out := make([]candidate, len(states))
	for i, st := range states {
		m := c.Member(st.Account.ID)
		v := candidate{State: st, order: i, limit: float64(m.Limit()), five: current(st.Quota.FiveHour, now), week: current(st.Quota.SevenDay, now)}
		switch {
		case m.Off:
			v.reason = "不参与"
		case !st.LoggedIn:
			v.reason = "未登录"
		case v.five.used >= v.limit || v.week.used >= v.limit:
			v.reason = "已达阈值"
		}
		out[i] = v
	}
	return out
}

// Status is an account's standing in automatic scheduling: "不参与", "未登录",
// "已达阈值", or "" when the scheduler may use it.
func (c Config) Status(st State, now time.Time) string {
	return c.candidates([]State{st}, now)[0].reason
}

// Choice is the account a decision settles on, and why.
type Choice struct {
	Account store.Account
	Reason  string
}

func (c Choice) OK() bool { return c.Account.ID != "" }

// Pick chooses the account for a new conversation under strategy. last is the account
// the scheduler assigned most recently; skip is an account to leave out, such as the one
// a resumed conversation is leaving. With nothing usable, the Choice is empty and its
// Reason says why.
func (c Config) Pick(strategy string, states []State, last, skip string, now time.Time) Choice {
	var usable, other []candidate
	for _, v := range c.candidates(states, now) {
		switch {
		case v.Account.ID == skip:
		case v.reason == "":
			usable = append(usable, v)
		default:
			other = append(other, v)
		}
	}
	if len(usable) == 0 {
		if slices.ContainsFunc(other, func(v candidate) bool { return v.reason == "已达阈值" }) {
			return Choice{Reason: "参与调度的账户都已达阈值"}
		}
		return Choice{Reason: "没有参与调度且已登录的账户"}
	}
	var to Choice
	switch strategy {
	case RoundRobin:
		to = roundRobin(usable, other, states, last)
	case FiveHour:
		to = fiveHour(usable, other)
	default:
		to = optimal(usable, other)
	}
	if !usable[slices.IndexFunc(usable, func(v candidate) bool { return v.Account.ID == to.Account.ID })].Read {
		to.Reason = "还没有它的额度数据，按未使用计算；" + to.Reason
	}
	return to
}

// Resume decides whether a conversation bound to from continues under another account:
// only in automatic mode with switching on resume enabled, for an account that takes part
// in scheduling and has reached its threshold. It reports false to keep the account.
func (c Config) Resume(states []State, from, last string, now time.Time) (Choice, bool) {
	if !c.Auto() || c.Sticky || c.Member(from).Off {
		return Choice{}, false
	}
	i := slices.IndexFunc(states, func(st State) bool { return st.Account.ID == from })
	if i < 0 || c.Status(states[i], now) != "已达阈值" {
		return Choice{}, false
	}
	to := c.Pick(c.Plan(), states, last, from, now)
	if !to.OK() {
		return to, false
	}
	to.Reason = "「" + states[i].Account.Name + "」已达阈值；" + to.Reason
	return to, true
}

// tooLate is how close to its reset a running 5-hour window counts as reset already:
// there is no time left to spend much of what remains in it.
const tooLate = 30 * time.Minute

// optimal spends first the quota that lapses first. Weekly quota is what an account has
// to spend; a 5-hour window only limits how fast it goes. So pace leads the score, and
// the 5-hour window can do no more than about double it.
func optimal(usable, other []candidate) Choice {
	score := func(v candidate) float64 { return pace(v) * fiveWeight(v) }
	best := usable[0]
	for _, v := range usable[1:] {
		s, b := score(v), score(best)
		if s > b || (s == b && v.limit-v.week.used > best.limit-best.week.used) {
			best = v
		}
	}
	why := "两个窗口都充足"
	switch {
	case pace(best) >= 2:
		why = fmt.Sprintf("本周额度 %s后重置，还剩 %.0f%%，先用掉", span(best.week.reset), best.limit-best.week.used)
	case best.five.reset >= tooLate && best.five.reset < 5*time.Hour && slices.ContainsFunc(usable, func(v candidate) bool { return v.five.reset < tooLate || v.five.reset > best.five.reset }):
		why = fmt.Sprintf("5 小时窗口 %s后重置，还剩 %.0f%%，先用掉", span(best.five.reset), best.limit-best.five.used)
	case pace(best) < 1:
		why = fmt.Sprintf("本周还剩 %.0f%%，相对最充足", best.limit-best.week.used)
	}
	var notes []string
	for _, v := range usable {
		if v.Account.ID != best.Account.ID && pace(v) < .5 {
			notes = append(notes, fmt.Sprintf("「%s」本周已用 %.0f%%，先留着", v.Account.Name, v.week.used))
		}
	}
	return Choice{best.Account, join(why, notes, other)}
}

// pace compares weekly headroom per remaining day with an even pace through the week.
// Above 1, quota will lapse at the reset unless the account gets more than its even
// share; below 1, the account is ahead of that pace and should wait.
func pace(v candidate) float64 {
	days := 7.0
	if v.week.reset > 0 {
		days = math.Min(7, math.Max(1.0/24, v.week.reset.Hours()/24))
	}
	return (v.limit - v.week.used) / days / (v.limit / 7)
}

// fiveWeight is what the 5-hour window does to the score. A window not running loses
// nothing until it starts and weighs 1. A running one weighs up to nearly 2 the closer it
// is to its reset, where its headroom lapses. With less than half its headroom left it
// weighs less in proportion, as a conversation would soon reach the limit.
func fiveWeight(v candidate) float64 {
	if v.five.reset < tooLate {
		return 1
	}
	room := math.Min(1, (v.limit-v.five.used)/(v.limit/2))
	return room * (2 - math.Min(5, v.five.reset.Hours())/5)
}

// fiveHour uses the running 5-hour window that resets first, so its headroom is not
// lost; accounts with no window running come after, the one with most headroom first.
func fiveHour(usable, other []candidate) Choice {
	best := slices.MinFunc(usable, func(a, b candidate) int {
		switch {
		case (a.five.reset > 0) != (b.five.reset > 0):
			if a.five.reset > 0 {
				return -1
			}
			return 1
		case a.five.reset != b.five.reset:
			return int(a.five.reset - b.five.reset)
		case a.limit-a.five.used != b.limit-b.five.used:
			if a.limit-a.five.used > b.limit-b.five.used {
				return -1
			}
			return 1
		}
		return a.order - b.order
	})
	why := fmt.Sprintf("没有进行中的 5 小时窗口，选 5 小时余量最多的（%.0f%%）", best.limit-best.five.used)
	if best.five.reset > 0 {
		why = fmt.Sprintf("5 小时窗口最早重置（%s后），还剩 %.0f%%", span(best.five.reset), best.limit-best.five.used)
	}
	return Choice{best.Account, join(why, nil, other)}
}

// roundRobin takes the next usable account after last, in account order.
func roundRobin(usable, other []candidate, states []State, last string) Choice {
	start := slices.IndexFunc(states, func(st State) bool { return st.Account.ID == last }) + 1
	best := usable[0]
	for n := range len(states) {
		i := (start + n) % len(states)
		if j := slices.IndexFunc(usable, func(v candidate) bool { return v.order == i }); j >= 0 {
			best = usable[j]
			break
		}
	}
	why := "按账户顺序轮流"
	if start > 0 && states[start-1].Account.ID != best.Account.ID {
		why = "上一个对话用了「" + states[start-1].Account.Name + "」，这次轮到它"
	}
	return Choice{best.Account, join(why, nil, other)}
}

// join adds what kept the other accounts out: why the choice, then notes, then each
// left-out account that takes part.
func join(why string, notes []string, other []candidate) string {
	parts := append([]string{why}, notes...)
	for _, v := range other {
		if v.reason != "不参与" {
			parts = append(parts, "「"+v.Account.Name+"」"+v.reason)
		}
	}
	return strings.Join(parts, "；")
}

// span writes a duration the way reset countdowns do: 35 分钟, 1 小时 10 分, 3 天 4 小时.
func span(d time.Duration) string {
	switch m := int(d.Minutes()); {
	case m < 1:
		return "不到 1 分钟"
	case m < 60:
		return fmt.Sprintf("%d 分钟", m)
	case m < 24*60 && m%60 == 0:
		return fmt.Sprintf("%d 小时", m/60)
	case m < 24*60:
		return fmt.Sprintf("%d 小时 %d 分", m/60, m%60)
	case m/60%24 == 0:
		return fmt.Sprintf("%d 天", m/(24*60))
	default:
		return fmt.Sprintf("%d 天 %d 小时", m/(24*60), m/60%24)
	}
}
