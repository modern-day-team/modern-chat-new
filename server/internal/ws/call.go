package ws

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"
)

// 通话信令只做中继与状态校验：媒体流端到端走浏览器 RTCPeerConnection，
// 服务端不落库、不解码、不记录通话内容（后端需求规格 §3.3.1）。
const (
	callInviteTTL = 60 * time.Second // 未接听自动超时
	callMaxTTL    = 5 * time.Minute  // 状态兜底清理（正常挂断会提前删除）
)

// callInfo 一路通话的内存状态。
type callInfo struct {
	fromUID uint
	fromSID string // 发起设备的 sid：answer/ice/accept 精确回传给它
	toUID   uint
	toSID   string // 接听设备的 sid：accept 时填充，offer/ice 精确回传给它
	created time.Time
	timer   *time.Timer // 60s 未接超时；accept 后停止
}

// callManager 通话状态：callID -> 状态，uid -> callID（忙线判定）。
// 单实例内存态；多实例不共享（二期边界，见设计文档 §7）。
type callManager struct {
	mu    sync.Mutex
	calls map[string]*callInfo
	byUID map[uint]string
}

func newCallManager() *callManager {
	return &callManager{calls: map[string]*callInfo{}, byUID: map[uint]string{}}
}

// add 登记一路通话；任一方已有通话时返回 false（忙线）。
func (m *callManager) add(id string, from, to uint, fromSID string, timer *time.Timer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	if _, busy := m.byUID[from]; busy {
		return false
	}
	if _, busy := m.byUID[to]; busy {
		return false
	}
	m.calls[id] = &callInfo{fromUID: from, fromSID: fromSID, toUID: to, created: time.Now(), timer: timer}
	m.byUID[from] = id
	m.byUID[to] = id
	return true
}

// get 取通话状态（只读）。
func (m *callManager) get(id string) (*callInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	info, ok := m.calls[id]
	return info, ok
}

// setToSID 记录接听设备 sid（仅被叫接听时调用一次）。
func (m *callManager) setToSID(id, sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info := m.calls[id]; info != nil {
		info.toSID = sid
	}
}

// remove 删除通话并释放双方忙线。
func (m *callManager) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info := m.calls[id]; info != nil {
		if info.timer != nil {
			info.timer.Stop()
		}
		delete(m.byUID, info.fromUID)
		delete(m.byUID, info.toUID)
	}
	delete(m.calls, id)
}

// gcLocked 清理超过 callMaxTTL 的僵尸通话（持有 mu 时调用）。
func (m *callManager) gcLocked() {
	now := time.Now()
	for id, info := range m.calls {
		if now.Sub(info.created) > callMaxTTL {
			if info.timer != nil {
				info.timer.Stop()
			}
			delete(m.byUID, info.fromUID)
			delete(m.byUID, info.toUID)
			delete(m.calls, id)
		}
	}
}

// newCallID 生成 128 位随机通话 ID。
func newCallID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "c" + hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf)
}

// ToSid 向指定用户的指定设备（sid）发帧；没有匹配连接时静默返回。
// 与 kickSessionLocal（hub.go）同构：只序列化一次，锁内 trySend，不持锁做网络操作。
func (h *Hub) ToSid(userID uint, sid string, frame any) bool {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws marshal call frame", "err", err.Error())
		return false
	}
	if sid == "" {
		return false
	}
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.conns[userID] {
		if c.sid == sid {
			c.trySend(data)
			return true
		}
	}
	return false
}

// endCall 结束通话并通知双方（toSid 为空时向对端全部连接广播）。
func (h *Hub) endCall(id, reason string) {
	info, ok := h.calls.get(id)
	if !ok {
		return
	}
	h.calls.remove(id)
	h.ToUsers([]uint{info.fromUID}, ginH{"type": "call.ended", "callId": id, "reason": reason})
	h.ToUsers([]uint{info.toUID}, ginH{"type": "call.ended", "callId": id, "reason": reason})
}

// dispatchCall 通话信令路由（已过控制帧令牌桶）。
func (h *Hub) dispatchCall(c *Client, f *clientFrame) {
	switch f.Type {
	case "call.invite":
		h.handleCallInvite(c, f)
	case "call.accept":
		h.handleCallAccept(c, f)
	case "call.reject":
		h.handleCallReject(c, f)
	case "call.offer", "call.answer", "call.ice":
		h.handleCallSignal(c, f)
	case "call.hangup":
		h.handleCallHangup(c, f)
	}
}

// handleCallInvite 发起通话：目标须在线、双方须好友或同会话成员、双方均不忙。
func (h *Hub) handleCallInvite(c *Client, f *clientFrame) {
	if f.CallID == "" || f.ToUserID == 0 || f.ToUserID == c.UserID {
		h.sendError(c, "通话参数无效")
		return
	}
	if !h.Online(f.ToUserID) {
		h.sendError(c, "对方当前不在线")
		return
	}
	if h.deps != nil && h.deps.Friends != nil {
		friends, err := h.deps.Friends.PendingBetween(c.UserID, f.ToUserID, 1)
		if err != nil {
			slog.Warn("call friend check failed", "err", err.Error())
			h.sendError(c, "无法发起通话，请稍后再试")
			return
		}
		if len(friends) == 0 && !h.sameConversation(c.UserID, f.ToUserID, f.ConvID) {
			h.sendError(c, "只能与好友或同会话成员通话")
			return
		}
	}
	id := f.CallID
	timer := time.AfterFunc(callInviteTTL, func() {
		if info, ok := h.calls.get(id); ok && info.toSID == "" {
			h.endCall(id, "timeout")
		}
	})
	if !h.calls.add(id, c.UserID, f.ToUserID, c.sid, timer) {
		timer.Stop()
		c.trySend(mustJSON(ginH{"type": "call.busy", "callId": id}))
		return
	}
	frame := ginH{
		"type": "call.incoming", "callId": id,
		"fromUserId": c.UserID, "mediaType": f.MediaType,
	}
	if h.deps != nil && h.deps.Users != nil {
		if u, err := h.deps.Users.ByID(c.UserID); err == nil && u != nil {
			frame["nickname"] = u.Nickname
			frame["avatar"] = u.AvatarThumb
		}
	}
	if frame["nickname"] == nil {
		frame["nickname"] = "用户 " + strconv.FormatUint(uint64(c.UserID), 10)
	}
	h.ToUsers([]uint{f.ToUserID}, frame)
}

// sameConversation 校验双方是否为同一会话成员（convID 为 0 时放弃该路径）。
func (h *Hub) sameConversation(a, b, convID uint) bool {
	if convID == 0 || h.deps == nil || h.deps.Convs == nil {
		return false
	}
	ma, err := h.deps.Convs.MemberOf(convID, a)
	if err != nil || ma == nil {
		return false
	}
	mb, err := h.deps.Convs.MemberOf(convID, b)
	return err == nil && mb != nil
}

// callPeer 校验通话存在且 c 是参与方，返回对端 uid 与状态。
func (h *Hub) callPeer(c *Client, id string) (*callInfo, uint, bool) {
	info, ok := h.calls.get(id)
	if !ok {
		return nil, 0, false
	}
	if c.UserID == info.fromUID {
		return info, info.toUID, true
	}
	if c.UserID == info.toUID {
		return info, info.fromUID, true
	}
	return nil, 0, false
}

// handleCallAccept 被叫接听：记录接听设备 sid，回主叫 accepted（带 fromSid 供其发 offer）。
func (h *Hub) handleCallAccept(c *Client, f *clientFrame) {
	info, _, ok := h.callPeer(c, f.CallID)
	if !ok || c.UserID != info.toUID {
		return
	}
	h.calls.setToSID(f.CallID, c.sid)
	if info.timer != nil {
		info.timer.Stop()
	}
	h.ToSid(info.fromUID, info.fromSID, ginH{
		"type": "call.accepted", "callId": f.CallID, "fromSid": c.sid,
	})
}

// handleCallReject 被叫拒绝：清理并回主叫 rejected。
func (h *Hub) handleCallReject(c *Client, f *clientFrame) {
	info, _, ok := h.callPeer(c, f.CallID)
	if !ok || c.UserID != info.toUID {
		return
	}
	h.calls.remove(f.CallID)
	h.ToSid(info.fromUID, info.fromSID, ginH{"type": "call.rejected", "callId": f.CallID})
}

// handleCallSignal 转发 SDP / ICE：只接受参与方，且 toSid 必须是当前对端设备。
func (h *Hub) handleCallSignal(c *Client, f *clientFrame) {
	info, peerUID, ok := h.callPeer(c, f.CallID)
	if !ok || f.ToSid == "" {
		return
	}
	wantSID := info.fromSID
	if c.UserID == info.fromUID {
		if info.toSID == "" {
			return // 被叫尚未接听
		}
		wantSID = info.toSID
	}
	if f.ToSid != wantSID {
		h.sendError(c, "通话设备不匹配")
		return
	}
	frame := ginH{"type": f.Type, "callId": f.CallID}
	if f.Type == "call.ice" {
		frame["candidate"] = f.Candidate
	} else {
		frame["sdp"] = f.SDP
	}
	h.ToSid(peerUID, f.ToSid, frame)
}

// handleCallHangup 任一方挂断：向双方广播 ended 并清理状态。
func (h *Hub) handleCallHangup(c *Client, f *clientFrame) {
	if _, _, ok := h.callPeer(c, f.CallID); !ok {
		return
	}
	h.endCall(f.CallID, "hangup")
}
