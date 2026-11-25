package model

import "time"

// UserPluginData 用户插件的服务端私有 KV。
// 唯一索引 user_id + plugin_id + key：一个用户在某个插件下同一个 key 只有一行。
// value 由 handler 限制 64KB，列用 MEDIUMTEXT 留出余量。
type UserPluginData struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	UserID    uint      `gorm:"not null;uniqueIndex:uk_plugin_user_data" json:"userId"`
	PluginID  uint      `gorm:"not null;uniqueIndex:uk_plugin_user_data" json:"pluginId"`
	Key       string    `gorm:"size:120;not null;uniqueIndex:uk_plugin_user_data" json:"key"`
	Value     string    `gorm:"type:mediumtext;not null" json:"value"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)" json:"updatedAt"`
}

// TableName 固定表名，避免 GORM 复数化规则变化。
func (UserPluginData) TableName() string { return "plugin_user_data" }

// ServerPlugin 管理员安装的服务端插件。
// ManifestJSON 存整份 manifest 原文；runtime=wasm 时 WasmData 存 WASM 二进制。
type ServerPlugin struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"size:30;not null;uniqueIndex:uk_server_plugins_name" json:"name"`
	Version      string    `gorm:"size:16;not null" json:"version"`
	Description  string    `gorm:"size:255;not null;default:''" json:"description"`
	ManifestJSON string    `gorm:"type:text;not null" json:"-"`
	WasmData     []byte    `gorm:"type:longblob" json:"-"`
	WasmHash     string    `gorm:"size:64;not null;default:''" json:"wasmHash"`
	Enabled      bool      `gorm:"not null;default:false" json:"enabled"`
	InstalledBy  uint      `gorm:"not null;default:0;index" json:"installedBy"`
	CreatedAt    time.Time `gorm:"column:created_at;type:datetime(3)" json:"createdAt"`
	UpdatedAt    time.Time `gorm:"column:updated_at;type:datetime(3)" json:"updatedAt"`
}

// TableName 固定表名。
func (ServerPlugin) TableName() string { return "server_plugins" }

// ServerPluginView 站长列表视图：刻意不含 manifest（可能很大，且属于内部配置）。
type ServerPluginView struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	InstalledBy uint      `json:"installedBy"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// 各声明段数量（前端列表展示用；不暴露 manifest 原文）
	Pages    int    `json:"pages"`
	Routes   int    `json:"routes"`
	Events   int    `json:"events"`
	Jobs     int    `json:"jobs"`
	Runtime  string `json:"runtime"`
	WasmSize int64  `json:"wasmSize"`
}

// View 从 ServerPlugin 组装简化视图。
func (p *ServerPlugin) View() ServerPluginView {
	return ServerPluginView{
		ID: p.ID, Name: p.Name, Version: p.Version, Description: p.Description,
		Enabled: p.Enabled, InstalledBy: p.InstalledBy,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		WasmSize: int64(len(p.WasmData)),
	}
}

// ServerPluginKV 服务端插件专属持久 KV（WASM 宿主 kv.* 使用）。
type ServerPluginKV struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	PluginID  uint      `gorm:"not null;uniqueIndex:uk_server_plugin_kv" json:"pluginId"`
	Key       string    `gorm:"size:120;not null;uniqueIndex:uk_server_plugin_kv" json:"key"`
	Value     string    `gorm:"type:mediumtext;not null" json:"value"`
	UpdatedAt time.Time `gorm:"column:updated_at;type:datetime(3)" json:"updatedAt"`
}

// TableName 固定表名。
func (ServerPluginKV) TableName() string { return "server_plugin_kv" }
