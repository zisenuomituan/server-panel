// center 是整个面板的服务端：对外提供页面和 API，后台周期性采集各机器指标。
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	cfgPath := flag.String("config", "center.json", "配置文件路径")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
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
		log.Println("提示: 还没有任何账号，请在配置里设置 admin_password，或在页面注册")
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
