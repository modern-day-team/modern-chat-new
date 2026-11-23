package ws

import (
	"testing"
	"time"
)

// relatedUsers 缓存命中时不能重复查库；失效后必须重算。
// 这里直接断言缓存条目本身（computeRelatedUsers 查库需完整 deps，单测不构造）。
func TestRelatedCacheInvalidation(t *testing.T) {
	h := NewHub(nil)
	h.relatedCache.Store(uint(7), &relatedEntry{ids: []uint{1, 2}, at: time.Now()})

	h.InvalidateRelated(7, 8)
	if _, ok := h.relatedCache.Load(uint(7)); ok {
		t.Fatal("user entry should be invalidated")
	}

	h.relatedCache.Store(uint(7), &relatedEntry{ids: []uint{1, 2}, at: time.Now()})
	h.relatedCache.Store(uint(8), &relatedEntry{ids: []uint{3}, at: time.Now()})
	h.InvalidateRelated(7, 8)
	if _, ok := h.relatedCache.Load(uint(8)); ok {
		t.Fatal("friend entry should be invalidated")
	}
}

// InvalidateMembers 应同时失效这些成员的相关受众缓存。
func TestInvalidateMembersAlsoInvalidatesRelated(t *testing.T) {
	h := NewHub(nil)
	h.membersCache.Store(uint(99), &membersEntry{ids: []uint{1, 2}, at: time.Now()})
	h.relatedCache.Store(uint(1), &relatedEntry{ids: []uint{1}, at: time.Now()})
	h.relatedCache.Store(uint(2), &relatedEntry{ids: []uint{2}, at: time.Now()})

	h.InvalidateMembers(99)

	if _, ok := h.membersCache.Load(uint(99)); ok {
		t.Fatal("member cache should be invalidated")
	}
	for _, uid := range []uint{1, 2} {
		if _, ok := h.relatedCache.Load(uid); ok {
			t.Fatalf("related cache for %d should be invalidated", uid)
		}
	}
}

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
