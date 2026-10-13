package downloader

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// DownloadJob 下载任务状态管理
type DownloadJob struct {
	Uri        string
	FilePath   string
	Header     http.Header
	StatusCode int
	Body       io.ReadCloser      // 用于非 200 响应透传
	HeaderDone chan struct{}      // Header 准备好信号
	Done       chan struct{}      // 下载完成信号
	StreamDone chan struct{}      // 非 200 流式透传完成信号
	Err        error
	Cond       *sync.Cond         // 用于广播新数据写入
	Mu         sync.Mutex         // 配合 Cond 使用
	Readers    int32              // 当前正在读取 Followers 的数量
	RenameMu   sync.Mutex         // 用于保护 Rename 操作
	FinalName  string             // 最终缓存文件名
	TmpName    string             // 临时文件名
	LastActive time.Time          // 最后一次成功读取数据的时间戳
	Cancel     context.CancelFunc // 用于取消底层 HTTP 请求
}

// TryRename 尝试重命名文件
func (job *DownloadJob) TryRename(reqID uint64) {
	job.RenameMu.Lock()
	defer job.RenameMu.Unlock()

	// Feature 331 双重断言校验：非 200 响应严禁重命名，强制清理临时文件
	if job.StatusCode != http.StatusOK {
		log.Printf("[%d] rename skipped: status code is non-200 (%d), removing tmp file: %s", reqID, job.StatusCode, job.TmpName)
		if job.TmpName != "" {
			_ = os.Remove(job.TmpName)
		}
		return
	}

	if job.TmpName == "" {
		return
	}
	if _, err := os.Stat(job.TmpName); os.IsNotExist(err) {
		log.Printf("[%d] rename skipped: tmp file does not exist %s", reqID, job.TmpName)
		return
	}

	// 只有当没有 Readers 时才尝试重命名 (主要针对 Windows)
	readers := atomic.LoadInt32(&job.Readers)
	if readers == 0 {
		// 如果任务发生错误，则删除临时文件，不进行重命名
		if job.Err != nil {
			log.Printf("[%d] job has error (%v), removing tmp file: %s", reqID, job.Err, job.TmpName)
			_ = os.Remove(job.TmpName)
			return
		}

		log.Printf("[%d] attempting rename: %s -> %s", reqID, job.TmpName, job.FinalName)
		_ = os.Remove(job.FinalName)
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

// Manager 任务池管理器
type Manager struct {
	cacheDir string
	idleTime time.Duration
	jobs     map[string]*DownloadJob
	jobsMu   sync.Mutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewManager 初始化面向对象的任务池管理器
func NewManager(cacheDir string, idleTime time.Duration) *Manager {
	if idleTime <= 0 {
		idleTime = 10 * time.Minute
	}
	return &Manager{
		cacheDir: cacheDir,
		idleTime: idleTime,
		jobs:     make(map[string]*DownloadJob),
		stopCh:   make(chan struct{}),
	}
}

// GetOrCreate 获取既有下载任务或注册新 Leader (Request Coalescing)
func (m *Manager) GetOrCreate(uri string) (*DownloadJob, bool) {
	m.jobsMu.Lock()
	defer m.jobsMu.Unlock()

	if job, ok := m.jobs[uri]; ok {
		return job, false
	}

	job := &DownloadJob{
		Uri:        uri,
		HeaderDone: make(chan struct{}),
		Done:       make(chan struct{}),
		StreamDone: make(chan struct{}),
		Header:     make(http.Header),
		LastActive: time.Now(),
	}
	job.Cond = sync.NewCond(&job.Mu)
	m.jobs[uri] = job
	return job, true
}

// Remove 移除下载任务
func (m *Manager) Remove(uri string) {
	m.jobsMu.Lock()
	delete(m.jobs, uri)
	m.jobsMu.Unlock()
}

// StartWatchdog 启动后台巡检，清理超时挂起的任务
func (m *Manager) StartWatchdog() {
	checkTime := m.idleTime / 3
	log.Printf("watchdog started: idle-time=%v, internal-check-time=%v", m.idleTime, checkTime)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(checkTime)
		defer ticker.Stop()

		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.jobsMu.Lock()
				now := time.Now()
				for uri, job := range m.jobs {
					job.Mu.Lock()
					if now.Sub(job.LastActive) > m.idleTime {
						if job.Cancel != nil {
							log.Printf("watchdog: killing idle job: %s, inactive for %v", uri, now.Sub(job.LastActive))
							job.Cancel()
						}
					}
					job.Mu.Unlock()
				}
				m.jobsMu.Unlock()
			}
		}
	}()
}

// StopWatchdog 优雅关闭后台巡检
func (m *Manager) StopWatchdog() {
	close(m.stopCh)
	m.wg.Wait()
}