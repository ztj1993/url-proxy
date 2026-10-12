package ttl

import (
	"net/http"
	"sync"
	"time"
)

// TTLItem 保存带有生命周期元数据的缓存项 (Feature 318)
type TTLItem struct {
	Data         []byte
	Header       http.Header
	StatusCode   int
	ETag         string
	LastModified string
	CachedAt     time.Time
	ExpiresAt    time.Time
}

// TTLManager 管理内存/临时存储的高并发元数据软缓存生命周期 (Feature 318)
type TTLManager struct {
	mu    sync.RWMutex
	items map[string]*TTLItem
}

// NewManager 初始化元数据软过期管理器
func NewManager() *TTLManager {
	return &TTLManager{
		items: make(map[string]*TTLItem),
	}
}

// Get 查询缓存项，若不存在返回 (nil, false)，若已软过期返回 (item, false)，若保鲜返回 (item, true)
func (m *TTLManager) Get(key string) (*TTLItem, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, found := m.items[key]
	if !found {
		return nil, false
	}
	if time.Now().After(item.ExpiresAt) {
		return item, false
	}
	return item, true
}

// Set 写入带有租期的元数据项并克隆响应头
func (m *TTLManager) Set(key string, data []byte, header http.Header, statusCode int, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	h := make(http.Header)
	for k, vv := range header {
		for _, v := range vv {
			h.Add(k, v)
		}
	}
	m.items[key] = &TTLItem{
		Data:         data,
		Header:       h,
		StatusCode:   statusCode,
		ETag:         header.Get("ETag"),
		LastModified: header.Get("Last-Modified"),
		CachedAt:     now,
		ExpiresAt:    now.Add(duration),
	}
}

// Refresh 在 304 重新验证成功后延长有效期
func (m *TTLManager) Refresh(key string, duration time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, found := m.items[key]; found {
		item.ExpiresAt = time.Now().Add(duration)
		return true
	}
	return false
}
