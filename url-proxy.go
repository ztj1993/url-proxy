package main

import (
	"flag"
	"io"
	"log"
	"net/http"
)

var (
	addr = flag.String("addr", ":8888", "http server addr")
)

//in 判断字符串是否存在于数组中
func in(target string, array []string) bool {
	for _, element := range array {
		if target == element {
			return true
		}
	}
	return false
}

//copyHeader 复制响应头
func copyHeader(dst http.Header, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

//Handler 请求处理程序
func Handler(w http.ResponseWriter, r *http.Request) {
	//获取 URI 并且必须大于 6 个字符
	uri := r.URL.Path[1:]
	if len(uri) < 6 {
		log.Printf("uri err - %s", uri)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//判断 URI 是否符合指定的模式
	prefixes := []string{"ftp://", "http:/", "https:"}
	if !in(uri[0:6], prefixes) {
		log.Printf("uri err - %s", uri)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	log.Printf("req - %s", uri)

	//请求远端文件
	resp, err := http.Get(uri)
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	if err != nil {
		log.Printf("get err - %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	//响应文件
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

//main 程序入口
func main() {
	//解析命令行
	flag.Parse()

	//HTTP Server
	server := &http.Server{
		Addr:    *addr,
		Handler: http.HandlerFunc(Handler),
	}
	log.Printf("http server - %s", *addr)
	log.Fatal(server.ListenAndServe())
}
