package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ccrouter/internal/schedule"
	"ccrouter/internal/status"
	"ccrouter/internal/store"
)

const scheduleUsage = `用法：
ccrouter schedule                        查看调度
ccrouter schedule manual                 手动调度
ccrouter schedule auto [最优|轮询|5h]     自动调度及其策略
ccrouter schedule threshold 名称 百分比   该账户的阈值（50–100，步长 5）
ccrouter schedule include|exclude 名称    参与或不参与自动调度
ccrouter schedule resume on|off          续聊达到阈值时是否自动换号`

// strategies accepts each strategy's key and the names people type for it.
var strategies = map[string]string{
	"optimal": schedule.Optimal, "最优": schedule.Optimal, "5h/7d": schedule.Optimal,
	"roundrobin": schedule.RoundRobin, "轮询": schedule.RoundRobin,
	"fivehour": schedule.FiveHour, "5h": schedule.FiveHour,
}

func scheduling(s *store.Store, args []string) error {
	c := schedule.Load(s)
	member := func(name string, change func(*schedule.Member)) error {
		a, e := s.Account(name)
		if e != nil {
			return e
		}
		m := c.Member(a.ID)
		change(&m)
		c.SetMember(a.ID, m)
		return nil
	}
	var e error
	switch {
	case len(args) == 0:
		return showSchedule(s, c)
	case args[0] == "manual" && len(args) == 1:
		c.Mode = schedule.Manual
	case args[0] == "auto" && len(args) <= 2:
		c.Mode = schedule.Auto
		if len(args) == 2 {
			v, ok := strategies[strings.ToLower(args[1])]
			if !ok {
				return fmt.Errorf("没有「%s」这个策略；可选：最优、轮询、5h", args[1])
			}
			c.Strategy = v
		}
	case args[0] == "threshold" && len(args) == 3:
		n, err := strconv.Atoi(strings.TrimSuffix(args[2], "%"))
		if err != nil || n < schedule.MinThreshold || n > schedule.MaxThreshold || n%schedule.Step != 0 {
			return fmt.Errorf("阈值是 %d 到 %d 之间、%d 的倍数", schedule.MinThreshold, schedule.MaxThreshold, schedule.Step)
		}
		e = member(args[1], func(m *schedule.Member) { m.Threshold = n })
	case (args[0] == "include" || args[0] == "exclude") && len(args) == 2:
		e = member(args[1], func(m *schedule.Member) { m.Off = args[0] == "exclude" })
	case args[0] == "resume" && len(args) == 2 && (args[1] == "on" || args[1] == "off"):
		c.Sticky = args[1] == "off"
	default:
		return errors.New(scheduleUsage)
	}
	if e != nil {
		return e
	}
	if e = schedule.Save(s, c); e != nil {
		return e
	}
	return showSchedule(s, c)
}

func showSchedule(s *store.Store, c schedule.Config) error {
	sts, now := states(s), time.Now()
	next := c.Pick(c.Plan(), sts, schedule.Last(s), "", now)
	if c.Auto() {
		resume := "开"
		if c.Sticky {
			resume = "关"
		}
		fmt.Printf("模式：自动 · %s\n续聊达到阈值时自动换号：%s\n", schedule.Name(c.Plan()), resume)
	} else {
		fmt.Println("模式：手动（新对话用默认账户，续聊沿用原账户）")
	}
	for _, st := range sts {
		m := c.Member(st.Account.ID)
		part := "参与"
		if m.Off {
			part = "不参与"
		}
		tag := c.Status(st, now)
		switch {
		case tag == "不参与":
			tag = ""
		case c.Auto() && next.Account.ID == st.Account.ID:
			tag = "下一个"
		}
		fmt.Printf("%s\t%s\t阈值 %d%%\t%s\t%s\n", st.Account.Name, part, m.Limit(), quotaLine(st.Quota, now), tag)
	}
	if c.Auto() {
		if next.OK() {
			fmt.Printf("下一个对话：%s（%s）\n", next.Account.Name, next.Reason)
		} else {
			fmt.Printf("下一个对话：没有可用账户（%s），使用默认账户\n", next.Reason)
		}
	}
	return nil
}

// quotaLine summarizes both windows; a window past its reset waits for the next reading.
func quotaLine(q status.Quota, now time.Time) string {
	part := func(name string, w *status.Window) string {
		switch {
		case w == nil:
			return name + " —"
		case w.ResetsAt != nil && !w.ResetsAt.After(now):
			return name + " 已重置"
		}
		return fmt.Sprintf("%s %.0f%%", name, w.Utilization)
	}
	return part("5 小时", q.FiveHour) + " · " + part("本周", q.SevenDay)
}
