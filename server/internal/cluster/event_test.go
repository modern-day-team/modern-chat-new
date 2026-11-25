package cluster

import (
	"encoding/json"
	"testing"
)

// 事件字段完整序列化，Node 由 Publish 填充以区分实例。
func TestEventJSON(t *testing.T) {
	ev := Event{Node: "node-a", Kind: KindKickUser, UID: 42, Reason: "封禁"}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got != ev {
		t.Fatalf("roundtrip = %+v, want %+v", got, ev)
	}
}

// sid 事件在 uid=0 时不应丢失 SID 字段。
func TestSessionEventKeepsSID(t *testing.T) {
	ev := Event{Node: "n", Kind: KindKickSession, SID: "abc123", Reason: "下线"}
	data, _ := json.Marshal(ev)
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["sid"] != "abc123" || got["uid"] != nil {
		t.Fatalf("session 事件字段异常: %v", got)
	}
}

// 未配置 Redis（client=nil）时总线为 no-op，组件可独立运行。
func TestBusNoClientIsNoop(t *testing.T) {
	bus := NewBus(nil, "")
	if bus.NodeID() == "" {
		t.Fatal("node id 不应为空")
	}
	if err := bus.Publish(Event{Kind: KindKickUser, UID: 1}); err != nil {
		t.Fatalf("nil client publish 应静默成功: %v", err)
	}
	if err := bus.Subscribe(nil, func(Event) {}); err != nil {
		t.Fatalf("nil client subscribe 应静默成功: %v", err)
	}
}
