package store

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// PluginDataStore 用户插件的服务端私有 KV。
// key / value 的长度校验在 handler 完成，store 只负责存取。
type PluginDataStore struct{ db *gorm.DB }

func NewPluginDataStore(db *gorm.DB) *PluginDataStore { return &PluginDataStore{db: db} }

// Get 读取一项。found=false 表示不存在（与「存在但 value 为空串」区分）。
func (s *PluginDataStore) Get(userID, pluginID uint, key string) (string, bool, error) {
	var row model.UserPluginData
	err := s.db.Where("user_id = ? AND plugin_id = ? AND `key` = ?", userID, pluginID, key).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return row.Value, true, nil
}

// Set 写入一项（不存在则插入，存在则覆盖 value 与 updated_at）。
func (s *PluginDataStore) Set(userID, pluginID uint, key, value string) error {
	now := time.Now()
	row := &model.UserPluginData{
		UserID: userID, PluginID: pluginID, Key: key,
		Value: value, UpdatedAt: now,
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"}, {Name: "plugin_id"}, {Name: "key"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"value": value, "updated_at": now,
		}),
	}).Create(row).Error
}

// Delete 删除一项。
func (s *PluginDataStore) Delete(userID, pluginID uint, key string) error {
	return s.db.Where("user_id = ? AND plugin_id = ? AND `key` = ?", userID, pluginID, key).
		Delete(&model.UserPluginData{}).Error
}

// Keys 列出某插件下该用户已存的全部 key（始终返回非 nil 空切片）。
func (s *PluginDataStore) Keys(userID, pluginID uint) ([]string, error) {
	keys := make([]string, 0)
	err := s.db.Model(&model.UserPluginData{}).
		Where("user_id = ? AND plugin_id = ?", userID, pluginID).
		Pluck("`key`", &keys).Error
	return keys, err
}

// ClearForPlugin 物理删除某插件的全部数据（用户卸载插件时调用）。
func (s *PluginDataStore) ClearForPlugin(pluginID uint) error {
	return s.db.Where("plugin_id = ?", pluginID).Delete(&model.UserPluginData{}).Error
}
