// center 是面板的服务端，同时也是管理用的命令行工具。
//
//	center [serve] [-config center.json]   启动 Web 面板
//	center user / host / key ...           在终端里管理账号、宿主、密钥
//	center help                            查看全部命令
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"serverpanel/internal/auth"
	"serverpanel/internal/config"
	"serverpanel/internal/model"
	"serverpanel/internal/server"
	"serverpanel/internal/store"
	"serverpanel/web"
)

func main() {
	configPath, args := splitConfig(os.Args[1:])

	if len(args) > 0 {
		switch args[0] {
		case "serve":
			runServe(configPath)
			return
		case "user", "host", "key", "backup", "restore", "version", "help", "-h", "--help":
			os.Exit(runCLI(configPath, args))
		}
	}
	// 没有子命令（或只有 -config）时，默认启动面板
	runServe(configPath)
}

// splitConfig 从参数里抽出 -config，返回配置路径和其余参数。
func splitConfig(args []string) (string, []string) {
	path := "center.json"
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 < len(args) {
				path = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-config="):
			path = strings.TrimPrefix(a, "-config=")
		case strings.HasPrefix(a, "--config="):
			path = strings.TrimPrefix(a, "--config=")
		default:
			rest = append(rest, a)
		}
	}
	return path, rest
}

func runServe(configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatal(err)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatal("打开数据库失败: ", err)
	}
	defer st.Close()

	bootstrapAdmin(st, cfg)

	srv := server.New(cfg, st)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go srv.RunPoller(ctx)

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Routes(http.FS(web.FS)),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("面板已启动，监听 %s（后端: %s）", cfg.Listen, cfg.Backend)
		log.Printf("命令行管理：center user add / host add / key issue ...")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Println("正在退出...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// bootstrapAdmin 在没有配置任何账号时，用配置里的用户名口令建一个管理员。
func bootstrapAdmin(st *store.Store, cfg *config.Config) {
	if n, _ := st.CountUsers(); n > 0 {
		return
	}
	if cfg.AdminPassword == "" {
		log.Println("提示: 还没有任何账号，可以在配置里设置 admin_password，")
		log.Println("      或用命令行创建: center user add admin -role admin")
		return
	}
	hash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatal(err)
	}
	u := &model.User{
		Username:     cfg.AdminUser,
		PasswordHash: hash,
		Role:         model.RoleAdmin,
	}
	if err := st.CreateUser(u); err != nil {
		log.Fatal("创建管理员失败: ", err)
	}
	log.Printf("已创建管理员账号: %s", cfg.AdminUser)
	_ = os.Stdout.Sync()
}
