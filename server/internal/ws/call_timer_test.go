package ws

import (
	"testing"
	"time"
)

// 超时 timer 只结束未接听的通话；接听后即使 timer 触发也不能误杀（H3 回归）。
func TestCallTimeoutOnlyWhenUnanswered(t *testing.T) {
	hub, caller, callee := newCallTestHub(1, 2)

	// 用短 TTL 模拟超时：直接调 endCall 前先检查 snapshot 语义
	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "timer-1", ToUserID: 2, MediaType: "video"})
	frameOf(t, callee)

	// 模拟 timer 回调逻辑（与 call.go 中一致）：未接听 → 结束
	if info, ok := hub.calls.snapshot("timer-1"); ok && info.toSID == "" {
		hub.endCall("timer-1", "timeout")
	}
	end := frameOf(t, caller)
	if end["reason"] != "timeout" {
		t.Fatalf("unanswered call should timeout: %v", end)
	}
	frameOf(t, callee) // ended also delivered

	// 接听后再触发同样的 timer 判断：不应结束
	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "timer-2", ToUserID: 2, MediaType: "video"})
	frameOf(t, callee)
	hub.handleCallAccept(callee, callFrame("call.accept", "timer-2"))
	frameOf(t, caller)
	if info, ok := hub.calls.snapshot("timer-2"); ok && info.toSID == "" {
		t.Fatal("accepted call should have toSID set")
	}
	// 翻转 toSID 为空的场景已被 setToSID 幂等与 snapshot 锁读覆盖；
	// 这里直接确认通话仍存活。
	if _, ok := hub.calls.snapshot("timer-2"); !ok {
		t.Fatal("accepted call should remain after timer check")
	}
}

// 并发接听压力测试：多端同时 accept 只有一个成功（竞态检测下验证无 data race）。
func TestCallConcurrentAcceptsSingleWinner(t *testing.T) {
	hub := NewHub(&Deps{})
	caller := newTestClient(hub, 1, 16)
	caller.sid = "sid-caller"
	hub.Register(caller)
	callees := make([]*Client, 8)
	for i := range callees {
		callees[i] = newTestClient(hub, 2, 16)
		callees[i].sid = "dev-" + string(rune('a'+i))
		hub.Register(callees[i])
	}

	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "race-1", ToUserID: 2, MediaType: "video"})
	for _, c := range callees {
		frameOf(t, c)
	}

	done := make(chan struct{}, len(callees))
	for _, c := range callees {
		go func(cl *Client) {
			hub.handleCallAccept(cl, callFrame("call.accept", "race-1"))
			done <- struct{}{}
		}(c)
	}
	for range callees {
		<-done
	}

	// 主叫只应收到一条 accepted
	count := 0
	for {
		select {
		case <-caller.send:
			count++
		case <-time.After(100 * time.Millisecond):
			goto doneCheck
		}
	}
doneCheck:
	if count != 1 {
		t.Fatalf("expected exactly one accepted frame, got %d", count)
	}
	info, _ := hub.calls.snapshot("race-1")
	if info.toSID == "" {
		t.Fatal("call should have an answering device")
	}
}
