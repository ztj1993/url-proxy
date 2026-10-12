package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"uri-proxy/internal/cache"
	"uri-proxy/internal/config"
	"uri-proxy/internal/downloader"
	"uri-proxy/internal/plugin"
	"uri-proxy/internal/proxy"
	"uri-proxy/internal/rewriter"
	"uri-proxy/internal/ttl"
)

var (
	addr     = flag.String("addr", ":8888", "http server addr")
	cacheDir = flag.String("cache", "", "cache directory")
	conf     = flag.String("conf", "proxy.conf", "path to unified proxy configuration file")
	idleTime = flag.Duration("idle-time", 10*time.Minute, "idle timeout for hanging downloads (default 10m)")
)

func main() {
	flag.Parse()

	// 1. 初始化并校验本地缓存目录
	resolvedCacheDir, err := cache.EnsureCacheDir(*cacheDir)
	if err != nil {
		log.Fatalf("failed to initialize cache directory %s: %v", *cacheDir, err)
	}
	log.Printf("cache dir: %s", resolvedCacheDir)

	// 2. 加载统一代理配置
	cfg := config.Load(*conf)

	// 3. 初始化并启动并发下载任务池与后台空闲看门狗巡检
	downMgr := downloader.NewManager(resolvedCacheDir, *idleTime)
	downMgr.StartWatchdog()
	defer downMgr.StopWatchdog()

	// 4. 初始化元数据软过期 SWR 存储、动态重写引擎与协议插件注册中心
	ttlMgr := ttl.NewManager()
	rw := rewriter.NewBodyRewriter(cfg.RewriteRules, cfg.Types)
	plugins := plugin.DefaultRegistry()

	// 5. 装配代理协议服务核心
	proxyServer := proxy.NewServer(cfg, resolvedCacheDir, downMgr, ttlMgr, rw, plugins, *addr)

	server := &http.Server{
		Addr:    *addr,
		Handler: proxyServer,
	}
	log.Printf("http server: %s", *addr)
	log.Fatal(server.ListenAndServe())
}
