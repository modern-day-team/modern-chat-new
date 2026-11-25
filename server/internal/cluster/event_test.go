package cluster

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
	if err := bus.Subscribe(context.Background(), func(Event) {}); err != nil {
		t.Fatalf("nil client subscribe 应静默成功: %v", err)
	}
}

// sleepBackoff 必须可被 ctx 取消，否则 Hub.Close 后重连 goroutine 无法退出。
func TestSleepBackoffCancellable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if sleepBackoff(ctx, time.Hour) {
		t.Fatal("已取消的 ctx 应立即退出退避")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("取消后不应等待，实际耗时 %v", elapsed)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() { done <- sleepBackoff(ctx2, time.Hour) }()
	time.Sleep(20 * time.Millisecond)
	cancel2()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("等待中被取消应返回 false")
		}
	case <-time.After(time.Second):
		t.Fatal("等待中的退避未被 ctx 取消")
	}
}
