package downloader

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestManager_GetOrCreate(t *testing.T) {
	mgr := NewManager(os.TempDir(), time.Minute)

	uri := "http://example.com/file.tar.gz"
	job1, isLeader1 := mgr.GetOrCreate(uri)
	if !isLeader1 || job1 == nil {
		t.Fatalf("expected first caller to be Leader")
	}

	job2, isLeader2 := mgr.GetOrCreate(uri)
	if isLeader2 || job2 != job1 {
		t.Fatalf("expected second caller to be Follower sharing same job")
	}

	mgr.Remove(uri)
	job3, isLeader3 := mgr.GetOrCreate(uri)
	if !isLeader3 || job3 == job1 {
		t.Fatalf("expected new Leader after removal")
	}
}

func TestDownloadJob_TryRename(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dl-job-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	tmpName := filepath.Join(tmpDir, "test.tmp")
	finalName := filepath.Join(tmpDir, "test.final")
	_ = os.WriteFile(tmpName, []byte("data"), 0644)

	job := &DownloadJob{
		StatusCode: http.StatusOK,
		TmpName:    tmpName,
		FinalName:  finalName,
		Readers:    1, // 模拟有 Follower 正在读取
	}

	// Readers > 0: 不应重命名
	job.TryRename(1)
	if _, err := os.Stat(finalName); err == nil {
		t.Errorf("file should not be renamed while readers > 0")
	}

	// Readers == 0: 应该成功重命名
	atomic.StoreInt32(&job.Readers, 0)
	job.TryRename(2)
	if _, err := os.Stat(finalName); err != nil {
		t.Errorf("file should be renamed successfully: %v", err)
	}

	// 错误情况下删除临时文件
	tmpErrName := filepath.Join(tmpDir, "err.tmp")
	finalErrName := filepath.Join(tmpDir, "err.final")
	_ = os.WriteFile(tmpErrName, []byte("partial"), 0644)

	errJob := &DownloadJob{
		StatusCode: http.StatusOK,
		TmpName:    tmpErrName,
		FinalName:  finalErrName,
		Err:        errors.New("download failed"),
		Readers:    0,
	}
	errJob.TryRename(3)
	if _, err := os.Stat(tmpErrName); !os.IsNotExist(err) {
		t.Errorf("tmp file should be removed on error")
	}

	// Feature 331 校验：非 200 响应严禁重命名，强制清理临时文件
	tmp404Name := filepath.Join(tmpDir, "404.tmp")
	final404Name := filepath.Join(tmpDir, "404.final")
	_ = os.WriteFile(tmp404Name, []byte("404 error html"), 0644)

	non200Job := &DownloadJob{
		StatusCode: http.StatusNotFound,
		TmpName:    tmp404Name,
		FinalName:  final404Name,
		Readers:    0,
	}
	non200Job.TryRename(4)
	if _, err := os.Stat(final404Name); err == nil {
		t.Errorf("non-200 job should never create final file")
	}
	if _, err := os.Stat(tmp404Name); !os.IsNotExist(err) {
		t.Errorf("non-200 job should clean up tmp file")
	}
}

func TestManager_WatchdogLifecycle(t *testing.T) {
	mgr := NewManager(os.TempDir(), 100*time.Millisecond)

	cancelled := false
	uri := "http://example.com/hang"
	job, _ := mgr.GetOrCreate(uri)
	job.LastActive = time.Now().Add(-200 * time.Millisecond) // 已超时
	_, cancel := context.WithCancel(context.Background())
	job.Cancel = func() {
		cancelled = true
		cancel()
	}

	mgr.StartWatchdog()
	time.Sleep(150 * time.Millisecond)
	mgr.StopWatchdog()

	if !cancelled {
		t.Errorf("expected watchdog to cancel stalled download job")
	}
}