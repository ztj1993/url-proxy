package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	addr    = flag.String("addr", ":8888", "http server addr")
	forward = flag.String("forward", "", "forward config file")
	cache   = flag.String("cache", "", "cache directory")
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
func Handler(w http.ResponseWriter, r *http.Request, forwards map[string]string) {
	uri := r.URL.Path[1:]
	log.Printf("req: %s", uri)
	//获取 URI 并且必须大于 6 个字符
	uris, err := url.Parse(uri)
	if err != nil {
		log.Printf("uri err: %s", err)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//判断 URI 是否符合指定的模式
	prefixes := []string{"ftp", "http", "https"}
	if !in(uris.Scheme, prefixes) {
		log.Printf("uri err: %s", uri)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//缓存文件信息
	name := filepath.Join(*cache, strings.Replace(uri, "://", "/", 1))
	stat, _ := os.Stat(name)

	//转发处理
	if _, ok := forwards[uris.Host]; ok {
		uri = forwards[uris.Host] + "/" + uri
	}

	//请求远端文件
	resp, err := http.Get(uri)
	if err != nil {
		log.Printf("get err: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	//响应头
	copyHeader(w.Header(), resp.Header)

	//响应内容
	length, _ := strconv.ParseInt(
		resp.Header.Get("Content-Length"), 10, 64,
	)

	//直接返回文件内容
	if stat != nil && stat.Size() == length {
		file, _ := os.Open(name)
		_, _ = io.Copy(w, file)
		return
	}

	//创建写入目录
	dir := filepath.Dir(name)
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		log.Printf("make dir err: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	//创建临时文件
	tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)
	file, err := os.Create(tmp)
	defer func(file *os.File) {
		_ = file.Close()
		_ = os.Remove(tmp)
	}(file)
	if err != nil {
		log.Printf("file create err: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	//写入文件和响应
	f := bufio.NewWriter(file)
	mw := io.MultiWriter(w, f)
	_, err = io.Copy(mw, resp.Body)
	if err != nil {
		log.Printf("io copy err: %s", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	//刷入文件并重命名文件
	_ = f.Flush()
	_ = file.Close()
	_ = os.Rename(tmp, name)

	//状态码
	w.WriteHeader(resp.StatusCode)
}

//forwardConfig 转发配置
func forwardConfig() map[string]string {
	forwards := make(map[string]string)
	//跳过转发
	if forward == nil || *forward == "" {
		return nil
	} else {
		log.Printf("forward config file: %s", *forward)
	}

	//转发文件不存在
	_, err := os.Stat(*forward)
	if err != nil {
		log.Fatal("forward config file does not exist")
	}

	//打开转发文件
	fd, err := os.Open(*forward)
	if err != nil {
		return nil
	}

	//按行拆分扫描器
	fc := bufio.NewScanner(fd)
	fc.Split(bufio.ScanLines)

	//按行读取
	for fc.Scan() {
		rule := strings.Fields(fc.Text())
		if len(rule) == 2 {
			forwards[rule[0]] = rule[1]
		} else {
			continue
		}
	}

	_ = fd.Close()

	return forwards
}

func cacheConfig() {
	//默认使用临时目录
	if cache == nil || *cache == "" {
		*cache = os.TempDir()
	}

	//打印缓存目录
	log.Printf("cache dir: %s", *cache)

	//缓存目录是否存在
	_, err := os.Stat(*cache)
	if err != nil {
		log.Fatal("cache directory does not exist")
	}
}

//main 程序入口
func main() {
	//解析命令行
	flag.Parse()

	//缓存目录
	cacheConfig()

	//转发处理
	forwards := forwardConfig()

	//HTTP Server
	server := &http.Server{
		Addr: *addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Handler(w, r, forwards)
		}),
	}
	log.Printf("http server: %s", *addr)
	log.Fatal(server.ListenAndServe())
}
