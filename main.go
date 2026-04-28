package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := loadConfig()

	db := initDB(cfg)
	redis := initRedis(cfg)
	email := NewEmailSender(cfg.ResendKey, cfg.ResendFrom)
	svc := NewInviteService(db, redis, email, cfg)
	svc.StartPendingReleaseWorker()

	handler := NewInviteHandler(svc, redis, cfg)

	r := gin.Default()
	r.ForwardedByClientIP = true
	r.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	handler.Register(r)
	r.StaticFile("/", "./static/index.html")
	r.StaticFile("/admin", "./static/admin.html")
	r.StaticFile("/altcha.min.js", "./static/altcha.min.js")
	r.StaticFile("/favicon.svg", "./static/favicon.svg")

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("服务启动，监听端口 %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("正在关闭服务...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("服务关闭异常: %v", err)
	}
	// 关闭 Redis 和 DB 连接
	_ = redis.Close()
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
	log.Println("服务已退出")
}
