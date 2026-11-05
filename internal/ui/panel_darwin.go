package ui

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"ccrouter/internal/schedule"
	"ccrouter/internal/status"
	"ccrouter/internal/store"
	"ccrouter/internal/usage"
	"github.com/progrium/darwinkit/helper/action"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/macos/quartzcore"
	"github.com/progrium/darwinkit/objc"
)

// Geometry follows the approved B2 "Liquid Glass" design study (280 pt wide).
// The popover supplies the system material; content only adds light tonal overlays.
const (
	panelWidth = 280.0
	inset      = 18.0
	edge       = panelWidth - inset
	column     = 43.0 // account text, after the default-account mark
)

var periods = []string{"今日", "近 7 天", "全部"}

type style struct {
	size   float64
	weight appkit.FontWeight
	color  appkit.Color
	digits bool
}
type placed struct {
	v          appkit.View
	x, y, w, h float64
}

// canvas places views top-down; finish flips them into AppKit's bottom-left coordinates.
type canvas struct {
	items []placed
	dark  bool
}

func rect(x, y, w, h float64) foundation.Rect {
	return foundation.Rect{Origin: foundation.Point{X: x, Y: y}, Size: foundation.Size{Width: w, Height: h}}
}
func (c *canvas) place(v appkit.View, x, y, w, h float64) int {
	c.items = append(c.items, placed{v, x, y, w, h})
	return len(c.items) - 1
}
func (c *canvas) finish(height float64) appkit.View {
	root := appkit.NewViewWithFrame(rect(0, 0, panelWidth, height))
	for _, p := range c.items {
		p.v.SetFrame(rect(p.x, height-p.y-p.h, p.w, p.h))
		root.AddSubview(p.v)
	}
	return root
}
func label(s string, st style) (appkit.TextField, foundation.Size) {
	l := appkit.TextField_LabelWithString(s)
	if st.digits {
		l.SetFont(appkit.Font_MonospacedDigitSystemFontOfSizeWeight(st.size, st.weight))
	} else {
		l.SetFont(appkit.Font_SystemFontOfSizeWeight(st.size, st.weight))
	}
	l.SetTextColor(st.color)
	objc.Call[objc.Void](l, objc.Sel("setLineBreakMode:"), appkit.LineBreakByTruncatingTail)
	return l, l.FittingSize()
}

// Label cells pad text by 2 pt on each side; positions below refer to the glyphs.
func measure(s string, st style) float64 { _, size := label(s, st); return math.Ceil(size.Width) - 4 }

// text anchors a single line at x (left edge, right edge or center, per align) and
// vertical center cy. Text wider than max is truncated with a tooltip. Returns the width.
func (c *canvas) text(s string, st style, x, cy float64, align appkit.TextAlignment, max float64) (appkit.TextField, float64) {
	l, size := label(s, st)
	l.SetAlignment(align)
	w := math.Ceil(size.Width) - 4
	if max > 0 && w > max {
		w = max
	}
	switch align {
	case appkit.TextAlignmentRight:
		x -= w
	case appkit.TextAlignmentCenter:
		// A frame that exactly fits centered text still truncates it; center in the full span.
		c.place(l.View, x-max/2-2, cy-size.Height/2, max+4, size.Height)
		return l, w
	}
	c.place(l.View, x-2, cy-size.Height/2, w+4, size.Height)
	return l, w
}
func (c *canvas) glyph(name string, size float64, weight appkit.FontWeight, color appkit.Color, cx, cy float64) {
	img := appkit.Image_ImageWithSystemSymbolNameAccessibilityDescription(name, "")
	img = img.ImageWithSymbolConfiguration(appkit.ImageSymbolConfiguration_ConfigurationWithPointSizeWeight(size, weight))
	s := img.Size()
	v := appkit.ImageView_ImageViewWithImage(img)
	v.SetContentTintColor(color)
	v.SetImageScaling(appkit.ImageScaleNone)
	c.place(v.View, cx-s.Width/2, cy-s.Height/2, s.Width, s.Height)
}

// hit is an invisible click target laid over the drawn content of a row or icon. The label
// is for VoiceOver only; nothing appears on hover.
func (c *canvas) hit(x, y, w, h float64, label string, fn func()) appkit.Button {
	b := appkit.NewButtonWithFrame(rect(0, 0, w, h))
	b.SetTitle("")
	b.SetBordered(false)
	b.SetTransparent(true)
	objc.Call[objc.Void](b, objc.Sel("setAccessibilityLabel:"), label)
	action.Set(b, func(objc.Object) { fn() })
	c.place(b.View, x, y, w, h)
	return b
}
func (c *canvas) fill(x, y, w, h, r float64, color appkit.Color) int {
	v := appkit.NewViewWithFrame(rect(0, 0, w, h))
	v.SetWantsLayer(true)
	l := v.Layer()
	l.SetBackgroundColor(color.CGColor())
	if r > 0 {
		l.SetCornerRadius(r)
		l.SetCornerCurve(quartzcore.KCornerCurveContinuous)
	}
	return c.place(v, x, y, w, h)
}
func (c *canvas) capsule(x, y, w, h float64) {
	l := c.items[c.fill(x, y, w, h, h/2, c.lift(.5, .08))].v.Layer()
	l.SetBorderWidth(.5)
	l.SetBorderColor(appkit.Color_LabelColor().ColorWithAlphaComponent(.08).CGColor())
}

// outline is a capsule ringed in color, for a label that stands out.
func (c *canvas) outline(x, y, w, h float64, color appkit.Color) {
	l := c.items[c.fill(x, y, w, h, h/2, c.lift(.5, .08))].v.Layer()
	l.SetBorderWidth(1)
	l.SetBorderColor(color.CGColor())
}

// paragraph places text that wraps within w, its top at y, and returns its height.
func (c *canvas) paragraph(s string, st style, x, y, w float64) float64 {
	l := appkit.TextField_WrappingLabelWithString(s)
	l.SetFont(appkit.Font_SystemFontOfSizeWeight(st.size, st.weight))
	l.SetTextColor(st.color)
	l.SetPreferredMaxLayoutWidth(w + 4)
	// The frame takes the fitted width too, so the label wraps exactly as it was measured.
	size := l.FittingSize()
	h := math.Ceil(size.Height)
	c.place(l.View, x-2, y, math.Ceil(size.Width), h)
	return h
}

var tagStyle = style{10, appkit.FontWeightSemibold, appkit.Color_WhiteColor(), false}

func tagWidth(s string) float64 { return measure(s, tagStyle) + 12 }

// tag draws an account's standing in automatic scheduling as a small filled label that
// ends at right, centered on cy.
func (c *canvas) tag(s string, right, cy float64) {
	fill, ink := appkit.Color_LabelColor().ColorWithAlphaComponent(.1), appkit.Color_SecondaryLabelColor()
	switch s {
	case "下一个":
		fill, ink = appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.22, .44, .58, 1), appkit.Color_WhiteColor()
	case "已达阈值":
		fill, ink = appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.70, .38, .05, 1), appkit.Color_WhiteColor()
	}
	w := tagWidth(s)
	c.fill(right-w, cy-8, w, 16, 8, fill)
	c.text(s, style{tagStyle.size, tagStyle.weight, ink, false}, right-w+6, cy, appkit.TextAlignmentLeft, 0)
}

// card is a rounded tonal block under a group of rows; set its height once they are placed.
func (c *canvas) card(y float64) int { return c.fill(12, y, panelWidth-24, 0, 12, c.lift(.2, .03)) }

// rule separates rows inside a card.
func (c *canvas) rule(y float64) { c.fill(24, y, panelWidth-48, .5, 0, appkit.Color_SeparatorColor()) }

func (c *canvas) hline(y float64)       { c.fill(inset, y, edge-inset, .5, 0, appkit.Color_SeparatorColor()) }
func (c *canvas) vline(x, y, h float64) { c.fill(x, y, .5, h, 0, appkit.Color_SeparatorColor()) }

// lift is a white tonal overlay on the shared material, never a separate card.
func (c *canvas) lift(light, dark float64) appkit.Color {
	if c.dark {
		return appkit.Color_WhiteColor().ColorWithAlphaComponent(dark)
	}
	return appkit.Color_WhiteColor().ColorWithAlphaComponent(light)
}
func (c *canvas) accent() appkit.Color {
	if c.dark {
		return appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.47, .67, .78, 1)
	}
	return appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.29, .50, .61, 1)
}
func (c *canvas) warm() appkit.Color {
	if c.dark {
		return appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.80, .60, .52, 1)
	}
	return appkit.Color_ColorWithSRGBRedGreenBlueAlpha(.70, .45, .37, 1)
}
func plain(dark bool) appkit.Appearance {
	if dark {
		return appkit.Appearance_AppearanceNamed(appkit.AppearanceNameDarkAqua)
	}
	return appkit.Appearance_AppearanceNamed(appkit.AppearanceNameAqua)
}
func isDark(v appkit.Appearance) bool {
	return v.BestMatchFromAppearancesWithNames([]appkit.AppearanceName{appkit.AppearanceNameAqua, appkit.AppearanceNameDarkAqua}) == appkit.AppearanceNameDarkAqua
}

func (a *application) render() {
	var view appkit.View
	var height float64
	// Layer colors are resolved once. Vibrant appearances resolve some dynamic colors to
	// near-black blend sources, so resolve under the plain light/dark appearance instead.
	dark := isDark(a.pop.EffectiveAppearance())
	a.dark = dark
	plain(dark).PerformAsCurrentDrawingAppearance(func() {
		c := &canvas{dark: dark}
		a.pick = schedule.Choice{}
		if a.scheduling() {
			cfg := a.data.Schedule
			a.pick = cfg.Pick(cfg.Plan(), a.states(), a.data.Last, "", time.Now())
		}
		y := a.header(c)
		// An open footer section is tall; usage steps aside while it is open.
		if a.open == "" {
			y = a.usage(c, y)
		}
		height = a.footer(c, a.accounts(c, y))
		view = c.finish(height)
	})
	a.content, a.height = view, height
	visible := height
	if s := appkit.Screen_MainScreen(); !s.IsNil() {
		visible = math.Min(height, s.VisibleFrame().Size.Height-48)
	}
	root := view
	if visible < height {
		scroll := appkit.NewScrollViewWithFrame(rect(0, 0, panelWidth, visible))
		scroll.SetDrawsBackground(false)
		scroll.SetHasVerticalScroller(true)
		scroll.SetScrollerStyle(appkit.ScrollerStyleOverlay)
		scroll.SetAutohidesScrollers(true)
		scroll.SetDocumentView(view)
		// NSView uses a bottom-left origin. Start each rebuilt scroll view at the top.
		scroll.ContentView().ScrollToPoint(foundation.Point{X: 0, Y: height - visible})
		scroll.ReflectScrolledClipView(scroll.ContentView())
		root = scroll.View
	}
	a.controller.SetView(root)
	a.pop.SetContentSize(foundation.Size{Width: panelWidth, Height: visible})
}

func (a *application) header(c *canvas) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	_, title := c.text("CCRouter", style{15, appkit.FontWeightBold, primary, false}, inset, 28, appkit.TextAlignmentLeft, 0)
	dot, state, detail := a.state()
	c.fill(inset, 43, 7, 7, 3.5, dot)
	_, w := c.text(state, style{11, appkit.FontWeightRegular, secondary, false}, inset+12, 46.5, appkit.TextAlignmentLeft, 110)
	if detail != nil {
		c.hit(inset-4, 37, w+20, 19, "查看状态详情", detail)
	}

	tint := primary
	if a.refreshing {
		tint = appkit.Color_TertiaryLabelColor()
	}
	c.glyph("arrow.triangle.2.circlepath", 16, appkit.FontWeightRegular, tint, 251, 34)
	c.hit(237, 20, 28, 28, "刷新用量、额度和 IP", a.refreshAll)

	// Exit IP accessory between the title and the refresh button. If country and address do
	// not both fit, the address alone is shown; it is truncated only past that (long IPv6).
	st := style{11, appkit.FontWeightRegular, primary, false}
	labels := a.exitText()
	room := 234 - 8 - (inset + title) - 33
	shown := labels[len(labels)-1]
	for _, l := range labels {
		if measure(l, st) <= room {
			shown = l
			break
		}
	}
	tw := math.Min(measure(shown, st), room)
	if a.copied {
		shown = "已复制"
	}
	w = tw + 33
	x := 234 - w
	c.capsule(x, 22, w, 24)
	c.glyph("globe", 12, appkit.FontWeightRegular, secondary, x+14, 34)
	c.text(shown, st, x+24, 34, appkit.TextAlignmentLeft, tw)
	c.hit(x, 22, w, 24, "复制出口 IP", a.copyIP)
	c.hline(60)
	return 60
}
func (a *application) state() (appkit.Color, string, func()) {
	switch {
	case a.demo:
		return appkit.Color_TertiaryLabelColor(), "演示模式", nil
	case a.notice != "":
		notice := a.notice
		return appkit.Color_SystemOrangeColor(), "需要关注", func() { a.alert("状态详情", notice) }
	case len(a.data.Accounts) == 0:
		return appkit.Color_TertiaryLabelColor(), "未添加账户", nil
	}
	return appkit.Color_SystemGreenColor(), "运行中", nil
}

func (a *application) usage(c *canvas, y float64) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	period := periods[a.period]
	c.text(period+"用量", style{13, appkit.FontWeightSemibold, primary, false}, inset, y+18, appkit.TextAlignmentLeft, 150)
	c.glyph("chevron.down", 9, appkit.FontWeightSemibold, primary, edge-4, y+18.5)
	_, pw := c.text(period, style{12, appkit.FontWeightRegular, primary, false}, edge-13, y+18, appkit.TextAlignmentRight, 0)
	c.hit(edge-pw-21, y+6, pw+25, 24, "切换统计范围：今日、近 7 天、全部", func() {
		a.period = (a.period + 1) % len(periods)
		a.refreshLocal()
		if a.demo {
			a.render()
		}
	})

	t := a.data.Total
	rate := "—"
	if t.Input+t.CacheRead+t.CacheWrite > 0 {
		rate = fmt.Sprintf("%.0f%%", t.HitRate())
	}
	metrics := []struct {
		x, w        float64
		name, value string
	}{
		{inset, 78, "总 tokens", compact(t.Total())},
		{112, 64, "请求数", compact(t.Requests)},
		{192, 70, "输入缓存命中", rate},
	}
	for _, m := range metrics {
		c.text(m.name, style{11, appkit.FontWeightRegular, primary, false}, m.x, y+44.5, appkit.TextAlignmentLeft, m.w)
		c.text(m.value, style{17, appkit.FontWeightSemibold, primary, true}, m.x, y+64, appkit.TextAlignmentLeft, m.w)
	}
	c.vline(100, y+37, 37)
	c.vline(180, y+37, 37)
	c.hline(y + 80)
	a.chart(c, inset, y+86, edge-inset, 36)
	small := style{11, appkit.FontWeightRegular, secondary, false}
	c.text("60 分钟前", small, inset, y+133, appkit.TextAlignmentLeft, 0)
	c.text("现在", small, edge, y+133, appkit.TextAlignmentRight, 0)
	c.hline(y + 147)

	body := style{12, appkit.FontWeightRegular, primary, false}
	digits := style{12, appkit.FontWeightRegular, primary, true}
	rows := []struct {
		l1 string
		v1 int64
		l2 string
		v2 int64
		y  float64
	}{{"缓存读取", t.CacheRead, "缓存写入", t.CacheWrite, y + 165}, {"未缓存输入", t.Input, "输出", t.Output, y + 187}}
	for _, r := range rows {
		c.text(r.l1, body, inset, r.y, appkit.TextAlignmentLeft, 0)
		c.text(compact(r.v1), digits, 124, r.y, appkit.TextAlignmentRight, 0)
		c.text(r.l2, body, 156, r.y, appkit.TextAlignmentLeft, 0)
		c.text(compact(r.v2), digits, edge, r.y, appkit.TextAlignmentRight, 0)
	}
	c.vline(140, y+154, 44)
	c.hline(y + 207)
	return y + 207
}

func (a *application) accounts(c *canvas, y float64) float64 {
	top := y + 5
	group := c.fill(8, top, panelWidth-16, 0, 14, c.lift(.16, .035))
	y = top
	if len(a.data.Accounts) == 0 {
		y = a.emptyAccounts(c, y)
	}
	for i, ac := range a.data.Accounts {
		if i > 0 {
			c.hline(y)
		}
		y = a.account(c, ac, y, i == 0, i == len(a.data.Accounts)-1)
	}
	c.items[group].h = y - top
	return y
}
func (a *application) account(c *canvas, ac store.Account, y float64, first, last bool) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	open := a.expanded[ac.ID]
	q := a.quotas[ac.ID]
	h := 60.0
	auto, cfg := a.scheduling(), a.data.Schedule
	m := cfg.Member(ac.ID)
	standing := ""
	if auto {
		standing = cfg.Status(a.scheduleState(ac), time.Now())
		if standing == "" && a.pick.Account.ID == ac.ID {
			standing = "下一个"
		}
	}
	notice, warn := quotaNotice(q)
	if notice == "" && standing == "已达阈值" {
		notice, warn = fmt.Sprintf("已达 %d%% 阈值，自动调度暂不使用", m.Limit()), true
	}
	if open {
		h = 128
		if notice != "" {
			h += 18
		}
		// Layer corners use AppKit's bottom-left origin: MinY is the bottom edge.
		var corners quartzcore.CornerMask
		if first {
			corners |= quartzcore.KLayerMinXMaxYCorner | quartzcore.KLayerMaxXMaxYCorner
		}
		if last {
			corners |= quartzcore.KLayerMinXMinYCorner | quartzcore.KLayerMaxXMinYCorner
		}
		c.items[c.fill(8, y, panelWidth-16, h, 14, c.lift(.2, .03))].v.Layer().SetMaskedCorners(corners)
	}
	cy := y + 22
	// The mark is the account a new conversation gets: the scheduler's next one, or the default.
	switch {
	case standing == "下一个":
		c.glyph("arrow.right", 12, appkit.FontWeightSemibold, c.accent(), 26, cy)
	case !auto && ac.Default:
		c.glyph("checkmark", 13, appkit.FontWeightSemibold, primary, 26, cy)
	}
	arrow := "chevron.right"
	if open {
		arrow = "chevron.down"
	}
	c.glyph(arrow, 11, appkit.FontWeightSemibold, primary, edge-5, cy)
	total := a.data.ByAccount[ac.ID]
	_, vw := c.text(compact(total.Total()), style{13, appkit.FontWeightSemibold, primary, true}, edge-16, cy, appkit.TextAlignmentRight, 0)
	_, pw := c.text(periods[a.period], style{12, appkit.FontWeightRegular, secondary, false}, edge-21-vw, cy, appkit.TextAlignmentRight, 0)
	left := edge - 21 - vw - pw

	badge := style{11, appkit.FontWeightRegular, secondary, false}
	plan := planLabel(ac.Plan)
	room := left - 10 - column
	bw := 0.0
	if plan != "" {
		bw = measure(plan, badge) + 16
		room -= bw + 8
	}
	_, nw := c.text(ac.Name, style{13, appkit.FontWeightSemibold, primary, false}, column, cy, appkit.TextAlignmentLeft, room)
	if plan != "" {
		c.capsule(column+nw+8, cy-9, bw, 18)
		c.text(plan, badge, column+nw+16, cy, appkit.TextAlignmentLeft, 0)
	}
	small := style{11, appkit.FontWeightRegular, secondary, false}
	if !open {
		room := edge - column
		if standing != "" {
			c.tag(standing, edge, y+42)
			room -= tagWidth(standing) + 6
		}
		c.text(quotaSummary(q), small, column, y+42, appkit.TextAlignmentLeft, room)
	} else {
		email := ac.Email
		if email == "" {
			email = "登录后显示账户信息"
		}
		action := "设为默认"
		if ac.Default {
			action = "新对话默认"
		}
		room := edge - column - 12
		switch {
		case auto && standing != "":
			room -= tagWidth(standing)
		case !auto:
			room -= measure(action, small)
		}
		c.text(email, style{12, appkit.FontWeightRegular, secondary, false}, column, y+40, appkit.TextAlignmentLeft, room)
		now := time.Now()
		limit := 0.0
		if auto && !m.Off {
			limit = float64(m.Limit())
		}
		c.quota("5 小时", q.Quota.FiveHour, y+63, now, limit)
		c.quota("本周", q.Quota.SevenDay, y+97, now, limit)
		if notice != "" {
			color := appkit.Color_SecondaryLabelColor()
			if warn {
				color = appkit.Color_SystemOrangeColor()
			}
			c.text(notice, style{11, appkit.FontWeightRegular, color, false}, column, y+131, appkit.TextAlignmentLeft, edge-column)
		}
	}

	// The whole block toggles; "设为默认" is the only nested action, so it is placed on top.
	c.hit(8, y, panelWidth-16, h, "展开或折叠账户", func() {
		a.expanded[ac.ID] = !open
		if !open {
			a.open, a.choosing = "", ""
		}
		a.render()
	})
	switch {
	case open && auto:
		// A new conversation goes where the scheduler says, so there is no default to set.
		if standing != "" {
			c.tag(standing, edge, y+40)
		}
	case open && ac.Default:
		c.text("新对话默认", small, edge, y+40, appkit.TextAlignmentRight, 0)
	case open:
		_, w := c.text("设为默认", style{11, appkit.FontWeightMedium, c.accent(), false}, edge, y+40, appkit.TextAlignmentRight, 0)
		c.hit(edge-w-8, y+30, w+16, 20, "设为新对话的默认账户", func() { a.selectAccount(ac) })
	}
	return y + h
}

// quota draws one window: label, bar and percentage, with the time to its reset below.
// Once the reset has passed, the reading no longer applies until Claude refreshes it. A
// limit above zero is the account's scheduling threshold, marked on the bar.
func (c *canvas) quota(title string, w *status.Window, cy float64, now time.Time, limit float64) {
	secondary := appkit.Color_SecondaryLabelColor()
	c.text(title, style{11, appkit.FontWeightRegular, secondary, false}, column, cy, appkit.TextAlignmentLeft, 0)
	c.fill(94, cy-1.5, 130, 3, 1.5, appkit.Color_QuaternaryLabelColor())
	text, reset := "—", ""
	if w != nil {
		reset = resetIn(w.ResetsAt, now)
		if reset == "已重置" {
			reset = "已重置，等待刷新"
		} else {
			text = fmt.Sprintf("%.0f%%", w.Utilization)
			fill := c.accent()
			if limit > 0 && w.Utilization >= limit {
				fill = appkit.Color_SystemOrangeColor()
			}
			c.fill(94, cy-1.5, 130*math.Min(100, math.Max(0, w.Utilization))/100, 3, 1.5, fill)
		}
	}
	if limit > 0 {
		c.fill(94+130*limit/100-.75, cy-4.5, 1.5, 9, .75, appkit.Color_LabelColor().ColorWithAlphaComponent(.55))
	}
	c.text(text, style{11, appkit.FontWeightRegular, secondary, true}, edge, cy, appkit.TextAlignmentRight, 0)
	if reset != "" {
		c.text(reset, style{10, appkit.FontWeightRegular, appkit.Color_TertiaryLabelColor(), false}, 94, cy+14, appkit.TextAlignmentLeft, edge-94)
	}
}

// quotaNotice is shown under an expanded account: an error, or the age of a stale reading.
func quotaNotice(q status.Result) (string, bool) {
	switch {
	case q.Error != "":
		return q.Error, true
	case !q.Updated.IsZero() && time.Since(q.Updated) > 10*time.Minute:
		return "额度数据更新于 " + usage.When(q.Updated), false
	}
	return "", false
}

func (a *application) emptyAccounts(c *canvas, y float64) float64 {
	c.text("还没有账户", style{13, appkit.FontWeightSemibold, appkit.Color_LabelColor(), false}, inset, y+24, appkit.TextAlignmentLeft, 0)
	c.text("添加新账户，或导入当前 Claude 登录。", style{11, appkit.FontWeightRegular, appkit.Color_SecondaryLabelColor(), false}, inset, y+44, appkit.TextAlignmentLeft, edge-inset)
	b := appkit.NewButtonWithFrame(rect(0, 0, 10, 10))
	b.SetTitle("导入当前 Claude 账户")
	b.SetBezelStyle(appkit.BezelStyleRounded)
	b.SetControlSize(appkit.ControlSizeSmall)
	b.SetFont(appkit.Font_SystemFontOfSize(11))
	action.Set(b, func(objc.Object) { a.importDefault() })
	s := b.FittingSize()
	c.place(b.View, inset-6, y+58, s.Width, s.Height)
	return y + 92
}

func (a *application) footer(c *canvas, y float64) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	c.hline(y + 4)
	y = a.recentSection(c, a.scheduleSection(c, y+4))

	cy := y + 21
	c.glyph("plus.circle.fill", 19, appkit.FontWeightRegular, primary, 28, cy)
	c.text("添加账户", style{12, appkit.FontWeightSemibold, primary, false}, 50, cy, appkit.TextAlignmentLeft, 0)
	c.hit(8, cy-14, 120, 28, "添加 Claude 账户", a.addAccount)
	c.glyph("gearshape", 16, appkit.FontWeightRegular, primary, 213, cy)
	c.hit(199, cy-14, 28, 28, "账户与设置", func() { a.settings(199, cy+14) })
	c.glyph("power", 16, appkit.FontWeightRegular, primary, 252, cy)
	c.hit(238, cy-14, 28, 28, "退出 CCRouter", a.quit)
	c.text("仅统计本机已记录的会话", style{11, appkit.FontWeightRegular, secondary, false}, panelWidth/2, y+47, appkit.TextAlignmentCenter, edge-inset)
	return y + 64
}

// scheduleSection is the row that says how accounts are chosen. With one account there
// is nothing to choose, so it only says that a resume keeps its account.
func (a *application) scheduleSection(c *canvas, y float64) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	cy := y + 22
	if len(a.data.Accounts) < 2 {
		c.glyph("lock.fill", 13, appkit.FontWeightRegular, primary, 26, cy)
		c.text("续聊沿用原账户", style{12, appkit.FontWeightRegular, secondary, false}, column, cy, appkit.TextAlignmentLeft, 0)
		y += 40
		c.hline(y)
		return y
	}
	cfg, open := a.data.Schedule, a.open == "schedule"
	icon, tint := "lock.fill", primary
	if cfg.Auto() {
		icon, tint = "arrow.triangle.2.circlepath", c.accent()
	}
	c.glyph(icon, 13, appkit.FontWeightRegular, tint, 26, cy)
	switch {
	case open:
		c.text("账户调度", style{12, appkit.FontWeightSemibold, primary, false}, column, cy, appkit.TextAlignmentLeft, 0)
	case cfg.Auto():
		next := 0.0
		if a.pick.OK() {
			_, next = c.text("→ "+a.pick.Account.Name, style{11, appkit.FontWeightRegular, c.accent(), false}, edge-16, cy, appkit.TextAlignmentRight, 90)
		}
		c.text("自动 · "+schedule.Name(cfg.Plan()), style{12, appkit.FontWeightSemibold, c.accent(), false}, column, cy, appkit.TextAlignmentLeft, edge-16-next-8-column)
	default:
		c.text("手动 · 续聊沿用原账户", style{12, appkit.FontWeightRegular, secondary, false}, column, cy, appkit.TextAlignmentLeft, edge-16-column)
	}
	arrow := "chevron.right"
	if open {
		arrow = "chevron.down"
	}
	c.glyph(arrow, 11, appkit.FontWeightSemibold, primary, edge-5, cy)
	c.hit(8, y+5, panelWidth-16, 34, "账户调度", func() { a.toggle("schedule") })
	y += 40
	if open {
		y = a.scheduleBody(c, y)
	}
	c.hline(y)
	return y
}

func (a *application) scheduleBody(c *canvas, y float64) float64 {
	primary, secondary, tertiary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor(), appkit.Color_TertiaryLabelColor()
	small := style{11, appkit.FontWeightRegular, secondary, false}
	cfg := a.data.Schedule
	set := func(change func(*schedule.Config)) func() {
		return func() {
			c := cfg
			// Members is a map; change a copy so the render's config stays as it was drawn.
			c.Members = maps.Clone(cfg.Members)
			change(&c)
			a.saveSchedule(c)
		}
	}

	// Mode: a segmented choice.
	w := (edge - inset - 4) / 2
	c.fill(inset, y, edge-inset, 26, 7, appkit.Color_LabelColor().ColorWithAlphaComponent(.07))
	for i, mode := range []string{schedule.Manual, schedule.Auto} {
		x, title := inset+2+float64(i)*w, []string{"手动", "自动"}[i]
		on := (mode == schedule.Auto) == cfg.Auto()
		weight := appkit.FontWeightRegular
		if on {
			c.fill(x, y+2, w, 22, 6, c.lift(.9, .2))
			weight = appkit.FontWeightSemibold
		}
		c.text(title, style{12, weight, primary, false}, x+w/2, y+13, appkit.TextAlignmentCenter, w-8)
		c.hit(x, y, w, 26, "调度模式："+title, set(func(c *schedule.Config) { c.Mode = mode }))
	}
	y += 36
	if !cfg.Auto() {
		h := c.paragraph("新对话用默认账户（✓），续聊沿用原账户。在最近对话里点账户名，可以把对话换到别的账户继续。", small, inset+2, y, edge-inset-4)
		return y + h + 12
	}

	// Strategies, each with the account it would choose now.
	states, now := a.states(), time.Now()
	top, card := y, c.card(y)
	for i, s := range schedule.Strategies {
		if i > 0 {
			c.rule(y)
		}
		row, on, cy := y, s == cfg.Plan(), y+15
		mark, tint := "circle", tertiary
		if on {
			mark, tint = "largecircle.fill.circle", c.accent()
		}
		c.glyph(mark, 13, appkit.FontWeightRegular, tint, 30, cy)
		to, ink := "没有可用账户", tertiary
		if pick := cfg.Pick(s, states, a.data.Last, "", now); pick.OK() {
			to, ink = "→ "+pick.Account.Name, c.accent()
		}
		_, pw := c.text(to, style{11, appkit.FontWeightRegular, ink, false}, panelWidth-24, cy, appkit.TextAlignmentRight, 90)
		c.text(schedule.Name(s), style{12, appkit.FontWeightSemibold, primary, false}, 44, cy, appkit.TextAlignmentLeft, panelWidth-24-pw-8-44)
		y += 30
		if on {
			y += c.paragraph(schedule.Describe(s), small, 44, y-5, panelWidth-24-44) + 4
		}
		c.hit(12, row, panelWidth-24, y-row, "调度策略："+schedule.Name(s), set(func(c *schedule.Config) { c.Strategy = s }))
	}
	c.items[card].h = y - top
	y += 12

	// Who takes part, and the threshold of each.
	c.text("参与调度的账户", small, inset+2, y+6, appkit.TextAlignmentLeft, 0)
	c.text("达到即换", small, edge-2, y+6, appkit.TextAlignmentRight, 0)
	y += 16
	top, card = y, c.card(y)
	for i, ac := range a.data.Accounts {
		if i > 0 {
			c.rule(y)
		}
		m, cy := cfg.Member(ac.ID), y+15
		box, tint, ink := "checkmark.square.fill", c.accent(), primary
		if m.Off {
			box, tint, ink = "square", tertiary, tertiary
		}
		sx := panelWidth - 24 - 84.0
		c.glyph(box, 14, appkit.FontWeightRegular, tint, 30, cy)
		c.text(ac.Name, style{12, appkit.FontWeightRegular, ink, false}, 44, cy, appkit.TextAlignmentLeft, sx-8-44)
		label := "让「" + ac.Name + "」参与自动调度"
		if !m.Off {
			label = "让「" + ac.Name + "」不参与自动调度"
		}
		c.hit(12, y, sx-16, 30, label, set(func(c *schedule.Config) { m.Off = !m.Off; c.SetMember(ac.ID, m) }))
		c.capsule(sx, cy-11, 84, 22)
		c.vline(sx+24, cy-7, 14)
		c.vline(sx+60, cy-7, 14)
		c.text("−", style{13, appkit.FontWeightRegular, primary, false}, sx+12, cy, appkit.TextAlignmentCenter, 16)
		c.text(fmt.Sprintf("%d%%", m.Limit()), style{11, appkit.FontWeightSemibold, ink, true}, sx+42, cy, appkit.TextAlignmentCenter, 34)
		c.text("+", style{13, appkit.FontWeightRegular, primary, false}, sx+72, cy, appkit.TextAlignmentCenter, 16)
		limit := m.Limit()
		c.hit(sx, cy-11, 24, 22, "降低「"+ac.Name+"」的阈值", set(func(c *schedule.Config) {
			m.Threshold = max(schedule.MinThreshold, limit-schedule.Step)
			c.SetMember(ac.ID, m)
		}))
		c.hit(sx+60, cy-11, 24, 22, "提高「"+ac.Name+"」的阈值", set(func(c *schedule.Config) {
			m.Threshold = min(schedule.MaxThreshold, limit+schedule.Step)
			c.SetMember(ac.ID, m)
		}))
		y += 30
	}
	c.items[card].h = y - top
	y += 6
	y += c.paragraph("5 小时或本周任一用到阈值，就算额度不足。", small, inset+2, y, edge-inset-4) + 10

	// Moving a resumed conversation costs a cold cache, so it has its own switch.
	top, card = y, c.card(y)
	text := panelWidth - 24 - 24 - 44
	c.text("续聊达到阈值时自动换号", style{12, appkit.FontWeightSemibold, primary, false}, 24, y+15, appkit.TextAlignmentLeft, text)
	h := 27 + c.paragraph("原账户用到它的阈值才换，会重建一次缓存", small, 24, y+25, text) + 9
	on, sx, sy := !cfg.Sticky, panelWidth-24-34.0, y+h/2
	track, knob := appkit.Color_LabelColor().ColorWithAlphaComponent(.16), sx+2
	if on {
		track, knob = c.accent(), sx+16
	}
	c.fill(sx, sy-10, 34, 20, 10, track)
	c.fill(knob, sy-8, 16, 16, 8, appkit.Color_WhiteColor())
	c.items[card].h = h
	c.hit(12, top, panelWidth-24, h, "续聊达到阈值时自动换号", set(func(c *schedule.Config) { c.Sticky = !c.Sticky }))
	y += h + 10

	// Why the next conversation goes where it goes.
	why := "没有可用账户（" + a.pick.Reason + "），新对话使用默认账户。"
	if a.pick.OK() {
		why = "下一个对话用「" + a.pick.Account.Name + "」：" + a.pick.Reason + "。"
	}
	return y + c.paragraph(why, small, inset+2, y, edge-inset-4) + 12
}

// recentSection lists recent conversations in place. A conversation resumes under its
// own account, or where automatic scheduling moves it; its account label opens the other
// accounts to move it by hand.
func (a *application) recentSection(c *canvas, y float64) float64 {
	primary := appkit.Color_LabelColor()
	open := a.open == "recent"
	c.text("最近对话", style{13, appkit.FontWeightSemibold, primary, false}, inset, y+16, appkit.TextAlignmentLeft, 0)
	arrow := "chevron.right"
	if open {
		arrow = "chevron.down"
	}
	c.glyph(arrow, 11, appkit.FontWeightSemibold, primary, edge-5, y+16)
	c.hit(8, y+1, panelWidth-16, 30, "最近对话与新对话", func() { a.toggle("recent") })
	y += 32
	if open {
		y = a.recentList(c, y)
	}
	c.hline(y)
	return y
}

func (a *application) recentList(c *canvas, y float64) float64 {
	primary, secondary := appkit.Color_LabelColor(), appkit.Color_SecondaryLabelColor()
	small := style{11, appkit.FontWeightRegular, secondary, false}
	auto, cfg := a.scheduling(), a.data.Schedule
	states, now := a.states(), time.Now()
	rs := a.resumable()
	// Where each conversation resumes, decided as the launcher will.
	moves := make([]schedule.Choice, len(rs))
	var full []string
	for i, r := range rs {
		if to, ok := cfg.Resume(states, r.account.ID, a.data.Last, now); auto && ok {
			moves[i] = to
			if name := "「" + r.account.Name + "」"; !slices.Contains(full, name) {
				full = append(full, name)
			}
		}
	}
	top, card := y, c.card(y)
	if len(full) > 0 {
		hint := strings.Join(full, "") + "已达阈值，它的对话恢复时会自动换号。点账户名可以手动改。"
		strip := c.fill(12, y, panelWidth-24, 0, 12, c.accent().ColorWithAlphaComponent(.1))
		h := c.paragraph(hint, small, 24, y+7, panelWidth-48) + 14
		c.items[strip].h = h
		c.items[strip].v.Layer().SetMaskedCorners(quartzcore.KLayerMinXMaxYCorner | quartzcore.KLayerMaxXMaxYCorner)
		y += h
	}

	cy := y + 16
	c.glyph("plus.circle.fill", 15, appkit.FontWeightRegular, primary, 30, cy)
	c.text("新对话…", style{12, appkit.FontWeightSemibold, primary, false}, 44, cy, appkit.TextAlignmentLeft, 0)
	if auto && a.pick.OK() {
		c.text("→ "+a.pick.Account.Name, style{11, appkit.FontWeightRegular, c.accent(), false}, panelWidth-24, cy, appkit.TextAlignmentRight, 110)
	}
	c.hit(12, y, panelWidth-24, 32, "新对话", a.newSession)
	y += 32

	for i, r := range rs {
		c.rule(y)
		label, pst, ring := r.account.Name, style{11, appkit.FontWeightRegular, secondary, false}, false
		if moves[i].OK() {
			label, pst, ring = r.account.Name+" → "+moves[i].Account.Name, style{11, appkit.FontWeightSemibold, c.accent(), false}, true
		}
		pw := math.Min(measure(label, pst)+16, 140)
		if !ring {
			pw += 14 // room for the swap glyph
		}
		px, py := panelWidth-24-pw, y+12
		c.text(r.title, style{12, appkit.FontWeightSemibold, primary, false}, 24, y+15, appkit.TextAlignmentLeft, px-8-24)
		c.text(r.detail, small, 24, y+31, appkit.TextAlignmentLeft, px-8-24)
		if ring {
			c.outline(px, py, pw, 20, c.accent())
			c.text(label, pst, px+8, py+10, appkit.TextAlignmentLeft, pw-16)
		} else {
			c.capsule(px, py, pw, 20)
			c.text(label, pst, px+8, py+10, appkit.TextAlignmentLeft, pw-30)
			c.glyph("arrow.left.arrow.right", 8, appkit.FontWeightSemibold, secondary, px+pw-13, py+10)
		}
		c.hit(12, y, px-14, 44, "恢复「"+r.title+"」", func() { a.resume(r) })
		id := r.session.ID
		c.hit(px-4, y+6, pw+8, 32, "为「"+r.title+"」选择账户", func() {
			if a.choosing == id {
				a.choosing = ""
			} else {
				a.choosing = id
			}
			a.render()
		})
		y += 44
		if a.choosing == id {
			y = a.choices(c, r, moves[i], y)
		}
	}
	if len(rs) == 0 {
		c.rule(y)
		c.text("暂无可恢复的对话", small, 24, y+16, appkit.TextAlignmentLeft, 0)
		y += 32
	}
	c.items[card].h = y - top
	return y + 10
}

// choices lists where else a conversation can resume. Every account can run it, since
// conversations are shared; an account not logged in, or not yet sharing its folders,
// cannot.
func (a *application) choices(c *canvas, r resumable, move schedule.Choice, y float64) float64 {
	secondary, tertiary := appkit.Color_SecondaryLabelColor(), appkit.Color_TertiaryLabelColor()
	top := y
	shade := c.fill(12, y, panelWidth-24, 0, 0, appkit.Color_LabelColor().ColorWithAlphaComponent(.04))
	row := func(text, detail string, ok bool, fn func()) {
		cy := y + 14
		ink := c.accent()
		if !ok {
			ink = tertiary
		}
		c.glyph("arrow.left.arrow.right", 9, appkit.FontWeightSemibold, ink, 32, cy)
		_, dw := c.text(detail, style{10, appkit.FontWeightRegular, secondary, false}, panelWidth-24, cy, appkit.TextAlignmentRight, 90)
		c.text(text, style{11.5, appkit.FontWeightSemibold, ink, false}, 44, cy, appkit.TextAlignmentLeft, panelWidth-24-dw-8-44)
		if ok {
			c.hit(12, y, panelWidth-24, 28, text, fn)
		}
		y += 28
	}
	pending := func(ac store.Account) string {
		switch {
		case !a.scheduleState(ac).LoggedIn:
			return "未登录"
		case !a.demo && usage.Pending(ac.Profile) != nil:
			return "Claude 运行中，暂不能换"
		}
		return ""
	}
	if move.OK() {
		row("仍用「"+r.account.Name+"」恢复", "缓存还在", pending(r.account) == "", func() { a.resume(r, "--account", r.account.ID) })
	}
	busy := pending(r.account)
	for _, to := range a.data.Accounts {
		if to.ID == r.account.ID {
			continue
		}
		why := pending(to)
		if why == "" && busy != "" && busy != "未登录" {
			why = busy
		}
		detail := why
		if detail == "" {
			detail = "5 小时 —"
			if w := a.quotas[to.ID].Quota.FiveHour; w != nil {
				detail = fmt.Sprintf("5 小时 %.0f%%", w.Utilization)
			}
			if move.Account.ID == to.ID {
				detail = "自动选择"
			}
		}
		row("改用「"+to.Name+"」继续", detail, why == "", func() { a.resume(r, "--switch-account", to.ID) })
	}
	c.items[shade].h = y - top
	return y
}

func (a *application) chart(c *canvas, x, y, w, h float64) {
	n := len(a.data.Trend)
	in, out := make([]float64, n), make([]float64, n)
	top := 1.0
	for i, t := range a.data.Trend {
		in[i], out[i] = float64(t.Input+t.CacheRead+t.CacheWrite), float64(t.Output)
		top = math.Max(top, math.Max(in[i], out[i]))
	}
	// The handler runs at draw time, after this render's autorelease pool; create colors inside it.
	palette := &canvas{dark: c.dark}
	im := appkit.Image_ImageWithSizeFlippedDrawingHandler(foundation.Size{Width: w, Height: h}, false, func(foundation.Rect) bool {
		line, warm := palette.accent(), palette.warm()
		points := func(vs []float64) []foundation.Point {
			if len(vs) < 2 {
				vs = []float64{0, 0}
			}
			p := make([]foundation.Point, len(vs))
			for i, v := range vs {
				p[i] = foundation.Point{X: float64(i) * w / float64(len(vs)-1), Y: 1.5 + v/top*(h-4)}
			}
			return p
		}
		upper := points(in)
		area := curve(upper, 1.5, h-1)
		area.LineToPoint(foundation.Point{X: w, Y: 0})
		area.LineToPoint(foundation.Point{X: 0, Y: 0})
		area.ClosePath()
		appkit.NewGradientWithStartingColorEndingColor(line.ColorWithAlphaComponent(.24), line.ColorWithAlphaComponent(0)).DrawInBezierPathAngle(area, -90)
		for i, p := range []appkit.BezierPath{curve(upper, 1.5, h-1), curve(points(out), 1.5, h-1)} {
			p.SetLineWidth(1.2)
			p.SetLineJoinStyle(appkit.LineJoinStyleRound)
			p.SetLineCapStyle(appkit.LineCapStyleRound)
			if i == 0 {
				line.SetStroke()
			} else {
				warm.SetStroke()
			}
			p.Stroke()
		}
		return true
	})
	v := appkit.NewImageViewWithFrame(rect(0, 0, w, h))
	v.SetImageScaling(appkit.ImageScaleNone)
	v.SetImage(im)
	c.place(v.View, x, y, w, h)
}

// curve is a Catmull-Rom spline; control points are clamped so it never dips below zero.
func curve(p []foundation.Point, low, high float64) appkit.BezierPath {
	clamp := func(v float64) float64 { return math.Min(high, math.Max(low, v)) }
	b := appkit.NewBezierPath()
	b.MoveToPoint(p[0])
	for i := 0; i+1 < len(p); i++ {
		p0, p1, p2, p3 := p[max(i-1, 0)], p[i], p[i+1], p[min(i+2, len(p)-1)]
		b.CurveToPointControlPoint1ControlPoint2(p2,
			foundation.Point{X: p1.X + (p2.X-p0.X)/6, Y: clamp(p1.Y + (p2.Y-p0.Y)/6)},
			foundation.Point{X: p2.X - (p3.X-p1.X)/6, Y: clamp(p2.Y - (p3.Y-p1.Y)/6)})
	}
	return b
}
func quotaSummary(q status.Result) string {
	if q.Quota.FiveHour == nil && q.Quota.SevenDay == nil {
		if q.Error != "" {
			return q.Error
		}
		return "额度待刷新"
	}
	part := func(name string, w *status.Window) string {
		switch {
		case w == nil:
			return name + " —"
		case resetIn(w.ResetsAt, time.Now()) == "已重置":
			return name + " 已重置"
		}
		return fmt.Sprintf("%s %.0f%%", name, w.Utilization)
	}
	return part("5 小时", q.Quota.FiveHour) + " · " + part("本周", q.Quota.SevenDay)
}

// DarwinKit's NSArray-to-slice conversion has an uninitialized length for an empty
// NSArray. Enumerate directly so leaf views are safe without modifying native code.
func children(v appkit.View) []appkit.View {
	array := objc.Call[objc.Object](v, objc.Sel("subviews"))
	if array.IsNil() {
		return nil
	}
	n := objc.Call[uint](array, objc.Sel("count"))
	out := make([]appkit.View, 0, n)
	for i := uint(0); i < n; i++ {
		out = append(out, objc.Call[appkit.View](array, objc.Sel("objectAtIndex:"), i))
	}
	return out
}
