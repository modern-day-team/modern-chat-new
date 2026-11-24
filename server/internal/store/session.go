package store

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// SessionStore 登录会话数据访问。只存 sid，不存 JWT 本体。
type SessionStore struct{ db *gorm.DB }

func NewSessionStore(db *gorm.DB) *SessionStore { return &SessionStore{db: db} }

// Touch 首次见到某 sid 时插行，之后只刷新 lastSeenAt 与客户端标识。
// 并发下两个请求同时首次到达不算错误：ON CONFLICT DO UPDATE 保证幂等。
func (s *SessionStore) Touch(uid uint, sid, platform, clientType, appVersion, ip string) (*model.UserSession, error) {
	if sid == "" {
		return nil, nil
	}
	now := time.Now()
	row := &model.UserSession{
		UserID: uid, SID: sid, Platform: platform, ClientType: clientType,
		AppVersion: appVersion, IP: ip, CreatedAt: now, LastSeenAt: now,
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "sid"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_seen_at": now,
			"platform":     platform,
			"client_type":  clientType,
			"app_version":  appVersion,
			"ip":           ip,
		}),
	}).Create(row).Error
	if err != nil {
		return nil, err
	}
	// OnConflict 走 UPDATE 时 row 的主键不会被回填，需要用 sid 再查一次。
	if row.ID == 0 {
		if err := s.db.Where("sid = ?", sid).First(row).Error; err != nil {
			return nil, err
		}
	}
	return row, nil
}

// Activate 登录时按 sid 复用/新建会话行并清除撤销标记。
// 与 Touch 的区别：Touch 只更新已登录请求，不能复活被撤销的会话；
// Activate 只在登录成功、重新签发 JWT 前调用。
func (s *SessionStore) Activate(uid uint, sid, platform, clientType, appVersion, ip string) (*model.UserSession, error) {
	if sid == "" {
		return nil, nil
	}
	now := time.Now()
	row := &model.UserSession{
		UserID: uid, SID: sid, Platform: platform, ClientType: clientType,
		AppVersion: appVersion, IP: ip, CreatedAt: now, LastSeenAt: now,
	}
	err := s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "sid"}},
		DoUpdates: clause.Assignments(map[string]any{
			"platform":     platform,
			"client_type":  clientType,
			"app_version":  appVersion,
			"ip":           ip,
			"last_seen_at": now,
			"revoked_at":   nil,
		}),
	}).Create(row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == 0 {
		if err := s.db.Where("sid = ?", sid).First(row).Error; err != nil {
			return nil, err
		}
	}
	// 清理升级前随机 sid 造成的同设备重复行；只删同一用户/平台/客户端类型，
	// 不影响其他浏览器或设备，也不会误伤已撤销行以外的跨端会话。
	if err := s.db.Where("user_id = ? AND platform = ? AND client_type = ? AND sid <> ?", uid, platform, clientType, sid).
		Delete(&model.UserSession{}).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// ActiveBySID 取未撤销的会话；不存在返回 (nil, nil)。
func (s *SessionStore) ActiveBySID(sid string) (*model.UserSession, error) {
	if sid == "" {
		return nil, nil
	}
	var row model.UserSession
	err := s.db.Where("sid = ? AND revoked_at IS NULL", sid).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// List 列出账号的未撤销会话（按最近活跃倒序）。
func (s *SessionStore) List(uid uint) ([]model.UserSession, error) {
	var rows []model.UserSession
	err := s.db.Where("user_id = ? AND revoked_at IS NULL", uid).
		Order("last_seen_at DESC").Find(&rows).Error
	return rows, err
}

// Revoke 撤销某条会话（原子条件更新，防并发重复撤销）。
// 返回 true 表示本次确实撤销了；false 表示已撤销或不属于该用户。
func (s *SessionStore) Revoke(uid, id uint) (bool, error) {
	res := s.db.Model(&model.UserSession{}).
		Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, uid).
		Update("revoked_at", time.Now())
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// RevokeAllForUser 撤销账号全部会话（logout-others 用；仍同时保留 tv+1 作为兜底）。
func (s *SessionStore) RevokeAllForUser(uid uint) error {
	return s.db.Model(&model.UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", uid).
		Update("revoked_at", time.Now()).Error
}

// SIDByID 取会话的 sid（撤销后用于踢 WS）。
func (s *SessionStore) SIDByID(uid, id uint) (string, error) {
	var row model.UserSession
	err := s.db.Where("id = ? AND user_id = ?", id, uid).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return row.SID, err
}
