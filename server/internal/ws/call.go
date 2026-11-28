package ws

import (
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
	// callInviteCooldown 同一主叫对被叫的呼叫冷却：挂断/拒绝后立即重呼是
	// 典型的呼死骚扰，且每次都会让对方响铃。冷却期内直接拒绝，间隔足够被叫反应。
	callInviteCooldown = 5 * time.Second
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
	// cooldowns 主叫→被叫的冷却截止时刻：仅在对方挂断/拒接后设置，
	// 防止「挂断即重呼」的呼死骚扰（超时/掉线不冷却，重连后可立即重呼）。
	cooldowns map[callPair]time.Time
}

// callPair 呼叫冷却键（方向敏感：只约束同一主叫重复骚扰同一被叫）。
type callPair struct{ from, to uint }

func newCallManager() *callManager {
	return &callManager{
		calls:     map[string]*callInfo{},
		byUID:     map[uint]string{},
		cooldowns: map[callPair]time.Time{},
	}
}

// add 登记一路通话；callId 已存在或任一方忙时返回 false。
// callId 必须服务端校验去重：客户端可控的 ID 若重复 add 会覆盖旧状态并留下永久忙线。
func (m *callManager) add(id string, from, to uint, fromSID string, timer *time.Timer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	if _, exists := m.calls[id]; exists {
		return false
	}
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

// inviteTooSoon 该主叫对被叫是否仍处于呼叫冷却期（对方刚挂断/拒接）。
func (m *callManager) inviteTooSoon(from, to uint) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	return time.Now().Before(m.cooldowns[callPair{from, to}])
}

// coolDown 对方挂断/拒接后给该主叫→被叫方向加冷却，抑制立即重呼。
func (m *callManager) coolDown(from, to uint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cooldowns[callPair{from, to}] = time.Now().Add(callInviteCooldown)
}

// snapshot 返回通话状态副本（调用方读到的字段不会被后续写操作改动）。
func (m *callManager) snapshot(id string) (callInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	info, ok := m.calls[id]
	if !ok {
		return callInfo{}, false
	}
	return *info, true
}

// setToSID 记录接听设备 sid，幂等：已有接听设备时返回 false（多端重复接听忽略）。
func (m *callManager) setToSID(id, sid string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := m.calls[id]
	if info == nil || info.toSID != "" {
		return false
	}
	info.toSID = sid
	return true
}

// findByDevice 找出指定用户/设备参与的通话，供断线清理。
// 主叫设备断开：未接听（fromSID 匹配）或已建立通话（任一侧 sid 匹配）都要结束；
// 被叫只在自己接听设备上断开时才结束（其它在线设备不应影响）。
func (m *callManager) findByDevice(uid uint, sid string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gcLocked()
	var out []string
	for id, info := range m.calls {
		if uid == info.fromUID && info.fromSID == sid {
			out = append(out, id)
		} else if uid == info.toUID && info.toSID == sid {
			out = append(out, id)
		}
	}
	return out
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

// gcLocked 清理超过 callMaxTTL 的僵尸通话（持有 mu 时调用），
// 并清掉早已过期的呼叫冷却记录，避免 map 随用户对无限增长。
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
	for k, until := range m.cooldowns {
		if now.After(until) {
			delete(m.cooldowns, k)
		}
	}
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

// ToUserExcept 向指定用户除某设备外的全部连接发帧。
// accept 时通知被叫其它设备停止响铃；与 ToSid 同构：只序列化一次，锁内 trySend。
func (h *Hub) ToUserExcept(userID uint, exceptSID string, frame any) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws marshal call frame", "err", err.Error())
		return
	}
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.conns[userID] {
		if c.sid != exceptSID {
			c.trySend(data)
		}
	}
}

// finishCall 结束一路通话的公共收尾：清理状态 + 向双方全部连接广播 ended。
// 挂断 / 超时 / 断线走这里；拒绝是另一种语义（给主叫 rejected），单独处理。
func (h *Hub) finishCall(id string, info callInfo, reason string) {
	h.calls.remove(id)
	// 任一方主动挂断才加冷却；超时/掉线属于自然结束，重连或重试应放行。
	if reason == "hangup" {
		h.calls.coolDown(info.fromUID, info.toUID)
	}
	frame := ginH{"type": "call.ended", "callId": id, "reason": reason}
	h.ToUsers([]uint{info.fromUID}, frame)
	h.ToUsers([]uint{info.toUID}, frame)
}

// endCall 按 callId 结束通话（挂断 / 拒绝 / 超时）。
func (h *Hub) endCall(id, reason string) {
	if info, ok := h.calls.snapshot(id); ok {
		h.finishCall(id, info, reason)
	}
}

// endCallForDevice 设备断开时结束其参与的通话（对端会收到 ended）。
func (h *Hub) endCallForDevice(uid uint, sid, reason string) {
	for _, id := range h.calls.findByDevice(uid, sid) {
		if info, ok := h.calls.snapshot(id); ok {
			h.finishCall(id, info, reason)
		}
	}
}

// dispatchCall 通话信令路由（已过控制帧令牌桶）。
// 站长可在后台关闭通话：关闭后所有 call.* 信令被拒，不只是隐藏前端入口。
func (h *Hub) dispatchCall(c *Client, f *clientFrame) {
	if h.deps != nil && h.deps.Settings != nil && !h.deps.Settings.Ops().CallEnabled {
		c.trySend(mustJSON(ginH{"type": "error", "reason": "站点已关闭语音/视频通话"}))
		return
	}
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

// handleCallInvite 发起通话。校验顺序至关重要：在线状态必须放在好友/同会话
// 鉴权与拉黑之后——否则任意登录用户对任意 user id 发一条 invite，就能用
// 「不在线」/「只能与好友…」两条不同错误探测对方的在线状态（presence 本来
// 只广播给好友与同会话成员）。
func (h *Hub) handleCallInvite(c *Client, f *clientFrame) {
	if f.CallID == "" || f.ToUserID == 0 || f.ToUserID == c.UserID {
		h.sendError(c, "通话参数无效")
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
	// 拉黑拦截：拉黑只写 user_block 行、不解除好友关系，所以「已拉黑的好友」
	// 能通过上面的好友检查。返回模糊错误，避免把拉黑状态反馈成可探测信号。
	if h.deps != nil && h.deps.Moderation != nil {
		blocked, err := h.deps.Moderation.BlockedEither(c.UserID, f.ToUserID)
		if err != nil {
			slog.Warn("call block check failed", "err", err.Error())
			h.sendError(c, "无法发起通话，请稍后再试")
			return
		}
		if blocked {
			h.sendError(c, "无法发起通话")
			return
		}
	}
	if !h.Online(f.ToUserID) {
		h.sendError(c, "对方当前不在线")
		return
	}
	// 冷却：拒绝/挂断后立即重呼会持续让对方响铃，冷却期内不再投递邀请。
	if h.calls.inviteTooSoon(c.UserID, f.ToUserID) {
		c.trySend(mustJSON(ginH{"type": "call.busy", "callId": f.CallID}))
		return
	}
	id := f.CallID
	timer := time.AfterFunc(callInviteTTL, func() {
		// snapshot 在锁内读取，避免 timer 与 accept 的 toSID 竞态。
		if info, ok := h.calls.snapshot(id); ok && info.toSID == "" {
			h.endCall(id, "timeout")
		}
	})
	if !h.calls.add(id, c.UserID, f.ToUserID, c.sid, timer) {
		timer.Stop()
		c.trySend(mustJSON(ginH{"type": "call.busy", "callId": id}))
		return
	}
	// mediaType 只接受 video / audio，未知值归一为 audio（避免客户端按异常值渲染）
	mediaType := f.MediaType
	if mediaType != "video" && mediaType != "audio" {
		mediaType = "audio"
	}
	frame := ginH{
		"type": "call.incoming", "callId": id,
		"fromUserId": c.UserID, "fromSid": c.sid, "mediaType": mediaType,
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

// callPeer 校验通话存在且 c 是参与方，返回状态副本与对端 uid。
func (h *Hub) callPeer(c *Client, id string) (callInfo, uint, bool) {
	info, ok := h.calls.snapshot(id)
	if !ok {
		return callInfo{}, 0, false
	}
	if c.UserID == info.fromUID && c.sid == info.fromSID {
		return info, info.toUID, true
	}
	if c.UserID == info.toUID && info.toSID != "" && c.sid == info.toSID {
		return info, info.fromUID, true
	}
	if c.UserID == info.toUID && info.toSID == "" {
		return info, info.fromUID, true // 未接听阶段，被叫任意设备可应答
	}
	return callInfo{}, 0, false
}

// handleCallAccept 被叫接听：幂等记录设备 sid，回主叫 accepted。
func (h *Hub) handleCallAccept(c *Client, f *clientFrame) {
	info, _, ok := h.callPeer(c, f.CallID)
	if !ok || c.UserID != info.toUID {
		return
	}
	// 多端重复接听只有第一条生效，避免改写 sid 与重复 offer。
	if !h.calls.setToSID(f.CallID, c.sid) {
		return
	}
	if info.timer != nil {
		info.timer.Stop()
	}
	h.ToSid(info.fromUID, info.fromSID, ginH{
		"type": "call.accepted", "callId": f.CallID, "fromSid": c.sid,
	})
	// 多端响铃联动：通话已归属本设备，其余响铃设备立即停止（通话并未结束，
	// 用独立原因与挂断区分，前端据 i18n 提示「已在其他设备接听」）。
	h.ToUserExcept(c.UserID, c.sid, ginH{
		"type": "call.ended", "callId": f.CallID, "reason": "accepted-elsewhere",
	})
}

// handleCallReject 被叫拒绝：清理并回主叫 rejected。
func (h *Hub) handleCallReject(c *Client, f *clientFrame) {
	info, _, ok := h.callPeer(c, f.CallID)
	if !ok || c.UserID != info.toUID {
		return
	}
	h.calls.remove(f.CallID)
	// 被叫拒接后给该方向加冷却，避免主叫立即重呼反复响铃。
	h.calls.coolDown(info.fromUID, info.toUID)
	h.ToSid(info.fromUID, info.fromSID, ginH{"type": "call.rejected", "callId": f.CallID})
}

// handleCallSignal 转发 SDP / ICE：目标设备由服务端根据通话状态推导，
// 客户端不需要（也不允许）指定 toSid —— 避免被叫不知道主叫 sid 导致 answer 全丢。
func (h *Hub) handleCallSignal(c *Client, f *clientFrame) {
	info, peerUID, ok := h.callPeer(c, f.CallID)
	if !ok {
		return
	}
	// 主叫发 offer/ice 前必须已被接听；被叫发 answer/ice 时主叫设备始终已知。
	var targetSID string
	if c.UserID == info.fromUID {
		targetSID = info.toSID
	} else {
		// 被叫必须是接听设备本身：响铃阶段其它设备不能替通话注入 SDP/ICE。
		// （callPeer 对未接听被叫放行所有设备，这里收紧到 toSID 精确匹配。）
		if c.sid != info.toSID {
			return
		}
		targetSID = info.fromSID
	}
	if targetSID == "" {
		return
	}
	frame := ginH{"type": f.Type, "callId": f.CallID}
	if f.Type == "call.ice" {
		frame["candidate"] = f.Candidate
	} else {
		frame["sdp"] = f.SDP
	}
	h.ToSid(peerUID, targetSID, frame)
}

// handleCallHangup 任一方挂断：向双方广播 ended 并清理状态。
func (h *Hub) handleCallHangup(c *Client, f *clientFrame) {
	if _, _, ok := h.callPeer(c, f.CallID); !ok {
		return
	}
	h.endCall(f.CallID, "hangup")
}
