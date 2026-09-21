package main

import (
	"fmt"
	"net/http"
	"time"
)

// 【已废弃 / DEPRECATED】
// 本模拟服务器基于历史旧版本的 forwarding.conf 独立配置文件规范。
// 自 Feature 304 统一配置系统发布后，域名转发功能已整合至 proxy.conf (forward 指令)。
// 本测试套件已废弃并跳过执行。最新实现与测试请参考 tests/304_unified_config。
//
// 这个服务器用于模拟前置镜像网关，用于验证 url-proxy 根据 forwarding.conf 进行特定域名规则重定向转发的能力（历史保留）。
func main() {
	http.HandleFunc("/mirror/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[%s] [镜像网关] 成功接收到被转发重写的请求: %s\n", time.Now().Format(time.RFC3339), r.URL.RequestURI())

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Forwarded-Mock", "true")
		w.WriteHeader(http.StatusOK)

		content := fmt.Sprintf("url-proxy host forwarding test: PASSED (target: %s)\n", r.URL.Path)
		_, _ = w.Write([]byte(content))
	})

	port := ":9993"
	fmt.Printf("测试用镜像转发服务器已启动，监听端口 %s\n", port)
	fmt.Printf("镜像接收前缀: http://localhost%s/mirror/\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("服务器启动失败: %v\n", err)
	}
}
