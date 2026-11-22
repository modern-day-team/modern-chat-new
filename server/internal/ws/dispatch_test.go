package ws

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidChatCard(t *testing.T) {
	owns := func(s string) bool { return strings.HasPrefix(s, "/uploads/") }
	good := `{"v":1,"title":"群聊","items":[{"n":"张三","t":"2026-11-23T10:00:00Z","m":"text","c":"你好"},{"n":"李四","t":"2026-11-23T10:01:00Z","m":"image","c":"/uploads/a.webp"}]}`
	if !validChatCard(good, []string{"敏感词"}, owns) {
		t.Fatal("valid card should pass")
	}
	// 敏感词藏在 nickname、正文、标题里都要拦住
	for _, raw := range []string{
		strings.Replace(good, "张三", "敏感词", 1),
		strings.Replace(good, "你好", "敏感词", 1),
		strings.Replace(good, "群聊", "敏感词", 1),
	} {
		if validChatCard(raw, []string{"敏感词"}, owns) {
			t.Fatalf("blocked word should be rejected in %s", raw)
		}
	}
	// 快照里的媒体外链：与媒体消息一样必须拦
	badURL := strings.Replace(good, "/uploads/a.webp", "https://evil.example/x.png", 1)
	if validChatCard(badURL, nil, owns) {
		t.Fatal("external media should be rejected")
	}
	if validChatCard("{broken", nil, owns) {
		t.Fatal("malformed JSON should be rejected")
	}
	if validChatCard(`{"v":2,"items":[]}`, nil, owns) {
		t.Fatal("wrong version should be rejected")
	}
	var many strings.Builder
	many.WriteString(`{"v":1,"title":"x","items":[`)
	for i := 0; i <= maxCardItems; i++ {
		if i > 0 {
			many.WriteByte(',')
		}
		fmt.Fprintf(&many, `{"n":"n","t":"t","m":"text","c":"c"}`)
	}
	many.WriteString("]}")
	if validChatCard(many.String(), nil, owns) {
		t.Fatalf("more than %d items should be rejected", maxCardItems)
	}
}

func TestIsSafeMsgType(t *testing.T) {
	good := []string{"text", "weather", "image-v2", "a_b", "chatcard"}
	for _, s := range good {
		if !isSafeMsgType(s) {
			t.Errorf("%q should be safe", s)
		}
	}
	bad := []string{"", "With Upper", "emoji😀", "sql;drop"}
	for _, s := range bad {
		if isSafeMsgType(s) {
			t.Errorf("%q should be rejected", s)
		}
	}
}

func TestUkey(t *testing.T) {
	if ukey(0) != "u0" {
		t.Fatalf("ukey(0) = %s", ukey(0))
	}
	if ukey(42) != "u42" {
		t.Fatalf("ukey(42) = %s", ukey(42))
	}
	// 稳定性
	if ukey(123456789) != ukey(123456789) {
		t.Fatal("ukey should be stable")
	}
}

func TestWithoutID(t *testing.T) {
	got := withoutID([]uint{1, 2, 3, 2}, 2)
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("withoutID = %v", got)
	}
	if len(withoutID(nil, 1)) != 0 {
		t.Fatal("nil input should give empty")
	}
}

func TestHubOnlineAndToUsersNoPanic(t *testing.T) {
	hub := NewHub(nil)
	if hub.Online(1) {
		t.Fatal("empty hub should report offline")
	}
	// 不应 panic（无连接时 ToUsers 为 no-op）
	hub.ToUsers([]uint{1, 2}, ginH{"type": "ping"})
	client := &Client{UserID: 1, send: make(chan []byte, 1)}
	hub.Register(client)
	if !hub.Online(1) {
		t.Fatal("registered client should be online")
	}
	hub.ToUsers([]uint{1}, ginH{"type": "message", "id": 1})
	if len(client.send) != 1 {
		t.Fatal("frame should be queued")
	}
	hub.Unregister(client)
	if hub.Online(1) {
		t.Fatal("after unregister should be offline")
	}
}
