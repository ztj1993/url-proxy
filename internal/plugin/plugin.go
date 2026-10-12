package plugin

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"uri-proxy/internal/cache"
)

// RoutePlugin 路由作用域协议插件接口 (Feature 317)
type RoutePlugin interface {
	Name() string
	Match(req *http.Request, uri string) bool
	FastPathServe(w http.ResponseWriter, r *http.Request, uri string, cacheDir string) bool
}

// DockerPlugin Docker / OCI Registry v2 协议优化插件 (Feature 317)
type DockerPlugin struct{}

func (p *DockerPlugin) Name() string {
	return "docker"
}

func (p *DockerPlugin) Match(req *http.Request, uri string) bool {
	return strings.Contains(uri, "/blobs/sha256:")
}

func (p *DockerPlugin) FastPathServe(w http.ResponseWriter, r *http.Request, uri string, cacheDir string) bool {
	if !p.Match(r, uri) {
		return false
	}
	cachePath := filepath.Join(cacheDir, cache.GetSafeUrlPath(uri))
	stat, err := os.Stat(cachePath)
	if err != nil || stat.IsDir() || stat.Size() == 0 {
		return false
	}
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "application/octet-stream")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return true
	}
	http.ServeFile(w, r, cachePath)
	return true
}

// Registry 插件注册中心与分发器
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]RoutePlugin
}

// NewRegistry 创建空插件注册表
func NewRegistry() *Registry {
	return &Registry{
		plugins: make(map[string]RoutePlugin),
	}
}

// Register 注册协议插件
func (r *Registry) Register(p RoutePlugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

// Get 查询指定名称的插件
func (r *Registry) Get(name string) (RoutePlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// DefaultRegistry 获取预注册标准官方插件的注册表
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Register(&DockerPlugin{})
	return reg
}
