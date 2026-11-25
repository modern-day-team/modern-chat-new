package store

import (
	"gorm.io/gorm"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// ServerPluginStore 管理员安装的服务端声明式插件。
type ServerPluginStore struct{ db *gorm.DB }

func NewServerPluginStore(db *gorm.DB) *ServerPluginStore { return &ServerPluginStore{db: db} }

// All 全部（站长列表）。
func (s *ServerPluginStore) All() ([]model.ServerPlugin, error) {
	var rows []model.ServerPlugin
	err := s.db.Order("created_at DESC").Find(&rows).Error
	return rows, err
}

// Enabled 已启用的插件（runtime Reload 用），需要 ManifestJSON。
func (s *ServerPluginStore) Enabled() ([]model.ServerPlugin, error) {
	var rows []model.ServerPlugin
	err := s.db.Where("enabled = ?", true).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (s *ServerPluginStore) ByID(id uint) (*model.ServerPlugin, error) {
	var p model.ServerPlugin
	err := s.db.First(&p, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ByName 按唯一名称查找（安装即同名升级）。
func (s *ServerPluginStore) ByName(name string) (*model.ServerPlugin, error) {
	var p model.ServerPlugin
	err := s.db.Where("name = ?", name).First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *ServerPluginStore) Create(p *model.ServerPlugin) error { return s.db.Create(p).Error }

func (s *ServerPluginStore) Save(p *model.ServerPlugin) error { return s.db.Save(p).Error }

func (s *ServerPluginStore) Delete(id uint) error {
	return s.db.Delete(&model.ServerPlugin{}, id).Error
}
