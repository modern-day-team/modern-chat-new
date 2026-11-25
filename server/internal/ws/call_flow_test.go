package ws

import (
	"encoding/json"
	"testing"
	"time"
)

// frameOf 从 Client.send 队列取一帧并解析 type / 关键字段。
func frameOf(t *testing.T, c *Client) map[string]any {
	t.Helper()
	select {
	case data := <-c.send:
		var f map[string]any
		if err := json.Unmarshal(data, &f); err != nil {
			t.Fatalf("bad frame json: %v (%s)", err, data)
		}
		return f
	case <-time.After(time.Second):
		t.Fatal("expected frame not queued")
		return nil
	}
}

func noFrame(t *testing.T, c *Client) {
	t.Helper()
	select {
	case data := <-c.send:
		t.Fatalf("unexpected frame: %s", data)
	case <-time.After(80 * time.Millisecond):
	}
}

func callFrame(typ, id string) *clientFrame {
	return &clientFrame{Type: typ, CallID: id}
}

// 完整通话流程：邀请 → 来电 → 接听 → offer → answer → ice 双向 → 挂断。
// 这是 H1 的回归测试：被叫 answer 必须送达主叫（旧实现用未初始化的 toSid 会被丢弃）。
func TestCallFlowInviteAnswerHangup(t *testing.T) {
	hub := NewHub(&Deps{}) // Friends/Users 为 nil：跳过关系校验，专注信令链路
	caller := newTestClient(hub, 1, 16)
	caller.sid = "sid-caller"
	callee := newTestClient(hub, 2, 16)
	callee.sid = "sid-callee"
	hub.Register(caller)
	hub.Register(callee)

	// 1) 主叫发起：被叫收到 incoming，带 fromSid
	hub.handleCallInvite(caller, &clientFrame{
		Type: "call.invite", CallID: "flow-1", ToUserID: 2, MediaType: "video", ConvID: 10,
	})
	in := frameOf(t, callee)
	if in["type"] != "call.incoming" || in["callId"] != "flow-1" || in["fromSid"] != "sid-caller" {
		t.Fatalf("incoming frame wrong: %v", in)
	}

	// 2) 被叫接听：主叫收到 accepted，带被叫 sid
	hub.handleCallAccept(callee, callFrame("call.accept", "flow-1"))
	acc := frameOf(t, caller)
	if acc["type"] != "call.accepted" || acc["fromSid"] != "sid-callee" {
		t.Fatalf("accepted frame wrong: %v", acc)
	}

	// 3) 主叫 offer → 被叫（关键：客户端不传 toSid，服务端推导）
	hub.handleCallSignal(caller, &clientFrame{Type: "call.offer", CallID: "flow-1", SDP: "offer-sdp"})
	off := frameOf(t, callee)
	if off["type"] != "call.offer" || off["sdp"] != "offer-sdp" {
		t.Fatalf("offer not delivered to callee: %v", off)
	}

	// 4) 被叫 answer → 主叫（H1 核心回归点）
	hub.handleCallSignal(callee, &clientFrame{Type: "call.answer", CallID: "flow-1", SDP: "answer-sdp"})
	ans := frameOf(t, caller)
	if ans["type"] != "call.answer" || ans["sdp"] != "answer-sdp" {
		t.Fatalf("answer not delivered to caller (H1): %v", ans)
	}

	// 5) ICE 双向
	hub.handleCallSignal(caller, &clientFrame{Type: "call.ice", CallID: "flow-1", Candidate: "c1"})
	if ice := frameOf(t, callee); ice["candidate"] != "c1" {
		t.Fatalf("caller ICE not delivered: %v", ice)
	}
	hub.handleCallSignal(callee, &clientFrame{Type: "call.ice", CallID: "flow-1", Candidate: "c2"})
	if ice := frameOf(t, caller); ice["candidate"] != "c2" {
		t.Fatalf("callee ICE not delivered (H1): %v", ice)
	}

	// 6) 主叫挂断：双方都收到 ended，状态清理
	hub.handleCallHangup(caller, callFrame("call.hangup", "flow-1"))
	for _, c := range []*Client{caller, callee} {
		end := frameOf(t, c)
		if end["type"] != "call.ended" || end["reason"] != "hangup" {
			t.Fatalf("ended frame wrong: %v", end)
		}
	}
	if _, ok := hub.calls.snapshot("flow-1"); ok {
		t.Fatal("call state not removed after hangup")
	}
}

// 语音通话：mediaType=audio 原样下发；未知值归一为 audio（安全边界）。
func TestCallMediaTypeNormalization(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"audio", "audio"},
		{"video", "video"},
		{"", "audio"},
		{"evil", "audio"},
	} {
		hub := NewHub(&Deps{})
		caller := newTestClient(hub, 1, 8)
		caller.sid = "sid-caller"
		callee := newTestClient(hub, 2, 8)
		callee.sid = "sid-callee"
		hub.Register(caller)
		hub.Register(callee)
		hub.handleCallInvite(caller, &clientFrame{
			Type: "call.invite", CallID: "mt-" + tc.in, ToUserID: 2, MediaType: tc.in,
		})
		frame := frameOf(t, callee)
		if frame["mediaType"] != tc.want {
			t.Errorf("mediaType %q → %q, want %q", tc.in, frame["mediaType"], tc.want)
		}
	}
}

// 未接听时主叫发 offer 必须被丢弃（不能投给未接听设备）。
func TestCallOfferBeforeAcceptDropped(t *testing.T) {
	hub := NewHub(&Deps{})
	caller := newTestClient(hub, 1, 8)
	caller.sid = "sid-caller"
	callee := newTestClient(hub, 2, 8)
	callee.sid = "sid-callee"
	hub.Register(caller)
	hub.Register(callee)

	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "pre-1", ToUserID: 2, MediaType: "audio"})
	frameOf(t, callee) // incoming
	hub.handleCallSignal(caller, &clientFrame{Type: "call.offer", CallID: "pre-1", SDP: "early"})
	noFrame(t, callee) // 未接听，offer 不应送达
}

// 多端重复接听只有第一条生效（M2 修复）。
func TestCallDuplicateAcceptIdempotent(t *testing.T) {
	hub := NewHub(&Deps{})
	caller := newTestClient(hub, 1, 8)
	caller.sid = "sid-caller"
	callee1 := newTestClient(hub, 2, 8)
	callee1.sid = "sid-dev-1"
	callee2 := newTestClient(hub, 2, 8)
	callee2.sid = "sid-dev-2"
	hub.Register(caller)
	hub.Register(callee1)
	hub.Register(callee2)

	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "multi-1", ToUserID: 2, MediaType: "video"})
	frameOf(t, callee1) // incoming 发到被叫全部设备
	frameOf(t, callee2)
	hub.handleCallAccept(callee1, callFrame("call.accept", "multi-1"))
	frameOf(t, caller) // 第一条 accepted
	hub.handleCallAccept(callee2, callFrame("call.accept", "multi-1"))
	noFrame(t, caller) // 第二条被忽略

	info, _ := hub.calls.snapshot("multi-1")
	if info.toSID != "sid-dev-1" {
		t.Fatalf("answering device changed on duplicate accept: %s", info.toSID)
	}
}

// 断线清理（H4 修复）：被叫接听设备断开 → 主叫收到 disconnect，状态释放。
func TestCallDisconnectNotifiesPeer(t *testing.T) {
	hub := NewHub(&Deps{})
	caller := newTestClient(hub, 1, 8)
	caller.sid = "sid-caller"
	callee := newTestClient(hub, 2, 8)
	callee.sid = "sid-callee"
	hub.Register(caller)
	hub.Register(callee)

	hub.handleCallInvite(caller, &clientFrame{Type: "call.invite", CallID: "disc-1", ToUserID: 2, MediaType: "video"})
	frameOf(t, callee)
	hub.handleCallAccept(callee, callFrame("call.accept", "disc-1"))
	frameOf(t, caller)

	hub.Unregister(callee) // 模拟掉线（含被踢走同一路径）
	end := frameOf(t, caller)
	if end["type"] != "call.ended" || end["reason"] != "disconnect" {
		t.Fatalf("peer should be notified on disconnect: %v", end)
	}
	if _, ok := hub.calls.snapshot("disc-1"); ok {
		t.Fatal("call state not released on disconnect")
	}
}

// 非参与方不能操作他人通话（安全边界）。
func TestCallNonParticipantRejected(t *testing.T) {
	hub := NewHub(&Deps{})
	a := newTestClient(hub, 1, 8)
	a.sid = "sid-a"
	b := newTestClient(hub, 2, 8)
	b.sid = "sid-b"
	attacker := newTestClient(hub, 3, 8)
	attacker.sid = "sid-attacker"
	hub.Register(a)
	hub.Register(b)
	hub.Register(attacker)

	hub.handleCallInvite(a, &clientFrame{Type: "call.invite", CallID: "sec-1", ToUserID: 2, MediaType: "video"})
	frameOf(t, b)

	// 伪装参与方发 answer / 挂断都无效
	hub.handleCallSignal(attacker, &clientFrame{Type: "call.answer", CallID: "sec-1", SDP: "evil"})
	hub.handleCallHangup(attacker, callFrame("call.hangup", "sec-1"))
	noFrame(t, a)
	noFrame(t, b)
	if _, ok := hub.calls.snapshot("sec-1"); !ok {
		t.Fatal("attacker should not be able to end others' call")
	}
}
