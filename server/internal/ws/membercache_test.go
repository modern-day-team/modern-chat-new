package ws

import (
	"testing"
	"time"
)

// 过期条目必须被清扫，否则缓存随会话数单调增长（长运行站点内存泄漏）。
func TestEvictExpiredMembers(t *testing.T) {
	h := NewHub(nil)
	h.membersCache.Store(uint(1), &membersEntry{ids: []uint{10}, at: time.Now()})
	h.membersCache.Store(uint(2), &membersEntry{ids: []uint{20}, at: time.Now().Add(-membersCacheTTL - time.Second)})

	h.evictExpiredMembers()

	if _, ok := h.membersCache.Load(uint(1)); !ok {
		t.Fatal("fresh entry should be kept")
	}
	if _, ok := h.membersCache.Load(uint(2)); ok {
		t.Fatal("expired entry should be evicted")
	}
}
