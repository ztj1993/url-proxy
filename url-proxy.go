package main

import (
	"bufio"
	"flag"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	addr      = flag.String("addr", ":8888", "http server addr")
	forward   = flag.String("forward", "", "forward config file")
	cache     = flag.String("cache", "", "cache directory")
	jobs      = make(map[string]*DownloadJob)
	jobsMu    sync.Mutex
	requestID uint64
)

//DownloadJob 下载任务状态管理
type DownloadJob struct {
	Uri        string
	FilePath   string
	Header     http.Header
	StatusCode int
	HeaderDone chan struct{} // Header 准备好信号
	Done       chan struct{} // 下载完成信号
	Err        error
	Cond       *sync.Cond // 用于广播新数据写入
	Mu         sync.Mutex // 配合 Cond 使用
	Readers    int32      // 当前正在读取 Followers 的数量
	RenameMu   sync.Mutex // 用于保护 Rename 操作
	FinalName  string     // 最终缓存文件名
	TmpName    string     // 临时文件名
}

//getDownloadJob 获取或创建下载任务
//返回 job 和 isLeader (是否为下载负责人)
func getDownloadJob(uri string) (*DownloadJob, bool) {
	jobsMu.Lock()
	defer jobsMu.Unlock()

	if job, ok := jobs[uri]; ok {
		return job, false
	}

	job := &DownloadJob{
		Uri:        uri,
		HeaderDone: make(chan struct{}),
		Done:       make(chan struct{}),
		Header:     make(http.Header),
	}
	job.Cond = sync.NewCond(&job.Mu)
	jobs[uri] = job
	return job, true
}

//removeJob 移除下载任务
func removeJob(uri string) {
	jobsMu.Lock()
	delete(jobs, uri)
	jobsMu.Unlock()
}

//tryRename 尝试重命名文件
func (job *DownloadJob) tryRename(reqID uint64) {
	job.RenameMu.Lock()
	defer job.RenameMu.Unlock()

	// 如果已经重命名成功，或者临时文件不存在，则直接返回
	if _, err := os.Stat(job.FinalName); err == nil {
		log.Printf("[%d] rename skipped: final file already exists %s", reqID, job.FinalName)
		return
	}
	if _, err := os.Stat(job.TmpName); os.IsNotExist(err) {
		log.Printf("[%d] rename skipped: tmp file does not exist %s", reqID, job.TmpName)
		return
	}

	// 只有当没有 Readers 时才尝试重命名 (主要针对 Windows)
	readers := atomic.LoadInt32(&job.Readers)
	if readers == 0 {
		log.Printf("[%d] attempting rename: %s -> %s", reqID, job.TmpName, job.FinalName)
		err := os.Rename(job.TmpName, job.FinalName)
		if err == nil {
			job.FilePath = job.FinalName
			log.Printf("[%d] job renamed successfully: %s", reqID, job.FinalName)
		} else {
			log.Printf("[%d] job rename failed: %v", reqID, err)
		}
	} else {
		log.Printf("[%d] rename skipped: file is still being read by %d followers", reqID, readers)
	}
}

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

//getSafeUrlPath 根据 URL 生成安全的本地缓存相对路径
//规则: 协议-域名-端口/请求路径/url编码后的(文件名?参数)
func getSafeUrlPath(rawUri string) string {
	u, err := url.Parse(rawUri)
	if err != nil {
		// 解析失败回退为完全编码
		return url.QueryEscape(rawUri)
	}

	// 1. 协议-域名-端口 (替换 : 为 - 防止 Windows 路径错误)
	hostStr := u.Host
	hostStr = strings.ReplaceAll(hostStr, ":", "-")
	
	schemeHostDir := u.Scheme + "-" + hostStr
	if u.Scheme == "" {
		schemeHostDir = hostStr
	}

	// 2. 请求路径
	pDir := path.Dir(u.Path)
	pBase := path.Base(u.Path)

	// 如果是以 / 结尾，说明全是目录，文件名为 index
	if strings.HasSuffix(u.Path, "/") {
		pDir = u.Path
		pBase = "index"
	}

	// 根目录处理
	if pDir == "/" || pDir == "." {
		pDir = ""
	}
	if pBase == "/" || pBase == "." {
		pBase = "index"
	}

	// 3. 文件和参数拼接后编码当文件
	fileAndQuery := pBase
	if u.RawQuery != "" {
		fileAndQuery += "?" + u.RawQuery
	}

	encodedFileName := url.QueryEscape(fileAndQuery)

	return filepath.Join(schemeHostDir, filepath.FromSlash(pDir), encodedFileName)
}

//Handler 请求处理程序
func Handler(w http.ResponseWriter, r *http.Request, forwards map[string]string) {
	reqID := atomic.AddUint64(&requestID, 1)
	uri := r.URL.Path[1:]
	log.Printf("[%d] req: %s", reqID, uri)
	//获取 URI 并且必须大于 6 个字符
	uris, err := url.Parse(uri)
	if err != nil {
		log.Printf("[%d] uri err: %s", reqID, err)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//判断 URI 是否符合指定的模式
	prefixes := []string{"ftp", "http", "https"}
	if !in(uris.Scheme, prefixes) {
		log.Printf("[%d] uri err: %s", reqID, uri)
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}

	//获取任务 (Request Coalescing)
	job, isLeader := getDownloadJob(uri)

	// Leader 负责下载数据
	if isLeader {
		log.Printf("[%d] job leader: %s", reqID, uri)
		go func() {
			defer removeJob(uri)
			// 确保结束时唤醒所有等待者
			defer func() {
				job.Cond.L.Lock()
				job.Cond.Broadcast()
				job.Cond.L.Unlock()
			}()
			defer close(job.Done)

			//缓存文件路径
			name := filepath.Join(*cache, getSafeUrlPath(uri))
			stat, _ := os.Stat(name)

			//转发处理
			targetUri := uri
			if _, ok := forwards[uris.Host]; ok {
				targetUri = forwards[uris.Host] + "/" + uri
			}

			log.Printf("[%d] job download: %s", reqID, targetUri)
			//请求远端文件
			resp, err := http.Get(targetUri)
			if err != nil {
				log.Printf("[%d] get err: %s", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(resp.Body)

			//设置 Job 信息
			job.StatusCode = resp.StatusCode
			copyHeader(job.Header, resp.Header)

			length, _ := strconv.ParseInt(
				resp.Header.Get("Content-Length"), 10, 64,
			)

			//如果缓存已存在且完整，直接使用
			if stat != nil && stat.Size() == length {
				log.Printf("[%d] job cached: %s", reqID, uri)
				job.FilePath = name
				close(job.HeaderDone)
				return
			}

			//准备写入
			dir := filepath.Dir(name)
			_ = os.MkdirAll(dir, 0755)

			tmp := name + "." + strconv.FormatInt(time.Now().UnixNano(), 10)

			// 记录文件名，供 tryRename 使用
			job.FinalName = name
			job.TmpName = tmp

			file, err := os.Create(tmp)
			if err != nil {
				log.Printf("[%d] file create err: %s", reqID, err)
				job.Err = err
				close(job.HeaderDone)
				return
			}

			job.FilePath = tmp
			close(job.HeaderDone) // 通知 Followers 可以开始读了

			// 写入循环
			buf := make([]byte, 32*1024)
			for {
				n, err := resp.Body.Read(buf)
				if n > 0 {
					_, wErr := file.Write(buf[:n])
					if wErr != nil {
						log.Printf("[%d] file write err: %s", reqID, wErr)
						break
					}
					// 广播：有新数据了
					job.Cond.L.Lock()
					job.Cond.Broadcast()
					job.Cond.L.Unlock()
				}
				if err != nil {
					break
				}
			}
			log.Printf("[%d] job finished: %s", reqID, uri)

			_ = file.Close()
			
			// Leader 下载完成后，尝试重命名
			job.tryRename(reqID)
		}()
	} else {
		log.Printf("[%d] job follower: %s", reqID, uri)
	}

	// 等待 Header 可用
	<-job.HeaderDone

	if job.Err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// 发送响应头
	copyHeader(w.Header(), job.Header)
	w.WriteHeader(job.StatusCode)

	// 增加 Reader 计数
	atomic.AddInt32(&job.Readers, 1)

	// 打开文件进行读取
	// 注意：如果是 Follower，这里打开的可能是正在写入的 tmp 文件
	file, err := os.Open(job.FilePath)
	if err != nil {
		log.Printf("[%d] file open err: %s", reqID, err)
		atomic.AddInt32(&job.Readers, -1) // 失败时减少计数
		return
	}
	defer func() {
		_ = file.Close()
		// Reader 退出时减少计数
		if atomic.AddInt32(&job.Readers, -1) == 0 {
			// 如果我是最后一个 Reader，且 Leader 已经完成下载，尝试重命名
			select {
			case <-job.Done:
				job.tryRename(reqID)
			default:
			}
		}
	}()

	// 流式发送数据
	buf := make([]byte, 32*1024)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, wErr := w.Write(buf[:n]); wErr != nil {
				log.Printf("[%d] client write err (cancelled)", reqID)
				return
			}
		}
		if err == io.EOF {
			// 读到末尾了，检查下载是否完成
			select {
			case <-job.Done:
				// 下载已完成，且读到了 EOF，说明真的结束了
				log.Printf("[%d] job done: %s", reqID, uri)
				return
			default:
				// 下载未完成，等待新数据
				job.Cond.L.Lock()
				job.Cond.Wait()
				job.Cond.L.Unlock()
			}
		} else if err != nil {
			log.Printf("[%d] read err: %s", reqID, err)
			return
		}
	}
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
