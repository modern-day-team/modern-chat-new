package ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/cluster"
)

// fakeBus 内存事件总线：记录 Publish 次数，并把非自己发布的事件同步回调给订阅者。
// nodeID 模拟「本实例」标识，用于验证回声过滤。
type fakeBus struct {
	nodeID string
	mu     sync.Mutex
	sent   []cluster.Event
	sub    func(cluster.Event)
}

func (f *fakeBus) Publish(ev cluster.Event) error {
	f.mu.Lock()
	f.sent = append(f.sent, ev)
	f.mu.Unlock()
	return nil
}

func (f *fakeBus) Subscribe(_ context.Context, cb func(cluster.Event)) error {
	f.mu.Lock()
	f.sub = cb
	f.mu.Unlock()
	return nil
}

func (f *fakeBus) injected(ev cluster.Event) {
	f.mu.Lock()
	cb := f.sub
	f.mu.Unlock()
	if cb != nil {
		cb(ev)
	}
}

func (f *fakeBus) published() []cluster.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cluster.Event(nil), f.sent...)
}

func newClusterHub(bus *fakeBus) *Hub {
	return NewHub(&Deps{Broadcaster: bus, ClusterEvents: bus})
}

// KickUser 踢本地连接后应广播 user 事件。
func TestClusterKickUserBroadcasts(t *testing.T) {
	bus := &fakeBus{nodeID: "node-a"}
	hub := newClusterHub(bus)
	c := newTestClient(hub, 7, 2)
	hub.Register(c)

	hub.KickUser(7, "封禁")

	select {
	case <-c.kickCh:
	case <-time.After(time.Second):
		t.Fatal("本地连接应被踢")
	}
	sent := bus.published()
	if len(sent) != 1 || sent[0].Kind != cluster.KindKickUser || sent[0].UID != 7 || sent[0].Reason != "封禁" {
		t.Fatalf("广播事件异常: %+v", sent)
	}
}

// 收到其它实例的 KickUser 事件：踢本地连接，且不再次广播（防回声）。
func TestClusterKickUserFromPeerDoesNotRebroadcast(t *testing.T) {
	bus := &fakeBus{nodeID: "node-a"}
	hub := newClusterHub(bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := hub.deps.ClusterEvents.Subscribe(ctx, func(ev cluster.Event) {
		if ev.Kind == cluster.KindKickUser {
			hub.kickUserLocal(ev.UID, ev.Reason)
		}
	}); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(hub, 9, 2)
	hub.Register(c)

	bus.injected(cluster.Event{Node: "node-b", Kind: cluster.KindKickUser, UID: 9, Reason: "远端封禁"})

	select {
	case <-c.kickCh:
	case <-time.After(time.Second):
		t.Fatal("远端事件应踢本地同 uid 连接")
	}
	if got := bus.published(); len(got) != 0 {
		t.Fatalf("远端事件不应被再次广播: %+v", got)
	}
}

// KickSession 广播 session 事件；收到远端 session 事件只踢本地匹配 sid。
func TestClusterKickSessionBroadcastsAndApplies(t *testing.T) {
	bus := &fakeBus{nodeID: "node-a"}
	hub := newClusterHub(bus)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bus.Subscribe(ctx, func(ev cluster.Event) {
		switch ev.Kind {
		case cluster.KindKickUser:
			hub.kickUserLocal(ev.UID, ev.Reason)
		case cluster.KindKickSession:
			hub.kickSessionLocal(ev.SID, ev.Reason)
		}
	}); err != nil {
		t.Fatal(err)
	}
	local := newTestClient(hub, 1, 2)
	local.sid = "sid-x"
	other := newTestClient(hub, 1, 2)
	other.sid = "sid-y"
	hub.Register(local)
	hub.Register(other)

	// 本地发起：广播 session 事件
	hub.KickSession("sid-x", "下线")
	sent := bus.published()
	if len(sent) != 1 || sent[0].Kind != cluster.KindKickSession || sent[0].SID != "sid-x" {
		t.Fatalf("广播事件异常: %+v", sent)
	}
	select {
	case <-local.kickCh:
	case <-time.After(time.Second):
		t.Fatal("本地匹配 sid 应被踢")
	}
	select {
	case <-other.kickCh:
		t.Fatal("不同 sid 不应被踢")
	default:
	}

	// 远端发起：只踢本地匹配连接，不二次广播（回调与 Hub.Run 一致，处理两种 kind）
	bus.mu.Lock()
	cb := bus.sub
	bus.mu.Unlock()
	if cb != nil {
		cb(cluster.Event{Node: "node-b", Kind: cluster.KindKickSession, SID: "sid-y", Reason: "远端下线"})
	}
	select {
	case <-other.kickCh:
	case <-time.After(time.Second):
		t.Fatal("远端 session 事件应踢本地匹配连接")
	}
	if got := bus.published(); len(got) != 1 {
		t.Fatalf("远端事件不应被再次广播: %+v", got)
	}
}

// 未注入总线时（单实例）踢线照常工作，不 panic。
func TestKickWithoutClusterBus(t *testing.T) {
	hub := NewHub(nil)
	c := newTestClient(hub, 3, 2)
	hub.Register(c)
	hub.KickUser(3, "x")
	select {
	case <-c.kickCh:
	case <-time.After(time.Second):
		t.Fatal("无总线时本地踢线仍应工作")
	}
}
