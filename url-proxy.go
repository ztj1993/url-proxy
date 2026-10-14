package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
)

var (
	addr     = flag.String("addr", ":8888", "http server addr")
	cacheDir = flag.String("cache", "", "cache directory")
	conf     = flag.String("conf", "proxy.conf", "path to unified proxy configuration file")
	idleTime = flag.Duration("idle-time", 10*time.Minute, "idle timeout for hanging downloads (default 10m)")
)

func main() {
	flag.Parse()

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. 初始化并校验本地缓存目录
	resolvedCacheDir, err := cache.EnsureCacheDir(*cacheDir)
	if err != nil {
		log.Fatalf("failed to initialize cache directory %s: %v", *cacheDir, err)
	}
	log.Printf("cache dir: %s", resolvedCacheDir)

	// 异步清理启动残留孤儿临时文件 (Feature 336)
	go func() {
		if removed, err := cache.CleanOrphanTempFiles(resolvedCacheDir, 1*time.Hour); err != nil {
			log.Printf("orphan temp file cleanup error: %v", err)
		} else if removed > 0 {
			log.Printf("orphan temp file cleanup completed: removed %d stale files", removed)
		}
	}()

	// 2. 加载统一代理配置
	cfg := config.Load(*conf)

	// 3. 初始化并启动并发下载任务池与后台空闲看门狗巡检
	downMgr := downloader.NewManager(resolvedCacheDir, *idleTime)
	downMgr.StartWatchdog()
	defer downMgr.StopWatchdog()

	// 4. 初始化动态重写引擎与协议插件注册中心
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	// 5. 装配代理协议服务核心
	proxyServer := proxy.NewServer(cfg, resolvedCacheDir, downMgr, rw, plugins, *addr)

	server := &http.Server{
		Addr:              *addr,
		Handler:           proxyServer,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Printf("http server: %s", *addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		log.Fatalf("http server startup error: %v", err)
	case <-rootCtx.Done():
		log.Println("received shutdown signal, starting graceful shutdown...")
	}

	// 6. 执行优雅停机与资源回收流程 (Feature 339)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown warning: %v", err)
	}

	downMgr.Shutdown()
	proxyServer.Close()
	log.Println("graceful shutdown completed")
}