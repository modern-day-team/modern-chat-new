package model

import "time"

// UserSession 登录会话。每个 JWT 的 sid 对应一行，用于跨端会话列表与单设备撤销。
//
// 为什么会需要这张表：JWT 无状态、只有 uid/tv，此前只能全局 logout-others。
// 桌面 / 移动 / Web 同时在线时，用户需要看到「哪台设备登录着」并能单独踢掉，
// 因此把 sid + 客户端标识落库。Token 本身不入库（只存 sid，泄露数据库无法伪造 JWT）。
type UserSession struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     uint      `gorm:"index:idx_user_sessions_user;not null" json:"-"`
	SID        string    `gorm:"column:sid;size:64;uniqueIndex:uk_user_sessions_sid;not null" json:"-"`
	Platform   string    `gorm:"size:32;default:web" json:"platform"`
	ClientType string    `gorm:"size:32;default:''" json:"clientType"`
	AppVersion string    `gorm:"size:32;default:''" json:"appVersion"`
	IP         string    `gorm:"size:64;default:''" json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	// RevokedAt 非空表示该会话已被撤销：JWT 仍有效但不再被接受，客户端需重新登录。
	RevokedAt *time.Time `json:"-"`
}

// SessionView 对外会话视图（只含可展示字段，不含 sid / token）。
type SessionView struct {
	ID         uint      `json:"id"`
	Platform   string    `json:"platform"`
	ClientType string    `json:"clientType"`
	AppVersion string    `json:"appVersion"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current"`
}
