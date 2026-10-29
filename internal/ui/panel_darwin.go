package ui

import (
	"fmt"
	"math"
	"time"

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
		height = a.footer(c, a.accounts(c, a.usage(c, a.header(c))))
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
	notice, warn := quotaNotice(q)
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
	if ac.Default {
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
		c.text(quotaSummary(q), small, column, y+42, appkit.TextAlignmentLeft, edge-column)
	} else {
		email := ac.Email
		if email == "" {
			email = "登录后显示账户信息"
		}
		action := "设为默认"
		if ac.Default {
			action = "新对话默认"
		}
		room := edge - column - measure(action, small) - 12
		c.text(email, style{12, appkit.FontWeightRegular, secondary, false}, column, y+40, appkit.TextAlignmentLeft, room)
		now := time.Now()
		c.quota("5 小时", q.Quota.FiveHour, y+63, now)
		c.quota("本周", q.Quota.SevenDay, y+97, now)
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
		a.render()
	})
	switch {
	case open && ac.Default:
		c.text("新对话默认", small, edge, y+40, appkit.TextAlignmentRight, 0)
	case open:
		_, w := c.text("设为默认", style{11, appkit.FontWeightMedium, c.accent(), false}, edge, y+40, appkit.TextAlignmentRight, 0)
		c.hit(edge-w-8, y+30, w+16, 20, "设为新对话的默认账户", func() { a.selectAccount(ac) })
	}
	return y + h
}

// quota draws one window: label, bar and percentage, with the time to its reset below.
// Once the reset has passed, the reading no longer applies until Claude refreshes it.
func (c *canvas) quota(title string, w *status.Window, cy float64, now time.Time) {
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
			c.fill(94, cy-1.5, 130*math.Min(100, math.Max(0, w.Utilization))/100, 3, 1.5, c.accent())
		}
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
	y += 4
	c.glyph("lock.fill", 13, appkit.FontWeightRegular, primary, 26, y+22)
	c.text("续聊沿用原账户", style{12, appkit.FontWeightRegular, secondary, false}, column, y+22, appkit.TextAlignmentLeft, 0)
	y += 40
	c.hline(y)

	c.text("最近对话", style{13, appkit.FontWeightSemibold, primary, false}, inset, y+16, appkit.TextAlignmentLeft, 0)
	c.glyph("chevron.right", 11, appkit.FontWeightSemibold, primary, edge-5, y+16)
	below := y + 32
	c.hit(8, y+1, panelWidth-16, 30, "最近对话与新对话", func() { a.recent(inset, below) })
	y += 32
	c.hline(y)

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
