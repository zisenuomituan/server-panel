package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"serverpanel/internal/auth"
	"serverpanel/internal/bindkey"
	"serverpanel/internal/config"
	"serverpanel/internal/model"
	"serverpanel/internal/store"
	"serverpanel/internal/version"
)

var cliHelp = "server-panel " + version.Version + `


用法:
  center [serve] [-config 路径]          启动 Web 面板（默认命令）
  center user add <用户名> [选项]        新建账号
  center user list                       列出账号
  center user passwd <用户名>            修改密码
  center user rm <用户名>                删除账号
  center host add [选项]                 登记宿主机
  center host list                       列出宿主机
  center host rm <id>                    删除宿主机（连同其虚拟机和密钥）
  center key issue (-host <id> | -server <id>) [选项]   生成绑定密钥
  center key list                        列出密钥
  center key revoke <id>                 撤销密钥
  center backup [文件]                   备份数据库
  center restore <文件>                  恢复数据库（恢复后重启服务）
  center version                         版本号

选项:
  -config <路径>      配置文件，默认 center.json

  user add:   -password <密码>  -role admin|operator|viewer  -email <邮箱>
  user passwd:-password <密码>
  host add:   -name <名称> -host <地址> -port <端口> -user <SSH用户> -uri <libvirtURI>
  key issue:  -days <有效天数，0=长期>
`

func runCLI(configPath string, args []string) int {
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取配置失败:", err)
		return 1
	}

	switch args[0] {
	case "version":
		fmt.Println("server-panel", version.Version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(cliHelp)
		return 0
	case "backup":
		return cliBackup(cfg, args[1:])
	case "restore":
		return cliRestore(cfg, args[1:])
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败:", err)
		return 1
	}
	defer st.Close()

	switch args[0] {
	case "user":
		return cliUser(st, args[1:])
	case "host":
		return cliHost(st, args[1:])
	case "key":
		return cliKey(st, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n%s", args[0], cliHelp)
		return 2
	}
}

func cliBackup(cfg *config.Config, args []string) int {
	path := ""
	if len(args) > 0 {
		path = args[0]
	} else {
		path = "panel-backup-" + time.Now().Format("20060102-150405") + ".db"
	}
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintln(os.Stderr, "目标文件已存在:", path)
		return 1
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return fail(err)
	}
	defer st.Close()
	if err := st.Backup(path); err != nil {
		return fail(err)
	}
	fmt.Println("已备份到", path)
	return 0
}

func cliRestore(cfg *config.Config, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: center restore <备份文件>")
		return 2
	}
	src := args[0]
	if _, err := os.Stat(src); err != nil {
		return fail(fmt.Errorf("备份文件不存在: %s", src))
	}
	// 先把当前的库另存一份，防手滑
	if _, err := os.Stat(cfg.DBPath); err == nil {
		bak := cfg.DBPath + ".before-restore-" + time.Now().Format("20060102-150405")
		if err := copyFile(cfg.DBPath, bak); err != nil {
			return fail(err)
		}
		fmt.Println("当前数据库已另存为", bak)
	}
	if err := copyFile(src, cfg.DBPath); err != nil {
		return fail(err)
	}
	_ = os.Remove(cfg.DBPath + "-wal")
	_ = os.Remove(cfg.DBPath + "-shm")
	fmt.Println("已恢复。请重启服务: systemctl restart panel-center")
	return 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// ---------- 账号 ----------

func cliUser(st *store.Store, args []string) int {
	if len(args) == 0 {
		fmt.Print("用法: center user add|list|passwd|rm\n")
		return 2
	}

	switch args[0] {
	case "list":
		users, err := st.Users()
		if err != nil {
			return fail(err)
		}
		if len(users) == 0 {
			fmt.Println("还没有账号")
			return 0
		}
		fmt.Printf("%-4s %-20s %-10s %s\n", "ID", "用户名", "角色", "创建时间")
		for _, u := range users {
			fmt.Printf("%-4d %-20s %-10s %s\n", u.ID, u.Username, u.Role, u.CreatedAt.Format("2006-01-02 15:04"))
		}

	case "add":
		fs := flag.NewFlagSet("user add", flag.ContinueOnError)
		pw := fs.String("password", "", "密码")
		role := fs.String("role", "", "角色")
		email := fs.String("email", "", "邮箱")
		rest, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return fail(err)
		}
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "用法: center user add <用户名> [-password x] [-role admin|operator|viewer]")
			return 2
		}
		name := rest[0]

		if *role == "" {
			// 第一个账号默认管理员，其余默认操作员
			if n, _ := st.CountUsers(); n == 0 {
				*role = model.RoleAdmin
			} else {
				*role = model.RoleOperator
			}
		}
		if *role != model.RoleAdmin && *role != model.RoleOperator && *role != model.RoleViewer {
			fmt.Fprintln(os.Stderr, "角色只能是 admin / operator / viewer")
			return 2
		}
		if *pw == "" {
			var err error
			*pw, err = promptNewPassword()
			if err != nil || *pw == "" {
				fmt.Fprintln(os.Stderr, "需要密码")
				return 1
			}
		}

		hash, err := auth.HashPassword(*pw)
		if err != nil {
			return fail(err)
		}
		u := &model.User{Username: name, Email: *email, PasswordHash: hash, Role: *role}
		if err := st.CreateUser(u); err != nil {
			return fail(err)
		}
		fmt.Printf("已创建账号 %s（%s，id=%d）\n", u.Username, u.Role, u.ID)

	case "passwd":
		fs := flag.NewFlagSet("user passwd", flag.ContinueOnError)
		pw := fs.String("password", "", "新密码")
		rest, err := parseInterspersed(fs, args[1:])
		if err != nil {
			return fail(err)
		}
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "用法: center user passwd <用户名>")
			return 2
		}
		u, err := st.UserByName(rest[0])
		if err != nil {
			return fail(fmt.Errorf("找不到账号 %s", rest[0]))
		}
		if *pw == "" {
			*pw, err = promptNewPassword()
			if err != nil || *pw == "" {
				fmt.Fprintln(os.Stderr, "需要密码")
				return 1
			}
		}
		hash, err := auth.HashPassword(*pw)
		if err != nil {
			return fail(err)
		}
		if err := st.UpdatePassword(u.ID, hash); err != nil {
			return fail(err)
		}
		fmt.Printf("已修改 %s 的密码\n", u.Username)

	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: center user rm <用户名>")
			return 2
		}
		u, err := st.UserByName(args[1])
		if err != nil {
			return fail(fmt.Errorf("找不到账号 %s", args[1]))
		}
		if err := st.DeleteUser(u.ID); err != nil {
			return fail(err)
		}
		fmt.Printf("已删除账号 %s\n", u.Username)

	default:
		fmt.Fprintln(os.Stderr, "用法: center user add|list|passwd|rm")
		return 2
	}
	return 0
}

// ---------- 宿主机 ----------

func cliHost(st *store.Store, args []string) int {
	if len(args) == 0 {
		fmt.Print("用法: center host add|list|rm\n")
		return 2
	}

	switch args[0] {
	case "list":
		hosts, err := st.Hosts()
		if err != nil {
			return fail(err)
		}
		if len(hosts) == 0 {
			fmt.Println("还没有登记宿主机")
			return 0
		}
		fmt.Printf("%-4s %-16s %-24s %-8s %s\n", "ID", "名称", "地址", "状态", "libvirt")
		for _, h := range hosts {
			addr := fmt.Sprintf("%s:%d", h.SSHHost, h.SSHPort)
			fmt.Printf("%-4d %-16s %-24s %-8s %s\n", h.ID, h.Name, addr, h.Status, h.LibvirtURI)
		}

	case "add":
		fs := flag.NewFlagSet("host add", flag.ContinueOnError)
		name := fs.String("name", "", "名称")
		host := fs.String("host", "", "SSH 地址")
		port := fs.Int("port", 22, "SSH 端口")
		user := fs.String("user", "panel", "SSH 用户")
		uri := fs.String("uri", "qemu:///system", "libvirt URI")
		if _, err := parseInterspersed(fs, args[1:]); err != nil {
			return fail(err)
		}
		if *name == "" || *host == "" {
			fmt.Fprintln(os.Stderr, "用法: center host add -name 机房A -host 1.2.3.4 -port 22 [-user panel] [-uri qemu:///system]")
			return 2
		}
		h := &model.Host{Name: *name, SSHHost: *host, SSHPort: *port, SSHUser: *user, LibvirtURI: *uri}
		if err := st.CreateHost(h); err != nil {
			return fail(err)
		}
		fmt.Printf("已登记宿主机 %s（id=%d）\n", h.Name, h.ID)
		fmt.Printf("接下来生成绑定密钥: center key issue -host %d\n", h.ID)

	case "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: center host rm <id>")
			return 2
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "id 要是数字")
			return 2
		}
		h, err := st.HostByID(id)
		if err != nil {
			return fail(fmt.Errorf("找不到宿主机 %d", id))
		}
		if err := st.DeleteHost(id); err != nil {
			return fail(err)
		}
		fmt.Printf("已删除宿主机 %s 及其虚拟机、密钥\n", h.Name)

	default:
		fmt.Fprintln(os.Stderr, "用法: center host add|list|rm")
		return 2
	}
	return 0
}

// ---------- 密钥 ----------

func cliKey(st *store.Store, args []string) int {
	if len(args) == 0 {
		fmt.Print("用法: center key issue|list|revoke\n")
		return 2
	}

	switch args[0] {
	case "issue":
		fs := flag.NewFlagSet("key issue", flag.ContinueOnError)
		hostID := fs.Int64("host", 0, "宿主 id（宿主级密钥）")
		serverID := fs.Int64("server", 0, "虚拟机 id（单机密钥）")
		days := fs.Int("days", 365, "有效天数，0=长期")
		if _, err := parseInterspersed(fs, args[1:]); err != nil {
			return fail(err)
		}

		if *hostID == 0 && *serverID == 0 {
			fmt.Fprintln(os.Stderr, "用法: center key issue -host <id>  或  -server <id>")
			return 2
		}

		hID := *hostID
		label := ""
		if *serverID > 0 {
			sv, err := st.ServerByID(*serverID)
			if err != nil {
				return fail(fmt.Errorf("找不到虚拟机 %d", *serverID))
			}
			hID = sv.HostID
			label = "虚拟机 " + sv.Name
			_ = st.RevokeKeysForServer(*serverID)
		} else {
			h, err := st.HostByID(*hostID)
			if err != nil {
				return fail(fmt.Errorf("找不到宿主机 %d", *hostID))
			}
			label = "宿主机 " + h.Name
			_ = st.RevokeKeysForHost(*hostID)
		}

		plain, err := issueKey(st, hID, *serverID, *days)
		if err != nil {
			return fail(err)
		}
		fmt.Printf("%s 的绑定密钥（%s）:\n\n  %s\n\n", label, expireText(*days), bindkey.Format(plain))

	case "list":
		keys, err := st.AllKeys()
		if err != nil {
			return fail(err)
		}
		if len(keys) == 0 {
			fmt.Println("还没有密钥")
			return 0
		}
		hosts, _ := st.Hosts()
		hostName := map[int64]string{}
		for _, h := range hosts {
			hostName[h.ID] = h.Name
		}
		fmt.Printf("%-4s %-6s %-10s %-8s %-10s %s\n", "ID", "作用域", "对象", "状态", "前缀", "到期")
		for _, k := range keys {
			scope, target := "宿主", hostName[k.HostID]
			if k.ServerID > 0 {
				scope = "虚拟机"
				if sv, err := st.ServerByID(k.ServerID); err == nil {
					target = sv.Name
				} else {
					target = "server#" + strconv.FormatInt(k.ServerID, 10)
				}
			}
			exp := "长期"
			if k.ExpiresAt != nil {
				exp = k.ExpiresAt.Format("2006-01-02")
			}
			fmt.Printf("%-4d %-6s %-10s %-8s %-10s %s\n", k.ID, scope, target, k.Status, k.Prefix+"…", exp)
		}

	case "revoke":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "用法: center key revoke <id>")
			return 2
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "id 要是数字")
			return 2
		}
		if err := st.RevokeKey(id); err != nil {
			return fail(err)
		}
		fmt.Printf("已撤销密钥 #%d\n", id)

	default:
		fmt.Fprintln(os.Stderr, "用法: center key issue|list|revoke")
		return 2
	}
	return 0
}

// ---------- 辅助 ----------

func issueKey(st *store.Store, hostID, serverID int64, days int) (string, error) {
	plain, err := bindkey.New()
	if err != nil {
		return "", err
	}
	var expires *time.Time
	if days > 0 {
		t := time.Now().AddDate(0, 0, days)
		expires = &t
	}
	k := &model.BindKey{
		HostID:    hostID,
		ServerID:  serverID,
		Prefix:    bindkey.Prefix(plain),
		KeyHash:   bindkey.Hash(plain),
		ExpiresAt: expires,
	}
	if err := st.CreateBindKey(k); err != nil {
		return "", err
	}
	return plain, nil
}

func expireText(days int) string {
	if days <= 0 {
		return "长期有效"
	}
	return fmt.Sprintf("%d 天", days)
}

// promptNewPassword 交互式读两次密码，非终端时退回普通读取。
func promptNewPassword() (string, error) {
	p1, err := readPassword("密码: ")
	if err != nil {
		return "", err
	}
	if p1 == "" {
		return "", nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		p2, err := readPassword("再输一次: ")
		if err != nil {
			return "", err
		}
		if p1 != p2 {
			return "", fmt.Errorf("两次密码不一致")
		}
	}
	return p1, nil
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	r := bufio.NewReader(os.Stdin)
	s, err := r.ReadString('\n')
	return strings.TrimSpace(s), err
}

// parseInterspersed 允许选项和位置参数任意顺序，
// 比如 `user add alice -password x` 和 `user add -password x alice` 都行。
// 标准库的 flag 遇到第一个位置参数就停了，所以这里先把选项挑出来。
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var opts, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		opts = append(opts, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if f := fs.Lookup(name); f != nil {
			if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
				if i+1 < len(args) {
					opts = append(opts, args[i+1])
					i++
				}
			}
		}
	}
	if err := fs.Parse(append(opts, positional...)); err != nil {
		return nil, err
	}
	return fs.Args(), nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "错误:", err)
	return 1
}
