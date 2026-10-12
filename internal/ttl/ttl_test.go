package ttl

import (
	"net/http"
	"testing"
	"time"
)

func TestTTLManager(t *testing.T) {
	mgr := NewManager()

	header := make(http.Header)
	header.Set("ETag", "v1.0")

	// Set with 50ms TTL
	mgr.Set("key1", []byte("data1"), header, 200, 50*time.Millisecond)

	// Immediate get: fresh
	item, fresh := mgr.Get("key1")
	if !fresh || item == nil || string(item.Data) != "data1" {
		t.Errorf("expected fresh item, got fresh=%v, item=%v", fresh, item)
	}

	// Sleep until expired
	time.Sleep(60 * time.Millisecond)
	item, fresh = mgr.Get("key1")
	if fresh || item == nil {
		t.Errorf("expected stale item, got fresh=%v, item=%v", fresh, item)
	}

	// Refresh lease
	if !mgr.Refresh("key1", time.Second) {
		t.Errorf("expected refresh to succeed")
	}

	item, fresh = mgr.Get("key1")
	if !fresh || item == nil {
		t.Errorf("expected fresh after refresh, got fresh=%v", fresh)
	}
}
