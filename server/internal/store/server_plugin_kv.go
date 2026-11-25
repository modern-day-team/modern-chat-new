package store

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// ServerPluginKVStore 服务端插件专属持久 KV。
type ServerPluginKVStore struct{ db *gorm.DB }

func NewServerPluginKVStore(db *gorm.DB) *ServerPluginKVStore { return &ServerPluginKVStore{db: db} }

// Get 不存在时返回空串。
func (s *ServerPluginKVStore) Get(pluginID uint, key string) (string, error) {
	var row model.ServerPluginKV
	err := s.db.Where("plugin_id = ? AND `key` = ?", pluginID, key).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

// Set 覆盖写。
func (s *ServerPluginKVStore) Set(pluginID uint, key, value string) error {
	row := model.ServerPluginKV{PluginID: pluginID, Key: key, Value: value, UpdatedAt: time.Now()}
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "plugin_id"}, {Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

// Delete 删除键。
func (s *ServerPluginKVStore) Delete(pluginID uint, key string) error {
	return s.db.Where("plugin_id = ? AND `key` = ?", pluginID, key).Delete(&model.ServerPluginKV{}).Error
}

// Keys 返回键列表（上限 500）。
func (s *ServerPluginKVStore) Keys(pluginID uint) ([]string, error) {
	var keys []string
	err := s.db.Model(&model.ServerPluginKV{}).
		Where("plugin_id = ?", pluginID).
		Order("`key` ASC").Limit(500).Pluck("`key`", &keys).Error
	return keys, err
}

// Clear 删除插件全部 KV（卸载时调用）。
func (s *ServerPluginKVStore) Clear(pluginID uint) error {
	return s.db.Where("plugin_id = ?", pluginID).Delete(&model.ServerPluginKV{}).Error
}
