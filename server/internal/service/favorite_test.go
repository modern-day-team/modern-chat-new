package service

import (
	"errors"
	"testing"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

type fakeFavoriteRepo struct {
	byID        map[uint]*model.Message
	favorited   []uint
	unfavorited []uint
	limitSeen   int
	rows        []model.FavoriteView
}

func (f *fakeFavoriteRepo) ByID(id uint) (*model.Message, error) { return f.byID[id], nil }
func (f *fakeFavoriteRepo) Favorite(_, messageID uint) error {
	f.favorited = append(f.favorited, messageID)
	return nil
}
func (f *fakeFavoriteRepo) Unfavorite(_, messageID uint) error {
	f.unfavorited = append(f.unfavorited, messageID)
	return nil
}
func (f *fakeFavoriteRepo) Favorites(_ uint, limit int) ([]model.FavoriteView, error) {
	f.limitSeen = limit
	return f.rows, nil
}

func TestFavoriteMessageByID(t *testing.T) {
	repo := &fakeFavoriteRepo{byID: map[uint]*model.Message{
		1: {ID: 1, ConversationID: 10},
	}}
	svc := NewFavoriteService(repo)

	msg, err := svc.MessageByID(1)
	if err != nil || msg.ID != 1 {
		t.Fatalf("existing message: %+v %v", msg, err)
	}
	if _, err := svc.MessageByID(999); !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("missing message should map to ErrMessageNotFound, got %v", err)
	}
}

func TestFavoriteActions(t *testing.T) {
	repo := &fakeFavoriteRepo{}
	svc := NewFavoriteService(repo)
	if err := svc.Favorite(1, 42); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unfavorite(1, 42); err != nil {
		t.Fatal(err)
	}
	if len(repo.favorited) != 1 || repo.favorited[0] != 42 {
		t.Fatalf("favorite not delegated: %v", repo.favorited)
	}
	if len(repo.unfavorited) != 1 || repo.unfavorited[0] != 42 {
		t.Fatalf("unfavorite not delegated: %v", repo.unfavorited)
	}
}

func TestFavoriteListNormalization(t *testing.T) {
	repo := &fakeFavoriteRepo{rows: nil}
	svc := NewFavoriteService(repo)

	// nil 结果归一为空数组，否则 JSON 序列化成 null 会让前端 .map 崩溃。
	rows, err := svc.Favorites(1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil || len(rows) != 0 {
		t.Fatalf("nil rows should normalize to empty slice, got %#v", rows)
	}

	// limit 越界回落到 100；合法值原样透传。
	if _, err := svc.Favorites(1, 0); err != nil || repo.limitSeen != 100 {
		t.Fatalf("limit=0 should fall back to 100, got %d (%v)", repo.limitSeen, err)
	}
	if _, err := svc.Favorites(1, 999); err != nil || repo.limitSeen != 100 {
		t.Fatalf("limit=999 should fall back to 100, got %d (%v)", repo.limitSeen, err)
	}
	if _, err := svc.Favorites(1, 20); err != nil || repo.limitSeen != 20 {
		t.Fatalf("limit=20 should pass through, got %d (%v)", repo.limitSeen, err)
	}
}
