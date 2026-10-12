package plugin

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"uri-proxy/internal/cache"
)

func TestDockerPlugin_MatchAndFastPath(t *testing.T) {
	p := &DockerPlugin{}
	if p.Name() != "docker" {
		t.Errorf("expected docker, got %s", p.Name())
	}

	req := httptest.NewRequest("GET", "http://localhost:8888/v2/library/ubuntu/blobs/sha256:123456", nil)
	uri := "https://registry-1.docker.io/v2/library/ubuntu/blobs/sha256:123456"

	if !p.Match(req, uri) {
		t.Errorf("expected docker blobs match")
	}

	tmpDir, err := os.MkdirTemp("", "docker-plugin-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	w := httptest.NewRecorder()
	// Cache miss: returns false
	if p.FastPathServe(w, req, uri, tmpDir) {
		t.Errorf("expected cache miss to return false")
	}

	// Create fake cached blob
	safePath := filepath.Join(tmpDir, cache.GetSafeUrlPath(uri))
	_ = os.MkdirAll(filepath.Dir(safePath), 0755)
	_ = os.WriteFile(safePath, []byte("blob-data"), 0644)

	w2 := httptest.NewRecorder()
	if !p.FastPathServe(w2, req, uri, tmpDir) {
		t.Errorf("expected cache hit to return true")
	}
	if w2.Header().Get("Content-Length") != "9" {
		t.Errorf("expected Content-Length 9, got %s", w2.Header().Get("Content-Length"))
	}
}

func TestRegistry(t *testing.T) {
	reg := DefaultRegistry()
	p, ok := reg.Get("docker")
	if !ok || p == nil {
		t.Fatalf("expected docker plugin in default registry")
	}
}
