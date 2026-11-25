package store

import (
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// TestPluginDataStoreRoundTrip 有 DB_DSN 时验证真实 SQL 往返；无库跳过。
func TestPluginDataStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN 未设置，跳过真实存储往返")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Skipf("数据库连接失败: %v", err)
	}
	if err := db.AutoMigrate(&model.UserPluginData{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	s := NewPluginDataStore(db)
	userID := uint(999999991)
	pluginID := uint(time.Now().UnixNano() % 1000000000)
	if pluginID == 0 {
		pluginID = 1
	}
	t.Cleanup(func() { _ = s.ClearForPlugin(pluginID) })
	_ = s.ClearForPlugin(pluginID)

	// 不存在的 key：found=false 而不是报错。
	if v, found, err := s.Get(userID, pluginID, "missing"); err != nil || found || v != "" {
		t.Fatalf("Get(missing): v=%q found=%v err=%v", v, found, err)
	}

	if err := s.Set(userID, pluginID, "token", "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, found, err := s.Get(userID, pluginID, "token")
	if err != nil || !found || v != "first" {
		t.Fatalf("Get: v=%q found=%v err=%v", v, found, err)
	}

	// 同一 key 覆盖写（唯一索引应命中 ON CONFLICT 分支）。
	if err := s.Set(userID, pluginID, "token", "second"); err != nil {
		t.Fatalf("Set overwrite: %v", err)
	}
	v, _, _ = s.Get(userID, pluginID, "token")
	if v != "second" {
		t.Fatalf("覆盖写失败: got %q", v)
	}

	if err := s.Set(userID, pluginID, "other", "value"); err != nil {
		t.Fatalf("Set other: %v", err)
	}
	keys, err := s.Keys(userID, pluginID)
	if err != nil {
		t.Fatalf("Keys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("Keys 应返回 2 项: %#v", keys)
	}

	// 另一个用户 / 插件的同名 key 必须隔离。
	otherUser := uint(999999992)
	if err := s.Set(otherUser, pluginID, "token", "isolated"); err != nil {
		t.Fatalf("Set other user: %v", err)
	}
	v, _, _ = s.Get(userID, pluginID, "token")
	if v != "second" {
		t.Fatalf("用户隔离失败: got %q", v)
	}

	if err := s.Delete(userID, pluginID, "token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, _ := s.Get(userID, pluginID, "token"); found {
		t.Fatal("删除后不应再找到该 key")
	}
	// 删除用户 A 的 key 不影响用户 B。
	if v, found, _ := s.Get(otherUser, pluginID, "token"); !found || v != "isolated" {
		t.Fatalf("跨用户数据被误删: v=%q found=%v", v, found)
	}

	if err := s.ClearForPlugin(pluginID); err != nil {
		t.Fatalf("ClearForPlugin: %v", err)
	}
	keys, err = s.Keys(userID, pluginID)
	if err != nil || len(keys) != 0 {
		t.Fatalf("ClearForPlugin 后应无 key: %#v err=%v", keys, err)
	}
	keys, err = s.Keys(otherUser, pluginID)
	if err != nil || len(keys) != 0 {
		t.Fatalf("ClearForPlugin 应清掉该插件全部用户数据: %#v err=%v", keys, err)
	}
}
