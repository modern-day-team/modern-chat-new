package store

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
)

// TestSessionColumnNames 锁定会话表列名与迁移 SQL 一致。
// SID 曾未显式声明 column:sid，AutoMigrate 生成 s_id，导致开发/集成库查询失败。
func TestSessionColumnNames(t *testing.T) {
	field, ok := reflect.TypeOf(model.UserSession{}).FieldByName("SID")
	if !ok {
		t.Fatal("UserSession 缺少 SID 字段")
	}
	tag := field.Tag.Get("gorm")
	if !strings.Contains(tag, "column:sid") {
		t.Fatalf("SID 必须显式声明 column:sid，got %q", tag)
	}
	if !strings.Contains(tag, "uniqueIndex:uk_user_sessions_sid") {
		t.Fatalf("SID 唯一索引名必须与迁移一致，got %q", tag)
	}
}

// TestSessionStoreRoundTrip 在有 DB_DSN 时验证真实 SQL 往返；无库跳过（CI integration 覆盖）。
func TestSessionStoreRoundTrip(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN 未设置，跳过真实存储往返")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Skipf("数据库连接失败: %v", err)
	}
	if err := db.AutoMigrate(&model.UserSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	s := NewSessionStore(db)
	sid := "test-session-" + time.Now().Format("20060102150405.000")
	row, err := s.Touch(1, sid, "web", "test", "0", "127.0.0.1")
	if err != nil || row == nil {
		t.Fatalf("Touch: row=%v err=%v", row, err)
	}
	got, err := s.ActiveBySID(sid)
	if err != nil || got == nil || got.SID != sid {
		t.Fatalf("ActiveBySID: got=%v err=%v", got, err)
	}
	if err := s.RevokeAllForUser(1); err != nil {
		t.Fatalf("RevokeAllForUser: %v", err)
	}
	_ = db.Exec("DELETE FROM user_sessions WHERE sid = ?", sid)
}

// TestSessionActivateReusesDevice 同设备重复登录应复用同一行，并在被撤销后重新激活。
func TestSessionActivateReusesDevice(t *testing.T) {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		t.Skip("DB_DSN 未设置，跳过真实存储往返")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Skipf("数据库连接失败: %v", err)
	}
	if err := db.AutoMigrate(&model.UserSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	s := NewSessionStore(db)
	sid := "test-device-" + time.Now().Format("20060102150405.000")
	cleanup := func() { _ = db.Exec("DELETE FROM user_sessions WHERE sid = ?", sid) }
	cleanup()
	t.Cleanup(cleanup)

	first, err := s.Activate(1, sid, "web", "Chrome · Windows", "0.12.0", "127.0.0.1")
	if err != nil || first == nil {
		t.Fatalf("first Activate: row=%v err=%v", first, err)
	}
	if _, err := s.Revoke(1, first.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	second, err := s.Activate(1, sid, "web", "Chrome · Windows", "0.12.0", "127.0.0.1")
	if err != nil || second == nil {
		t.Fatalf("second Activate: row=%v err=%v", second, err)
	}
	if second.ID != first.ID {
		t.Fatalf("同设备应复用会话行: first=%d second=%d", first.ID, second.ID)
	}
	active, err := s.ActiveBySID(sid)
	if err != nil || active == nil {
		t.Fatalf("重新激活失败: row=%v err=%v", active, err)
	}
}
