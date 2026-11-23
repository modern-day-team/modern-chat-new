// Package service 的会话管理业务规则：列表的「当前设备」标记、单设备撤销的原子性与踢线决策。
// handler 只做参数解析与状态码映射，不再直连 store。
package service

import (
	"errors"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// ErrSessionNotFound 会话不存在或已撤销（handler 映射 404）。
var ErrSessionNotFound = errors.New("会话不存在或已退出")

// sessionRepo 消费方定义的窄接口（由 store.SessionStore 实现），单测可用假仓储。
type sessionRepo interface {
	List(uid uint) ([]model.UserSession, error)
	SIDByID(uid, id uint) (string, error)
	Revoke(uid, id uint) (bool, error)
	RevokeAllForUser(uid uint) error
}

// SessionService 会话业务规则唯一归属。
type SessionService struct {
	repo sessionRepo
}

func NewSessionService(repo sessionRepo) *SessionService {
	return &SessionService{repo: repo}
}

// List 当前账号活跃会话，按 currentSID 标记「当前设备」。
func (s *SessionService) List(uid uint, currentSID string) ([]model.SessionView, error) {
	rows, err := s.repo.List(uid)
	if err != nil {
		return nil, err
	}
	out := make([]model.SessionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.SessionView{
			ID: row.ID, Platform: row.Platform, ClientType: row.ClientType,
			AppVersion: row.AppVersion, IP: row.IP,
			CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt,
			Current: currentSID != "" && row.SID == currentSID,
		})
	}
	return out, nil
}

// Revoke 撤销单条会话并返回其 sid（供调用方踢该设备的 WS）。
// 把「查 sid → 撤销 → 原子性」合为一个内核操作，避免 handler 分两步带来的竞态与状态不一致。
func (s *SessionService) Revoke(uid, id uint) (string, error) {
	sid, err := s.repo.SIDByID(uid, id)
	if err != nil {
		return "", err
	}
	if sid == "" {
		return "", ErrSessionNotFound
	}
	ok, err := s.repo.Revoke(uid, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrSessionNotFound
	}
	return sid, nil
}

// RevokeAll 撤销账号全部会话（logout-others 用）。
// 与 tv+1 配合：tv 让旧 token 全部失效，本操作让会话记录与踢线口径一致。
func (s *SessionService) RevokeAll(uid uint) error {
	return s.repo.RevokeAllForUser(uid)
}
