// Package service 的邀请码业务规则：范围校验、有效期计算、CSPRNG 生成与批量创建。
package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

var (
	ErrInviteMaxUses  = errors.New("可用次数需为 1-1000")
	ErrInviteExpires  = errors.New("有效期需为 0（永久）-365 天")
	ErrInviteCount    = errors.New("一次最多生成 20 个")
	ErrInviteNotFound = errors.New("邀请码不存在")
)

// inviteRepo 消费方定义的窄接口（由 store.InviteStore 实现）。
type inviteRepo interface {
	List(limit int) ([]model.InviteCode, error)
	Create(row *model.InviteCode) error
	Disable(id uint, disabled bool) (bool, error)
}

// InviteService 邀请码业务规则唯一归属。
type InviteService struct {
	repo inviteRepo
}

func NewInviteService(repo inviteRepo) *InviteService {
	return &InviteService{repo: repo}
}

func (s *InviteService) List() ([]model.InviteCode, error) {
	rows, err := s.repo.List(100)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []model.InviteCode{} // 空集返回 [] 而不是 null
	}
	return rows, nil
}

// Create 校验范围、计算过期时间并批量生成（12 字节 CSPRNG，不可预测）。
func (s *InviteService) Create(maxUses, expiresDays, count int, createdBy uint, creatorNickname string) ([]model.InviteCode, error) {
	if maxUses < 1 || maxUses > 1000 {
		return nil, ErrInviteMaxUses
	}
	if expiresDays < 0 || expiresDays > 365 {
		return nil, ErrInviteExpires
	}
	if count < 1 || count > 20 {
		return nil, ErrInviteCount
	}
	var expiresAt *time.Time
	if expiresDays > 0 {
		t := time.Now().AddDate(0, 0, expiresDays)
		expiresAt = &t
	}
	out := make([]model.InviteCode, 0, count)
	for i := 0; i < count; i++ {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			return nil, err
		}
		row := &model.InviteCode{
			Code: hex.EncodeToString(buf), CreatedBy: createdBy,
			Creator: creatorNickname, MaxUses: maxUses, ExpiresAt: expiresAt,
		}
		if err := s.repo.Create(row); err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, nil
}

func (s *InviteService) Disable(id uint, disabled bool) error {
	ok, err := s.repo.Disable(id, disabled)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInviteNotFound
	}
	return nil
}
