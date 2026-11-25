// Package qrsession 提供扫码登录的一次性会话状态机。
// handler 与 serverplugin 共用同一份进程内状态：插件启动的二维码可由本站
// 手机确认页完成确认，再由桌面轮询取走登录态。
package qrsession

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// TTL 二维码有效期（与既有扫码登录保持一致）。
const TTL = 5 * time.Minute

// 状态常量：0=待确认 1=已确认 2=已消费/失效。
const (
	Pending = iota
	Confirmed
	Expired
)

type entry struct {
	status  int
	userID  uint
	expires time.Time
}

// Store 进程内一次性扫码会话表；单实例部署，重启后自然作废。
type Store struct {
	mu sync.Mutex
	m  map[string]*entry
}

// NewStore 构造空会话表。
func NewStore() *Store { return &Store{m: map[string]*entry{}} }

var defaultStore = NewStore()

// Default 返回 handler 与 serverplugin 共用的全局会话表。
func Default() *Store { return defaultStore }

// Issue 生成 256 位随机票据与过期时间，并顺手清理过期/已消费会话。
func (s *Store) Issue() (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(TTL)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, v := range s.m {
		if now.After(v.expires) || v.status == Expired {
			delete(s.m, k)
		}
	}
	s.m[token] = &entry{expires: expires}
	return token, expires, nil
}

// Confirm 手机端确认：待确认 → 已确认并绑定用户；其余返回 false。
func (s *Store) Confirm(token string, userID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[token]
	if !ok || e.status != Pending || time.Now().After(e.expires) {
		return false
	}
	e.status = Confirmed
	e.userID = userID
	return true
}

// Take 桌面轮询取走登录态（一次性）：返回状态与用户 ID。
func (s *Store) Take(token string) (state int, userID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[token]
	if !ok {
		return Expired, 0
	}
	if time.Now().After(e.expires) {
		delete(s.m, token)
		return Expired, 0
	}
	if e.status == Pending {
		return Pending, 0
	}
	if e.status == Confirmed {
		e.status = Expired
		return Confirmed, e.userID
	}
	return Expired, 0
}

// Invalidate 立即作废票据。
func (s *Store) Invalidate(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, token)
}

// ExpireForTest 将票据改为已过期；仅测试使用。
func (s *Store) ExpireForTest(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.m[token]; ok {
		e.expires = time.Now().Add(-time.Minute)
	}
}
