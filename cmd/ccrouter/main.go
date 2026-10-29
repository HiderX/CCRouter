package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ccrouter/internal/session"
	"ccrouter/internal/status"
	"ccrouter/internal/store"
	"ccrouter/internal/usage"
)

const version = "0.1.1"

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "CCRouter:", e)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") {
		fmt.Println("CCRouter", version)
		return nil
	}
	if len(args) > 0 && (args[0] == "--help" || args[0] == "help") {
		fmt.Print(help)
		return nil
	}
	if len(args) > 0 && (args[0] == "gui" || args[0] == "demo") {
		return launchGUI(args)
	}
	root, e := store.DefaultRoot()
	if e != nil {
		return e
	}
	s, e := store.Open(root)
	if e != nil {
		return e
	}
	defer s.Close()
	if len(args) == 0 {
		return launch(s, nil)
	}
	switch args[0] {
	case "run":
		// The non-interactive form the menu bar uses: default account, no picker.
		account, rest := splitAccount(args[1:])
		cwd, e := os.Getwd()
		if e != nil {
			return e
		}
		return start(s, account, rest, cwd)
	case "account":
		return accounts(s, args[1:])
	case "sessions":
		vs, e := s.Recent("", 100)
		if e != nil {
			return e
		}
		as, _ := s.Accounts()
		names := map[string]string{}
		for _, a := range as {
			names[a.ID] = a.Name
		}
		for _, v := range vs {
			fmt.Printf("%s\t%s\t%s\t%s\n", v.ID, names[v.AccountID], time.Unix(v.Updated, 0).Local().Format("01-02 15:04"), v.CWD)
		}
		return nil
	case "scan":
		if e = (&usage.Indexer{Store: s}).Scan(); e != nil {
			return e
		}
		fmt.Println("会话和用量索引已更新。")
		return nil
	case "stats":
		f := flag.NewFlagSet("stats", flag.ContinueOnError)
		account := f.String("account", "", "账户 ID 或名称；省略为总用量")
		days := f.Int("days", 1, "统计最近几天；0 表示全部")
		if e = f.Parse(args[1:]); e != nil {
			return e
		}
		if *days < 0 {
			return errors.New("days 不能为负数")
		}
		id := ""
		if *account != "" {
			a, e := s.Account(*account)
			if e != nil {
				return e
			}
			id = a.ID
		}
		now := time.Now()
		start := time.Unix(0, 0)
		if *days > 0 {
			start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1-*days)
		}
		t, e := s.Totals(id, start, now.Add(time.Second))
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(t)
	case "quota":
		r := status.New(s)
		as, e := s.Accounts()
		if e != nil {
			return e
		}
		for _, a := range as {
			q := r.Quota(context.Background(), a, false)
			v := struct {
				Account string
				status.Result
			}{a.Name, q}
			if e = json.NewEncoder(os.Stdout).Encode(v); e != nil {
				return e
			}
		}
		return nil
	case "ip":
		x, _, e := status.New(s).IP(context.Background(), false)
		if e != nil {
			return e
		}
		fmt.Println(strings.TrimSpace(x.IP + " " + x.Country))
		return nil
	default:
		return launch(s, args)
	}
}
func launchGUI(args []string) error {
	self, e := os.Executable()
	if e != nil {
		return e
	}
	// ccrouter is usually reached through a symlink on PATH; the tray sits beside the real file.
	if real, err := filepath.EvalSymlinks(self); err == nil {
		self = real
	}
	bin := filepath.Join(filepath.Dir(self), "ccrouter-tray")
	if _, e = os.Stat(bin); e != nil {
		return errors.New("未找到原生菜单栏程序，请先执行 make app；命令行功能可独立使用")
	}
	return session.Exec(session.Plan{Binary: bin, Args: args, Env: os.Environ()})
}
func accounts(s *store.Store, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		as, e := s.Accounts()
		if e != nil {
			return e
		}
		for _, a := range as {
			mark := " "
			if a.Default {
				mark = "*"
			}
			fmt.Printf("%s %s\t%s\t%s\n", mark, a.ID, a.Name, a.Email)
		}
		return nil
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return errors.New("用法：ccrouter account add 账户名称")
		}
		a, e := s.AddAccount(args[1], "")
		if e != nil {
			return e
		}
		fmt.Println("已创建独立账户：", a.Name)
		return session.Login(s, a)
	case "import":
		f := flag.NewFlagSet("account import", flag.ContinueOnError)
		name := f.String("name", "当前 Claude 账户", "账户名称")
		profile := f.String("profile", "", "已有配置目录；默认 ~/.claude")
		if e := f.Parse(args[1:]); e != nil {
			return e
		}
		if *profile == "" {
			h, _ := os.UserHomeDir()
			*profile = filepath.Join(h, ".claude")
		}
		if _, e := os.Stat(*profile); e != nil {
			return e
		}
		a, e := s.AddAccount(*name, *profile)
		if e != nil {
			return e
		}
		email, plan, e := session.ReadIdentity(context.Background(), a)
		if e == nil {
			s.UpdateAccount(a.ID, email, plan)
		}
		fmt.Println("已登记账户：", a.Name)
		return (&usage.Indexer{Store: s}).Scan()
	case "use":
		if len(args) != 2 {
			return errors.New("用法：ccrouter account use 账户名称或ID")
		}
		return s.SetDefault(args[1])
	case "rename":
		if len(args) != 3 {
			return errors.New("用法：ccrouter account rename 名称或ID 新名称")
		}
		return s.RenameAccount(args[1], args[2])
	case "login":
		if len(args) != 2 {
			return errors.New("用法：ccrouter account login 账户名称或ID")
		}
		a, e := s.Account(args[1])
		if e != nil {
			return e
		}
		return session.Login(s, a)
	default:
		return fmt.Errorf("未知账户操作 %q", args[0])
	}
}

const help = `CCRouter — Claude 账户与会话管理（无反向代理）

像 claude 一样使用：
ccrouter [Claude 参数]            在当前目录开新对话；终端中先选择账户（回车用默认）
ccrouter --resume [搜索词]        从所有账户的对话中选择并恢复，自动进入其项目目录
ccrouter --resume 对话UUID        用该对话原账户恢复
ccrouter -c                       恢复当前目录最近的已绑定对话
ccrouter --account 名称 [参数]    跳过选择，指定新对话的账户

管理：
ccrouter gui                      打开 macOS 菜单栏面板
ccrouter demo                     用独立的示例数据预览面板，不读取凭据
ccrouter run [--account 名称] [参数]
                                  不弹出选择，直接用默认或指定账户启动
ccrouter account add 名称         新建独立账户并完成官方登录
ccrouter account import           登记当前 ~/.claude 账户
ccrouter account list             列出账户
ccrouter account use 名称或ID     设置新对话默认账户
ccrouter account login 名称或ID   重新登录指定账户
ccrouter account rename 名称或ID 新名称
                                  修改账户显示名称
ccrouter sessions                 列出已绑定对话
ccrouter scan                     增量索引各账户本地会话记录
ccrouter stats [--account 名称] [--days 1|7|0]
ccrouter quota                    查询各账户的服务端额度窗口
ccrouter ip                       查询 IP 探测服务看到的出口地址

CCROUTER_HOME 可指定数据目录；CCROUTER_CLAUDE 可指定原版 Claude 可执行文件。
`
