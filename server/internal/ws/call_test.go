package ws

import (
	"testing"
	"time"
)

// 忙线判定：同一用户不能在两路通话里；挂断后释放。
func TestCallManagerBusyAndRelease(t *testing.T) {
	m := newCallManager()
	if !m.add("c1", 1, 2, "sid-a", nil) {
		t.Fatal("first call should be registered")
	}
	if m.add("c2", 1, 3, "sid-b", nil) {
		t.Fatal("caller already busy should be rejected")
	}
	if m.add("c3", 2, 3, "sid-c", nil) {
		t.Fatal("callee already busy should be rejected")
	}
	m.remove("c1")
	if !m.add("c2", 1, 3, "sid-b", nil) {
		t.Fatal("busy flag should be released after remove")
	}
}

// get 取状态；remove 后不可再取。
func TestCallManagerGetRemove(t *testing.T) {
	m := newCallManager()
	timer := time.NewTimer(time.Hour)
	if !m.add("c1", 1, 2, "sid-a", timer) {
		t.Fatal("add failed")
	}
	info, ok := m.snapshot("c1")
	if !ok || info.fromUID != 1 || info.toUID != 2 || info.fromSID != "sid-a" {
		t.Fatalf("unexpected call info: %+v %v", info, ok)
	}
	if !m.setToSID("c1", "sid-b") {
		t.Fatal("first setToSID should succeed")
	}
	if m.setToSID("c1", "sid-c") {
		t.Fatal("duplicate setToSID should be ignored")
	}
	info, _ = m.snapshot("c1")
	if info.toSID != "sid-b" {
		t.Fatalf("toSID not recorded: %+v", info)
	}
	m.remove("c1")
	if _, ok := m.snapshot("c1"); ok {
		t.Fatal("removed call should not be found")
	}
	select {
	case <-timer.C:
		t.Fatal("timer should be stopped")
	default:
	}
}

// 重复 callId 不得覆盖已有通话（H2：否则忙线永久泄漏）。
func TestCallManagerDuplicateIDRejected(t *testing.T) {
	m := newCallManager()
	if !m.add("same", 1, 2, "sid-a", nil) {
		t.Fatal("first add failed")
	}
	if m.add("same", 3, 4, "sid-c", nil) {
		t.Fatal("duplicate callId should be rejected")
	}
	if _, ok := m.snapshot("same"); !ok {
		t.Fatal("original call should remain")
	}
	if !m.add("other", 3, 4, "sid-c", nil) {
		t.Fatal("different callId with free users should succeed")
	}
}

// 断线设备查找：主叫设备与非接听的被叫设备不影响通话（H4）。
func TestCallManagerFindByDevice(t *testing.T) {
	m := newCallManager()
	if !m.add("c1", 1, 2, "sid-caller", nil) {
		t.Fatal("add failed")
	}
	if got := m.findByDevice(1, "sid-caller"); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("caller device should find call: %v", got)
	}
	if got := m.findByDevice(2, "sid-other"); len(got) != 0 {
		t.Fatalf("callee's non-answering device should not find call: %v", got)
	}
	m.setToSID("c1", "sid-answer")
	if got := m.findByDevice(2, "sid-answer"); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("answering device should find call: %v", got)
	}
	if got := m.findByDevice(2, "sid-other"); len(got) != 0 {
		t.Fatalf("unrelated device should not find call: %v", got)
	}
}

// ToSid 只投给指定设备，不投给同用户其它设备。
func TestToSidTargetsOneDevice(t *testing.T) {
	hub := NewHub(nil)
	a := newTestClient(hub, 7, 4)
	b := newTestClient(hub, 7, 4)
	a.sid = "target"
	b.sid = "other"
	hub.Register(a)
	hub.Register(b)

	if !hub.ToSid(7, "target", ginH{"type": "call.accepted", "callId": "c1"}) {
		t.Fatal("target device should receive frame")
	}
	select {
	case data := <-a.send:
		if len(data) == 0 {
			t.Fatal("empty frame")
		}
	case <-time.After(time.Second):
		t.Fatal("target device frame not queued")
	}
	select {
	case <-b.send:
		t.Fatal("other device should not receive frame")
	default:
	}
}

// 未知 sid 不投递，不 panic。
func TestToSidUnknownNoPanic(t *testing.T) {
	hub := NewHub(nil)
	if hub.ToSid(999, "nope", ginH{"type": "call.ended"}) {
		t.Fatal("unknown device should not be targeted")
	}
	if hub.ToSid(999, "", ginH{"type": "call.ended"}) {
		t.Fatal("empty sid should not be targeted")
	}
}
