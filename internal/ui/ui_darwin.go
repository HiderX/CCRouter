package ui

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"ccrouter/internal/icon"
	"ccrouter/internal/session"
	"ccrouter/internal/status"
	"ccrouter/internal/store"
	"ccrouter/internal/usage"
	"github.com/progrium/darwinkit/dispatch"
	"github.com/progrium/darwinkit/helper/action"
	"github.com/progrium/darwinkit/macos"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/objc"
)

type application struct {
	s           *store.Store
	index       *usage.Indexer
	reader      *status.Reader
	app         appkit.Application
	item        appkit.StatusItem
	pop         appkit.Popover
	controller  appkit.ViewController
	data        snapshot
	expanded    map[string]bool
	quotas      map[string]status.Result
	period      int
	exit        status.Exit
	ipErr       string
	notice      string
	ipTime      time.Time
	copied      bool
	dark        bool // appearance used by the last render
	refreshing  bool
	content     appkit.View // current document view; menus anchor in its coordinates
	height      float64
	demo        bool
	ctx         context.Context
	cancel      context.CancelFunc
	watchCancel context.CancelFunc
}

func mainThread(f func()) { dispatch.MainQueue().DispatchAsync(func() { objc.WithAutoreleasePool(f) }) }

func Run(s *store.Store, demo bool) error {
	lock, e := os.OpenFile(filepath.Join(s.Root, "menu.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return fmt.Errorf("CCRouter 菜单栏已在运行")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &application{s: s, index: &usage.Indexer{Store: s}, reader: status.New(s), expanded: map[string]bool{}, quotas: map[string]status.Result{}, ctx: ctx, cancel: cancel, demo: demo}
	macos.RunApp(func(app appkit.Application, delegate *appkit.ApplicationDelegate) {
		a.app = app
		app.SetActivationPolicy(appkit.ApplicationActivationPolicyAccessory)
		a.item = appkit.StatusBar_SystemStatusBar().StatusItemWithLength(28)
		a.item.Retain()
		a.item.Button().SetImage(icon.MenuBar())
		a.item.Button().SetToolTip("CCRouter · 续聊沿用原账户")
		a.pop = appkit.NewPopover()
		a.pop.Retain()
		a.pop.SetBehavior(appkit.PopoverBehaviorTransient)
		a.pop.SetAnimates(false)
		a.controller = appkit.NewViewController()
		a.pop.SetContentViewController(a.controller)
		popoverDelegate := &appkit.PopoverDelegate{}
		popoverDelegate.SetPopoverDidClose(func(foundation.Notification) {
			a.controller.SetView(appkit.NewViewWithFrame(rect(0, 0, panelWidth, 1)))
		})
		a.pop.SetDelegate(popoverDelegate)
		action.Set(a.item.Button(), func(objc.Object) {
			if a.pop.IsShown() {
				a.pop.Close()
				return
			}
			a.show()
		})
		if demo {
			a.seedDemo()
			a.show()
			if dir := os.Getenv("CCROUTER_SMOKE"); dir != "" {
				a.smoke(dir)
			}
			if path := os.Getenv("CCROUTER_SNAPSHOT"); path != "" {
				time.AfterFunc(time.Second, func() { mainThread(func() { a.exportSnapshot(path); a.app.Terminate(nil) }) })
			}
		} else {
			a.restartWatch()
			a.refreshLocal()
		}
	})
	return nil
}
func (a *application) show() {
	a.render()
	a.app.ActivateIgnoringOtherApps(true)
	b := a.item.Button()
	a.pop.ShowRelativeToRectOfViewPreferredEdge(b.Bounds(), b, foundation.RectEdgeMaxY)
	// A status-item popover follows the menu bar's appearance, known only once shown.
	if isDark(a.pop.EffectiveAppearance()) != a.dark {
		a.render()
	}
	if !a.demo {
		a.refreshLocal()
		a.refreshRemote(false)
	}
}
func (a *application) restartWatch() {
	if a.watchCancel != nil {
		a.watchCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.watchCancel = cancel
	go func() {
		e := a.index.Watch(ctx, func(err error) {
			if err != nil {
				mainThread(func() { a.notice = err.Error(); a.refreshLocal() })
			} else {
				a.refreshLocalAsync()
			}
		})
		if e != nil {
			mainThread(func() {
				a.notice = "用量监听失败：" + e.Error()
				if a.pop.IsShown() {
					a.render()
				}
			})
		}
	}()
}
func (a *application) refreshLocalAsync() { mainThread(a.refreshLocal) }
func (a *application) refreshLocal() {
	if a.demo {
		return
	}
	period := a.period
	go func() {
		v, e := load(a.s, period)
		mainThread(func() {
			if e != nil {
				a.notice = e.Error()
			} else if a.period == period {
				a.data = v
				for _, ac := range v.Accounts {
					if ac.Default {
						if _, ok := a.expanded[ac.ID]; !ok {
							a.expanded[ac.ID] = true
						}
					}
				}
			}
			if a.pop.IsShown() {
				a.render()
			}
		})
	}()
}
func (a *application) refreshRemote(force bool) {
	if a.demo || a.refreshing {
		return
	}
	a.refreshing = true
	go func() {
		accounts, _ := a.s.Accounts()
		ip, ts, e := a.reader.IP(a.ctx, force)
		mainThread(func() {
			if e == nil {
				a.exit, a.ipTime, a.ipErr = ip, ts, ""
			} else {
				a.ipErr = e.Error()
			}
			if a.pop.IsShown() {
				a.render()
			}
		})
		for _, ac := range accounts {
			q := a.reader.Quota(a.ctx, ac, force)
			mainThread(func() {
				a.quotas[ac.ID] = q
				if a.pop.IsShown() {
					a.render()
				}
			})
		}
		mainThread(func() {
			a.refreshing = false
			if a.pop.IsShown() {
				a.render()
			}
		})
	}()
}

// exportSnapshot renders the actual AppKit view for layout verification, without screen capture permission.
// It is available only in isolated demo mode. The popover material is not captured, so a
// window-background fill stands in for it.
func (a *application) exportSnapshot(path string) {
	v := a.controller.View()
	look := plain(isDark(a.pop.EffectiveAppearance()))
	// Offscreen caching does not apply the window's appearance to text; set it explicitly.
	objc.Call[objc.Void](v, objc.Sel("setAppearance:"), look)
	defer objc.Call[objc.Void](v, objc.Sel("setAppearance:"), objc.Object{})
	v.SetWantsLayer(true)
	look.PerformAsCurrentDrawingAppearance(func() {
		v.Layer().SetBackgroundColor(appkit.Color_WindowBackgroundColor().CGColor())
	})
	defer v.Layer().SetBackgroundColor(0)
	v.LayoutSubtreeIfNeeded()
	v.DisplayIfNeeded()
	rep := v.BitmapImageRepForCachingDisplayInRect(v.Bounds())
	if rep.IsNil() {
		fmt.Fprintln(os.Stderr, "无法生成界面快照")
		return
	}
	v.CacheDisplayInRectToBitmapImageRep(v.Bounds(), rep)
	data := rep.RepresentationUsingTypeProperties(appkit.BitmapImageFileTypePNG, nil)
	if e := os.WriteFile(path, data, 0600); e != nil {
		fmt.Fprintln(os.Stderr, e)
	} else {
		fmt.Println("界面快照：", path)
	}
}
func (a *application) seedDemo() {
	// Explicit demo mode is isolated from credentials and the network.
	a.data = snapshot{ByAccount: map[string]store.Totals{}, Trend: make([]store.Totals, trendBuckets)}
	names := []string{"个人账户", "工作账户", "备用账户"}
	plans := []string{"max", "team", "pro"}
	emails := []string{"personal@example.com", "work@example.com", "backup@example.com"}
	for i, n := range names {
		id := fmt.Sprint(i)
		ac := store.Account{ID: id, Name: n, Email: emails[i], Plan: plans[i], Default: i == 0}
		a.data.Accounts = append(a.data.Accounts, ac)
		factor := []int64{3, 2, 1}[i]
		a.data.ByAccount[id] = store.Totals{Input: 40000 * factor, Output: 20000 * factor, CacheRead: 300000 * factor, CacheWrite: 40000 * factor, Requests: 26 * factor}
		a.quotas[id] = status.Result{Quota: status.Quota{FiveHour: &status.Window{Utilization: []float64{32, 68, 12}[i]}, SevenDay: &status.Window{Utilization: []float64{54, 41, 22}[i]}}, Updated: time.Now()}
	}
	a.expanded["0"] = true
	a.data.Total = store.Totals{Input: 240000, Output: 120000, CacheRead: 1800000, CacheWrite: 240000, Requests: 156}
	bump := func(x, at, width float64) float64 { return math.Exp(-math.Pow((x-at)/width, 2)) }
	for i := range a.data.Trend {
		x := float64(i) / float64(len(a.data.Trend)-1)
		in := .42 + .36*bump(x, .22, .05) + .44*bump(x, .86, .04) - .18*bump(x, .62, .05) + .05*math.Sin(x*41) + .03*math.Cos(x*23)
		out := .1 + .02*math.Sin(x*29) + .05*bump(x, .86, .05)
		a.data.Trend[i] = store.Totals{Input: int64(in * 40000), Output: int64(out * 40000)}
	}
	a.exit, a.ipTime = status.Exit{IP: "203.0.113.24", Country: "JP"}, time.Now()
}
func (a *application) refreshAll() {
	if !a.demo {
		go func() {
			e := a.index.Scan()
			mainThread(func() {
				a.notice = ""
				if e != nil {
					a.notice = e.Error()
				}
				a.refreshLocal()
			})
		}()
	}
	a.refreshRemote(true)
}

// exitText lists labels from most to least detailed; the header shows the first that fits.
func (a *application) exitText() (labels []string, tip string) {
	switch {
	case a.exit.IP != "":
		labels, tip = []string{a.exit.IP}, a.exit.IP
		if r := region(a.exit.Country); r != "" {
			labels = []string{r + " · " + a.exit.IP, a.exit.IP}
			tip = labels[0]
		}
		tip += "\n点击复制。此 IP 为 ipify 探测结果，分流规则可能使 Claude 的实际出口不同。\n探测于 " + a.ipTime.Local().Format("15:04")
	case a.ipErr != "":
		labels, tip = []string{"IP 查询失败"}, a.ipErr+"，点击重试。"
	default:
		labels, tip = []string{"查询出口 IP"}, "点击查询出口 IP"
	}
	return
}
func (a *application) copyIP() {
	if a.exit.IP == "" {
		a.refreshRemote(true)
		return
	}
	pb := appkit.Pasteboard_GeneralPasteboard()
	pb.ClearContents()
	pb.SetStringForType(a.exit.IP, appkit.PasteboardTypeString)
	a.copied = true
	a.render()
	time.AfterFunc(1200*time.Millisecond, func() {
		mainThread(func() {
			a.copied = false
			if a.pop.IsShown() {
				a.render()
			}
		})
	})
}
func (a *application) quit() { a.cancel(); a.app.Terminate(nil) }

// popUp shows a menu with its top-left corner at a top-down point in the panel.
func (a *application) popUp(m appkit.Menu, x, y float64) {
	m.PopUpMenuPositioningItemAtLocationInView(nil, foundation.Point{X: x, Y: a.height - y}, a.content)
}
func (a *application) alert(title, message string) {
	a.pop.Close()
	a.app.ActivateIgnoringOtherApps(true)
	v := appkit.NewAlert()
	v.SetMessageText(title)
	v.SetInformativeText(message)
	v.AddButtonWithTitle("好")
	v.RunModal()
}
func (a *application) selectAccount(ac store.Account) {
	if a.demo {
		for i := range a.data.Accounts {
			a.data.Accounts[i].Default = a.data.Accounts[i].ID == ac.ID
		}
		a.render()
		return
	}
	if e := a.s.SetDefault(ac.ID); e != nil {
		a.alert("切换失败", e.Error())
		return
	}
	a.refreshLocal()
}
func (a *application) addAccount() {
	if a.demo {
		return
	}
	a.pop.Close()
	a.app.ActivateIgnoringOtherApps(true)
	alert := appkit.NewAlert()
	alert.SetMessageText("添加 Claude 账户")
	alert.SetInformativeText("为账户命名。随后将在终端中完成 Claude 官方登录。")
	input := appkit.NewTextFieldWithFrame(rect(0, 0, 250, 24))
	input.SetPlaceholderString("例如：个人账户")
	alert.SetAccessoryView(input)
	alert.AddButtonWithTitle("继续登录")
	alert.AddButtonWithTitle("取消")
	if alert.RunModal() != 1000 {
		return
	}
	ac, e := a.s.AddAccount(input.StringValue(), "")
	if e != nil {
		a.alert("添加失败", e.Error())
		return
	}
	a.expanded[ac.ID] = true
	a.restartWatch()
	a.refreshLocal()
	a.terminal("", []string{"account", "login", ac.ID})
}
func (a *application) importDefault() {
	if a.demo {
		return
	}
	h, _ := os.UserHomeDir()
	profile := filepath.Join(h, ".claude")
	if _, e := os.Stat(profile); e != nil {
		a.alert("没有找到现有账户", "请先通过“添加账户”完成 Claude 登录。")
		return
	}
	ac, e := a.s.AddAccount("当前 Claude 账户", profile)
	if e != nil {
		a.alert("导入失败", e.Error())
		return
	}
	a.expanded[ac.ID] = true
	a.restartWatch()
	a.refreshLocal()
	go func() {
		email, plan, e := session.ReadIdentity(a.ctx, ac)
		if e == nil {
			a.s.UpdateAccount(ac.ID, email, plan)
		}
		mainThread(func() {
			if e != nil {
				a.notice = e.Error()
			}
			a.refreshLocal()
		})
	}()
}
func (a *application) newSession() {
	if a.demo {
		return
	}
	if len(a.data.Accounts) == 0 {
		a.addAccount()
		return
	}
	a.pop.Close()
	if !a.defaultLoggedIn() {
		return
	}
	p := appkit.OpenPanel_OpenPanel()
	p.SetCanChooseDirectories(true)
	p.SetCanChooseFiles(false)
	p.SetAllowsMultipleSelection(false)
	p.SetMessage("选择 Claude 的工作目录")
	if p.RunModal() == 1 {
		a.terminal(p.URL().Path(), []string{"run"})
	}
}

// defaultLoggedIn makes sure a new conversation will not start in Claude's login flow.
// It offers to log in, or to switch the default to an account that has credentials.
func (a *application) defaultLoggedIn() bool {
	var def store.Account
	var other *store.Account
	for i, ac := range a.data.Accounts {
		switch {
		case ac.Default:
			def = ac
		case other == nil && session.LoggedIn(ac.Profile):
			other = &a.data.Accounts[i]
		}
	}
	if def.ID == "" || session.LoggedIn(def.Profile) {
		return true
	}
	a.app.ActivateIgnoringOtherApps(true)
	v := appkit.NewAlert()
	v.SetMessageText("「" + def.Name + "」尚未登录")
	info := "新对话使用默认账户。"
	if other != nil {
		info += "可以先登录它，或把默认账户改为已登录的「" + other.Name + "」。"
	}
	v.SetInformativeText(info)
	v.AddButtonWithTitle("登录「" + def.Name + "」")
	if other != nil {
		v.AddButtonWithTitle("改用「" + other.Name + "」")
	}
	v.AddButtonWithTitle("取消")
	switch r := v.RunModal(); {
	case r == 1000:
		a.terminal("", []string{"account", "login", def.ID})
	case r == 1001 && other != nil:
		if e := a.s.SetDefault(other.ID); e != nil {
			a.alert("切换失败", e.Error())
			return false
		}
		a.refreshLocal()
		return true
	}
	return false
}
func (a *application) recent(x, y float64) {
	m := appkit.NewMenuWithTitle("最近对话")
	a.menuItem(m, "新对话…", a.newSession)
	m.AddItem(appkit.MenuItem_SeparatorItem())
	shown := 0
	for _, s := range a.data.Recent {
		if shown == 12 {
			break
		}
		var ac store.Account
		for _, v := range a.data.Accounts {
			if v.ID == s.AccountID {
				ac = v
			}
		}
		// No title means no prompt was ever sent, so there is nothing to resume.
		title := usage.Title(ac.Profile, s.CWD, s.ID)
		if title == "" {
			continue
		}
		shown++
		project := filepath.Base(s.CWD)
		if s.CWD == "" {
			project = "未知目录"
		}
		detail := project + " · " + usage.When(time.Unix(s.Updated, 0)) + " · " + ac.Name
		i := a.menuItem(m, title, func() { a.terminal(s.CWD, []string{"run", "--resume", s.ID}) })
		// NSMenuItem.subtitle needs macOS 14.4; older systems get one line.
		if i.RespondsToSelector(objc.Sel("setSubtitle:")) {
			objc.Call[objc.Void](i, objc.Sel("setSubtitle:"), detail)
		} else {
			i.SetTitle(title + " — " + detail)
		}
		i.SetToolTip(s.CWD + "\n" + s.ID)
	}
	if shown == 0 {
		a.menuItem(m, "暂无可恢复的对话", nil)
	}
	a.popUp(m, x, y)
}

// menuItem adds an item; one without an action stays disabled unless it gets a submenu.
func (a *application) menuItem(m appkit.Menu, title string, fn func()) appkit.MenuItem {
	i := appkit.NewMenuItemWithTitleActionKeyEquivalent(title, objc.Selector{}, "")
	if fn != nil {
		action.Set(i, func(objc.Object) { fn() })
	}
	m.AddItem(i)
	return i
}
func (a *application) rename(ac store.Account) {
	if a.demo {
		return
	}
	a.pop.Close()
	a.app.ActivateIgnoringOtherApps(true)
	v := appkit.NewAlert()
	v.SetMessageText("重命名「" + ac.Name + "」")
	v.SetInformativeText("只修改 CCRouter 中的显示名称，不影响登录和已有对话。")
	input := appkit.NewTextFieldWithFrame(rect(0, 0, 250, 24))
	input.SetStringValue(ac.Name)
	v.SetAccessoryView(input)
	v.AddButtonWithTitle("保存")
	v.AddButtonWithTitle("取消")
	objc.Call[objc.Void](v.Window(), objc.Sel("setInitialFirstResponder:"), input)
	if v.RunModal() != 1000 {
		return
	}
	if e := a.s.RenameAccount(ac.ID, input.StringValue()); e != nil {
		a.alert("重命名失败", e.Error())
		return
	}
	a.refreshLocal()
}
func (a *application) settings(x, y float64) {
	m := appkit.NewMenuWithTitle("设置")
	a.menuItem(m, "导入当前 Claude 账户", a.importDefault)
	a.menuItem(m, "刷新账户资料", func() {
		go func() {
			acs, _ := a.s.Accounts()
			for _, ac := range acs {
				email, plan, e := session.ReadIdentity(a.ctx, ac)
				if e == nil {
					a.s.UpdateAccount(ac.ID, email, plan)
				}
			}
			a.refreshLocalAsync()
		}()
	})
	if len(a.data.Accounts) > 0 {
		rename, login := appkit.NewMenuWithTitle("重命名账户"), appkit.NewMenuWithTitle("重新登录")
		for _, ac := range a.data.Accounts {
			a.menuItem(rename, ac.Name, func() { a.rename(ac) })
			a.menuItem(login, ac.Name, func() { a.terminal("", []string{"account", "login", ac.ID}) })
		}
		a.menuItem(m, "重命名账户", nil).SetSubmenu(rename)
		a.menuItem(m, "重新登录", nil).SetSubmenu(login)
	}
	a.menuItem(m, "打开数据目录", func() { go func() { _ = exec.Command("/usr/bin/open", a.s.Root).Run() }() })
	a.menuItem(m, "关于 CCRouter", func() {
		a.alert("CCRouter 0.1", "Go · 原生 AppKit\n按对话绑定账户，Claude 直接发送请求。\n\n额度为服务器窗口用量；Token 统计仅覆盖本机已记录会话。\nIP 为独立探测结果，分流时可能与 Claude 出口不同。")
	})
	a.popUp(m, x, y)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (a *application) terminal(cwd string, args []string) {
	if a.demo {
		return
	}
	a.pop.Close()
	bin, e := os.Executable()
	if e != nil {
		a.alert("启动失败", e.Error())
		return
	}
	bin = filepath.Join(filepath.Dir(bin), "ccrouter")
	if _, e = os.Stat(bin); e != nil {
		a.alert("启动失败", "缺少配套的 ccrouter 启动器，请重新构建完整应用包。")
		return
	}
	command := ""
	if cwd != "" {
		command = "cd " + shellQuote(cwd) + " && "
	}
	command += "CCROUTER_HOME=" + shellQuote(a.s.Root) + " " + shellQuote(bin)
	for _, v := range args {
		command += " " + shellQuote(v)
	}
	// Pass the command as argv, never interpolate it into AppleScript source.
	script := "on run argv\n tell application \"Terminal\"\n activate\n do script (item 1 of argv)\n end tell\nend run"
	go func() {
		e := exec.Command("/usr/bin/osascript", "-e", script, command).Run()
		if e != nil {
			mainThread(func() {
				a.alert("无法打开终端", "请允许 CCRouter 控制 Terminal，或从终端使用 ccrouter 命令。")
			})
		}
	}()
}
