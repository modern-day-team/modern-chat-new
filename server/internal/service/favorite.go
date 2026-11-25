// Package service 的消息收藏业务规则：消息归属校验、收藏/取消收藏、列表空数组兜底。
// handler 只做参数解析与状态码映射，不再直连 MessageStore。
package service

import (
	"errors"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// ErrMessageNotFound 消息不存在或不属于该会话（handler 映射 404）。
var ErrMessageNotFound = errors.New("消息不存在")

// favoriteRepo 消费方定义的窄接口（由 store.MessageStore 实现），单测可用假仓储。
type favoriteRepo interface {
	ByID(id uint) (*model.Message, error)
	Favorite(userID, messageID uint) error
	Unfavorite(userID, messageID uint) error
	Favorites(userID uint, limit int) ([]model.FavoriteView, error)
}

// FavoriteService 收藏业务规则唯一归属。
type FavoriteService struct {
	repo favoriteRepo
}

func NewFavoriteService(repo favoriteRepo) *FavoriteService {
	return &FavoriteService{repo: repo}
}

// MessageByID 按 ID 取消息并校验存在（消息不存在统一为 ErrMessageNotFound）。
// 会话成员权限校验属于 handler 的传输层边界，不在此处——它需要当前调用者的身份上下文。
func (s *FavoriteService) MessageByID(messageID uint) (*model.Message, error) {
	msg, err := s.repo.ByID(messageID)
	if err != nil {
		return nil, err
	}
	if msg == nil {
		return nil, ErrMessageNotFound
	}
	return msg, nil
}

// Favorite / Unfavorite 收藏与取消收藏（幂等，由 store 的 UPSERT / DELETE 保证）。
func (s *FavoriteService) Favorite(userID, messageID uint) error {
	return s.repo.Favorite(userID, messageID)
}

func (s *FavoriteService) Unfavorite(userID, messageID uint) error {
	return s.repo.Unfavorite(userID, messageID)
}

// Favorites 收藏列表，limit 归一化到 [1,200]，默认 100；结果保证非 nil（空数组而非 null）。
func (s *FavoriteService) Favorites(userID uint, limit int) ([]model.FavoriteView, error) {
	n := limit
	if n < 1 || n > 200 {
		n = 100
	}
	rows, err := s.repo.Favorites(userID, n)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []model.FavoriteView{}
	}
	return rows, nil
}
