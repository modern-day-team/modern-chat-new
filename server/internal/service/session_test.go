package service

import (
	"errors"
	"testing"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

type stubSessionRepo struct {
	rows     []model.UserSession
	sid      string
	revokeOK bool
	err      error
	gotUID   uint
	gotID    uint
}

func (s *stubSessionRepo) List(uid uint) ([]model.UserSession, error) {
	s.gotUID = uid
	return s.rows, s.err
}
func (s *stubSessionRepo) SIDByID(uid, id uint) (string, error) {
	s.gotUID, s.gotID = uid, id
	return s.sid, s.err
}
func (s *stubSessionRepo) Revoke(uid, id uint) (bool, error) {
	s.gotUID, s.gotID = uid, id
	return s.revokeOK, s.err
}
func (s *stubSessionRepo) RevokeAllForUser(uid uint) error {
	s.gotUID = uid
	return s.err
}

func TestSessionListMarksCurrent(t *testing.T) {
	now := time.Now()
	repo := &stubSessionRepo{rows: []model.UserSession{
		{ID: 1, SID: "a", Platform: "web", LastSeenAt: now},
		{ID: 2, SID: "b", Platform: "desktop", LastSeenAt: now},
	}}
	svc := NewSessionService(repo)

	out, err := svc.List(7, "b")
	if err != nil {
		t.Fatal(err)
	}
	if repo.gotUID != 7 {
		t.Fatalf("应只查当前用户会话: %d", repo.gotUID)
	}
	if len(out) != 2 || !out[1].Current || out[0].Current {
		t.Fatalf("当前设备标记错误: %+v", out)
	}
}

func TestSessionRevokeBranches(t *testing.T) {
	// 不存在
	svc := NewSessionService(&stubSessionRepo{sid: "", revokeOK: false})
	if _, err := svc.Revoke(7, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("空 sid 应返回 ErrSessionNotFound: %v", err)
	}

	// 已被并发撤销
	svc = NewSessionService(&stubSessionRepo{sid: "s", revokeOK: false})
	if _, err := svc.Revoke(7, 1); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("撤销失败应返回 ErrSessionNotFound: %v", err)
	}

	// 成功：返回 sid 供踢 WS
	repo := &stubSessionRepo{sid: "s", revokeOK: true}
	svc = NewSessionService(repo)
	sid, err := svc.Revoke(7, 42)
	if err != nil || sid != "s" || repo.gotID != 42 {
		t.Fatalf("撤销应返回 sid 并作用于目标 id: sid=%q id=%d err=%v", sid, repo.gotID, err)
	}
}
