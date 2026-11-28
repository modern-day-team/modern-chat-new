package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// EditWindow 消息编辑时限（与前端 canEdit 的判断保持一致）。
const EditWindow = 5 * time.Minute

// 合并转发卡片是结构化快照（content 存 JSON），20 条摘要远超普通文本上限，
// 用独立上限；字节数仍受 serve.go 的 64KB 帧限制兜底。数值与前端
// web/src/components/chat/mergeCard.ts 的预算对齐（前端留了余量）。
const (
	maxCardContentChars = 12000
	maxCardContentBytes = 60000
	maxCardItems        = 20
)

// 非聊天上行帧限流阈值（F2）：前端 typing 每 2 秒至多一帧（MessageComposer
// 节流）、read 带已上报水位去重、recall/edit/reaction/delivered 均为人类操作
// 或被动回执频率，20 帧/秒 + 突发 40 只会命中脚本刷帧，不会误伤正常使用。
const (
	ctrlFrameRate  = rate.Limit(20)
	ctrlFrameBurst = 40
	// connStateTTL 连接级复核结果复用窗口：封禁 / 重置密码 / 强制下线均由
	// KickUser 立即断连兜底（handler/admin.go、auth.go），这里只是把防御性
	// 复核从「每帧一次查库」降频，不改变安全语义。
	connStateTTL = 5 * time.Second
)

// allowCtrlFrame 非聊天上行帧每连接令牌桶。Dispatch 只由 readPump 串行调用，
// 惰性创建不存在并发竞争。
func (c *Client) allowCtrlFrame() bool {
	if c.ctrl == nil {
		c.ctrl = rate.NewLimiter(ctrlFrameRate, ctrlFrameBurst)
	}
	return c.ctrl.Allow()
}

// 上行帧（§7）：宽松解析，按 type 路由。
type clientFrame struct {
	Type        string `json:"type"`
	ConvID      uint   `json:"convId"`
	ClientMsgID string `json:"clientMsgId"`
	Content     string `json:"content"`
	MsgType     string `json:"msgType"`
	UpToID      uint64 `json:"upToId"`
	MessageID   uint64 `json:"messageId"`
	// ReplyToID 引用消息 id；Mentions 被 @ 的用户 id（均为可选）
	ReplyToID *uint64 `json:"replyToId"`
	Mentions  []uint  `json:"mentions"`
	// Forwarded 标记这是一条转发消息（只影响展示，不改变投递逻辑）
	Forwarded bool `json:"forwarded"`
	// Emoji / Action 用于表情回应：action=set|remove
	Emoji  string `json:"emoji"`
	Action string `json:"action"`
	// 通话信令（§3.3.1）：callId 一路通话 ID；toUserId / mediaType 用于 invite；
	// toSid 为精确投递的目标设备；sdp / candidate 为 WebRTC 信令载荷。
	CallID    string `json:"callId"`
	ToUserID  uint   `json:"toUserId"`
	ToSid     string `json:"toSid"`
	MediaType string `json:"mediaType"`
	SDP       string `json:"sdp"`
	Candidate string `json:"candidate"`
}

// Dispatch 上行帧路由：先复核连接状态，再按类型分发。
//
// 修复缺陷：此前只有 chat 帧做封禁 / token_version 复核，被封禁或强制下线的
// 用户仍可用 typing/read/recall/edit/reaction/delivered 等帧继续写库与广播。
// 现在所有上行帧（含 ping）都先走连接级复核，失败即回 error 帧并断开。
//
// 修复缺陷（F2）：除 chat 外的上行帧此前零限流，认证用户单连接即可高频驱动
// 查库与全员广播。typing/read/recall/edit/reaction/delivered 六类帧、通话信令、
// ping 与未知类型帧都先过每连接令牌桶再分发（后两者虽无库操作，但会回写帧，
// 同样可被脚本放大）。
func (h *Hub) Dispatch(c *Client, data []byte) {
	var f clientFrame
	if err := json.Unmarshal(data, &f); err != nil {
		h.sendError(c, "帧格式错误")
		return
	}
	if !h.checkConnState(c, f.ConvID) {
		return
	}
	switch f.Type {
	case "ping":
		// ping 回帧会触发一次网络写；不设限时客户端可高频刷 ping 放大写压力。
		// 心跳 25s 一次，20/s 的桶不会误伤；超限直接丢弃，不回 error。
		if !c.allowCtrlFrame() {
			return
		}
		c.trySend(mustJSON(ginH{"type": "pong"}))
	case "chat":
		h.handleChat(c, &f)
	case "typing", "read", "recall", "edit", "reaction", "delivered":
		if !c.allowCtrlFrame() {
			// typing/read/delivered 是尽力而为的信号（下一条自然帧会补上），
			// 静默丢弃，避免错误帧在前端刷 toast；recall/edit/reaction 是
			// 用户显式操作，回 error 帧给出反馈。
			if f.Type == "recall" || f.Type == "edit" || f.Type == "reaction" {
				h.sendError(c, "操作过于频繁，请稍后再试")
			}
			return
		}
		h.dispatchControl(c, &f)
	case "call.invite", "call.accept", "call.reject", "call.offer", "call.answer", "call.ice", "call.hangup":
		// 通话信令同走控制帧令牌桶（20/s + burst 40，trickle ICE 足够）：
		// 被限流时静默丢弃，下一帧 candidate 会补上，避免刷 toast。
		if !c.allowCtrlFrame() {
			return
		}
		h.dispatchCall(c, &f)
	default:
		// 未知类型每帧回一个 error，同样是可被脚本放大的写压力，纳入同一令牌桶。
		if !c.allowCtrlFrame() {
			return
		}
		h.sendError(c, "未知帧类型")
	}
}

// dispatchControl 已过限流的控制帧路由。
func (h *Hub) dispatchControl(c *Client, f *clientFrame) {
	switch f.Type {
	case "typing":
		h.handleTyping(c, f)
	case "read":
		h.handleRead(c, f)
	case "recall":
		h.handleRecall(c, f)
	case "edit":
		h.handleEdit(c, f)
	case "reaction":
		h.handleReaction(c, f)
	case "delivered":
		h.handleDelivered(c, f)
	}
}

// handleChat 发消息链路（§9.1）。
func (h *Hub) handleChat(c *Client, f *clientFrame) {
	if h.deps == nil {
		return
	}
	// 限流：每用户 20 条/分
	if !h.deps.MsgLimiter.Allow(ukey(c.UserID)) {
		slog.Warn("ws message rate limited", "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "操作过于频繁，请稍后再试", f.ClientMsgID)
		return
	}
	// 成员行一次取足：成员校验、禁言检查与全员禁言的角色判定都复用它。
	// 此前 requireMember + 下方各查一次 MemberOf，一条消息重复两次成员查询。
	member, ok := h.memberForFrame(c, f.ConvID, f.ClientMsgID)
	if !ok {
		return
	}
	// 禁言：到时间自动失效；命中时直接拒绝发送
	if h.memberMuted(member) {
		h.sendError(c, "你已被禁言，暂时不能发送消息", f.ClientMsgID)
		return
	}
	// 全员禁言：群主与管理员不受影响
	conv, err := h.deps.Convs.ByID(f.ConvID)
	if err != nil {
		slog.Error("ws conversation lookup", "err", err.Error(), "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试", f.ClientMsgID)
		return
	}
	if conv != nil && conv.MuteAll {
		if member.Role != "owner" && member.Role != "admin" {
			h.sendError(c, "群已开启全员禁言，只有群主和管理员可以发言", f.ClientMsgID)
			return
		}
	}
	// 单聊拉黑：任一方向拉黑后双方都不能再发送（拉黑即互不打扰）
	if conv != nil && conv.Type == 1 && h.deps.Moderation != nil {
		peerID, pErr := h.deps.Convs.OtherMemberID(f.ConvID, c.UserID)
		if pErr != nil {
			slog.Error("ws single peer lookup", "err", pErr.Error(), "conv", f.ConvID)
			h.sendError(c, "服务器开小差了，请稍后再试", f.ClientMsgID)
			return
		}
		if peerID > 0 {
			if blocked, bErr := h.deps.Moderation.BlockedEither(c.UserID, peerID); bErr != nil {
				slog.Error("ws block lookup", "err", bErr.Error())
				h.sendError(c, "服务器开小差了，请稍后再试", f.ClientMsgID)
				return
			} else if blocked {
				h.sendError(c, "你们已互相拉黑，无法发送消息", f.ClientMsgID)
				return
			}
		}
	}
	// 消息长度上限与敏感词可在后台调整，默认 1000 / 无敏感词
	ops := store.DefaultOps()
	if h.deps.Settings != nil {
		ops = h.deps.Settings.Ops()
	}
	maxLen := ops.MaxMessageLength
	if f.MsgType == "chatcard" {
		maxLen = maxCardContentChars
	}
	content := []rune(f.Content)
	if len(content) < 1 || len(content) > maxLen {
		h.sendError(c, fmt.Sprintf("消息需为 1-%d 个字符", maxLen), f.ClientMsgID)
		return
	}
	if f.MsgType == "chatcard" && len(f.Content) > maxCardContentBytes {
		h.sendError(c, "聊天记录内容过大，请减少选中消息", f.ClientMsgID)
		return
	}
	if f.ClientMsgID == "" || len(f.ClientMsgID) > 36 {
		h.sendError(c, "clientMsgId 不合法", f.ClientMsgID)
		return
	}
	msgType := f.MsgType
	if msgType == "" {
		msgType = "text"
	}
	if l := len(msgType); l > 32 || !isSafeMsgType(msgType) {
		h.sendError(c, "msgType 不合法", f.ClientMsgID)
		return
	}
	if msgType == "text" && hitBlockedWord(ops.BlockedWords, string(content)) {
		h.sendError(c, "消息包含敏感词，请修改后重发", f.ClientMsgID)
		return
	}
	// 由存储实现自己判断 URL 归属（本地是 /uploads/，S3 是配置的对外域名），
	// 这样换存储不用改校验逻辑。测试里未注入 Storage 时退回本地前缀判断。
	ownsMedia := func(s string) bool {
		if h.deps.Storage != nil {
			return h.deps.Storage.Owns(s)
		}
		return strings.HasPrefix(s, "/uploads/") && !strings.Contains(s, "..")
	}
	// 卡片的 content 是 JSON 快照：敏感词藏在 JSON 字段里，普通文本分支拦不到；
	// 快照里的媒体 URL 也必须校验归属，否则可借卡片加载外部资源。
	if msgType == "chatcard" && !validChatCard(string(content), ops.BlockedWords, ownsMedia) {
		h.sendError(c, "聊天记录内容不合法，请减少选中消息后重试", f.ClientMsgID)
		return
	}
	// 媒体消息（图片/语音/表情/视频）的 content 必须是本站上传目录下的地址。
	// 不校验的话用户可以塞 https://evil.com/x.mp4，接收方客户端会去加载外部资源
	// （泄露 IP、可被打点统计），而消息看起来完全正常。
	if msgType == "image" || msgType == "audio" || msgType == "sticker" || msgType == "video" {
		if !ownsMedia(string(content)) {
			h.sendError(c, "媒体消息的内容必须是本站上传地址", f.ClientMsgID)
			return
		}
	}

	// 幂等：同 client_msg_id 直接回放原消息
	if existing, err := h.deps.Messages.ByClientMsg(f.ConvID, f.ClientMsgID); err != nil {
		slog.Error("ws idempotent lookup", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试", f.ClientMsgID)
		return
	} else if existing != nil {
		// 回放也要带上引用摘要：断线重发时前端会用这一帧替换本地乐观消息，
		// 缺了 replyTo 引用块就会凭空消失
		one := []model.Message{*existing}
		if aErr := h.deps.Messages.AttachReply(one); aErr == nil {
			existing.ReplyTo = one[0].ReplyTo
		}
		c.trySend(mustJSON(messageFrame(existing, f.ClientMsgID)))
		return
	}

	// 成员列表：@ 校验与收件箱冗余都要用，只取一次
	memberIDs, err := h.MemberUserIDsCached(f.ConvID)
	if err != nil {
		slog.Error("ws members", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试", f.ClientMsgID)
		return
	}

	// 引用必须指向同一条会话里的消息：
	// 不校验的话可以引用别的会话的消息 id，把那个会话的内容透出来。
	var replyTo *uint64
	if f.ReplyToID != nil && *f.ReplyToID > 0 {
		target, tErr := h.deps.Messages.ByID(uint(*f.ReplyToID))
		if tErr != nil || target == nil || target.ConversationID != f.ConvID {
			h.sendError(c, "引用的消息不存在", f.ClientMsgID)
			return
		}
		replyTo = f.ReplyToID
	}

	// @ 只保留「确实在群里」的 id，并去掉自己
	mentions := resolveMentions(memberIDs, c.UserID, f.Mentions)

	msg := &model.Message{
		ConversationID: f.ConvID,
		SenderID:       c.UserID,
		ClientMsgID:    f.ClientMsgID,
		MsgType:        msgType,
		Content:        string(content),
		ReplyToID:      replyTo,
		Mentions:       mentions,
		Forwarded:      f.Forwarded,
		CreatedAt:      time.Now(),
	}
	if err := h.deps.Messages.CreateWithInbox(msg, memberIDs); err != nil {
		slog.Error("ws create message", "err", err.Error(), "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "发送失败，请稍后重试", f.ClientMsgID)
		return
	}
	// 广播帧要带引用摘要：单条装配一次（复用批量实现，避免再写一套查询）
	one := []model.Message{*msg}
	if aErr := h.deps.Messages.AttachReply(one); aErr == nil {
		msg.ReplyTo = one[0].ReplyTo
	}

	// 消息正文不入日志（隐私），只记元数据
	slog.Debug("message stored",
		"msg", msg.ID, "conv", msg.ConversationID, "from", msg.SenderID,
		"type", msg.MsgType, "chars", len([]rune(msg.Content)))

	// 收件箱 last、未读 +1、@ 标记已与消息本体在同一事务内提交；
	// 之前在这里分开写且失败只记日志，会出现消息已入库、列表/未读没更新
	// 甚至事务失败后仍然广播的永久不一致。

	// fan-out：message 帧；非发送者额外 conv_change
	mf := messageFrame(msg, f.ClientMsgID)
	others := withoutID(memberIDs, c.UserID)
	h.ToUsers(memberIDs, mf)
	h.ToUsers(others, ginH{"type": "conv_change", "convId": f.ConvID})
	// 离线用户补一条 Web Push（在线用户已经收到桌面通知，不重复打扰）
	h.notifyPush(others, msg)
	// 服务端插件事件：消息成功落库并广播后异步通知订阅端点。
	// 失败由 runtime 内部只记日志，绝不影响消息主流程。
	if h.deps.Events != nil {
		h.deps.Events.Emit("message:new", ginH{
			"id":        msg.ID,
			"convId":    msg.ConversationID,
			"from":      msg.SenderID,
			"msgType":   msg.MsgType,
			"content":   msg.Content,
			"createdAt": msg.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
}

// notifyPush 给离线成员推送浏览器通知。失败只记日志，不影响消息投递。
func (h *Hub) notifyPush(userIDs []uint, msg *model.Message) {
	if h.deps == nil || h.deps.Push == nil || !h.deps.Push.Enabled() || h.deps.PushSubs == nil {
		return
	}
	body := pushPreview(msg)
	payload := ginH{"title": "ModernChat", "body": body, "convId": msg.ConversationID, "url": "/chat"}
	offline := make([]uint, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid == 0 || h.IsOnline(uid) {
			continue
		}
		offline = append(offline, uid)
	}
	if len(offline) == 0 {
		return
	}
	// 一次 IN 查询取全部离线用户的订阅，替代「每离线用户一个 goroutine + 一次单查」：
	// 大群多为离线时一条消息就是 N 次 RTT 与 N 个 goroutine，与消息速率相乘。
	// goroutine 只为真正有订阅的用户创建（多数用户没有，数量骤减）；
	// 发送并发仍由 service/push 内的信号量约束。
	subs, err := h.deps.PushSubs.ForUsers(offline)
	if err != nil {
		slog.Warn("push subscriptions lookup failed", "count", len(offline), "err", err.Error())
		return
	}
	byUser := make(map[uint][]model.PushSubscription)
	for i := range subs {
		byUser[subs[i].UserID] = append(byUser[subs[i].UserID], subs[i])
	}
	for _, list := range byUser {
		go func(subs []model.PushSubscription) {
			for i := range subs {
				if err := h.deps.Push.Send(&subs[i], payload); err != nil {
					if errors.Is(err, service.ErrPushGone) {
						_ = h.deps.PushSubs.Remove(subs[i].Endpoint)
					} else {
						slog.Warn("push send failed", "uid", subs[i].UserID, "err", err.Error())
					}
				}
			}
		}(list)
	}
}

func pushPreview(m *model.Message) string {
	switch m.MsgType {
	case "image":
		return "[图片]"
	case "audio":
		return "[语音]"
	case "sticker":
		return "[表情]"
	case "video":
		return "[视频]"
	case "chatcard":
		return "[聊天记录]"
	}
	s := m.Content
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60]) + "…"
	}
	return s
}

// handleTyping 转发正在输入（不落库）。
func (h *Hub) handleTyping(c *Client, f *clientFrame) {
	if !h.requireMember(c, f.ConvID) {
		return
	}
	ids, err := h.MemberUserIDsCached(f.ConvID)
	if err != nil {
		return
	}
	h.ToUsers(withoutID(ids, c.UserID), ginH{"type": "typing", "convId": f.ConvID, "userId": c.UserID})
}

// handleRead 已读上报。
//
// 修复缺陷：此前只校验了 upToId != 0 与成员身份，未校验该消息是否属于本会话，
// 客户端上报 9e18 之类的伪造 ID 即可把 last_read_message_id 永久推到未来，
// 从而废掉撤回时的未读回退（RecallUnreadAdjust 以 last_read_message_id < msgID
// 为条件），并伪造「已读到最新」。现在落库前先按 id 反查消息，要求存在且
// ConversationID 与上报的 convId 一致，否则回 error 帧并丢弃。
func (h *Hub) handleRead(c *Client, f *clientFrame) {
	if f.UpToID == 0 || !h.requireMember(c, f.ConvID) {
		return
	}
	// upToId 归属校验：必须真实存在且属于本会话（防止伪造超大 ID 污染已读游标）。
	if h.deps == nil || h.deps.Messages == nil {
		return
	}
	// 归属校验只需要定位列，用 ByIDMeta 避开正文读放大（read 逐消息逐读者触发）
	msg, err := h.deps.Messages.ByIDMeta(uint(f.UpToID))
	if err != nil {
		slog.Error("ws read lookup", "err", err.Error(), "uid", c.UserID, "conv", f.ConvID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	if msg == nil || msg.ConversationID != f.ConvID {
		slog.Warn("ws read rejected: upToId not in conversation",
			"uid", c.UserID, "conv", f.ConvID, "upToId", f.UpToID)
		h.sendError(c, "已读位置不合法")
		return
	}
	affected, err := h.deps.Messages.MarkRead(c.UserID, f.ConvID, f.UpToID)
	if err != nil {
		slog.Error("ws mark read", "err", err.Error())
		return
	}
	if affected == 0 {
		return
	}
	ids, err := h.MemberUserIDsCached(f.ConvID)
	if err != nil {
		return
	}
	// 广播给包括自己在内的所有端：同账号其他设备也要把未读清掉
	h.ToUsers(ids, ginH{
		"type": "read", "convId": f.ConvID, "userId": c.UserID, "upToId": f.UpToID,
	})
}

// handleRecall 撤回：本人 + 2 分钟内；未读接收者回退。
func (h *Hub) handleRecall(c *Client, f *clientFrame) {
	if f.MessageID == 0 {
		return
	}
	msg, err := h.deps.Messages.ByIDMeta(uint(f.MessageID))
	if err != nil || msg == nil || msg.SenderID != c.UserID {
		return
	}
	// 撤回时限可配（0 表示禁止撤回）
	recallMinutes := store.DefaultOps().RecallMinutes
	if h.deps.Settings != nil {
		recallMinutes = h.deps.Settings.Ops().RecallMinutes
	}
	if recallMinutes <= 0 {
		h.sendError(c, "本站已关闭消息撤回")
		return
	}
	if time.Since(msg.CreatedAt) > time.Duration(recallMinutes)*time.Minute {
		h.sendError(c, fmt.Sprintf("超过 %d 分钟，无法撤回", recallMinutes))
		return
	}
	// 幂等：已经撤回过的直接返回，既不重复扣未读、也不重复广播
	changed, recalledAt, err := h.deps.Messages.MarkRecalled(msg.ID)
	if err != nil {
		slog.Error("ws recall", "err", err.Error())
		return
	}
	if !changed {
		return
	}
	if err := h.deps.Messages.RecallUnreadAdjust(msg.ConversationID, c.UserID, uint64(msg.ID)); err != nil {
		slog.Error("ws recall unread adjust", "err", err.Error())
	}
	slog.Info("message recalled", "msg", msg.ID, "conv", msg.ConversationID, "by", c.UserID)
	ids, err := h.MemberUserIDsCached(msg.ConversationID)
	if err != nil {
		return
	}
	// 广播带上服务端撤回时间：所有端用同一个时间点计算「提示显示满 3 分钟」
	h.ToUsers(ids, ginH{
		"type":       "recall",
		"messageId":  msg.ID,
		"convId":     msg.ConversationID,
		"recalledAt": recalledAt.UTC().Format(time.RFC3339Nano),
	})
}

// handleDelivered 单聊送达回执：接收方客户端确认已收到，转发给发送者用于展示「已送达」。
func (h *Hub) handleDelivered(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByIDMeta(uint(f.MessageID))
	if err != nil || msg == nil || msg.SenderID == c.UserID {
		return
	}
	if !h.requireMember(c, msg.ConversationID) {
		return
	}
	conv, err := h.deps.Convs.ByID(msg.ConversationID)
	if err != nil || conv == nil || conv.Type != 1 {
		return
	}
	h.ToUsers([]uint{msg.SenderID}, ginH{
		"type": "message_delivered", "messageId": msg.ID, "convId": msg.ConversationID,
		"userId": c.UserID,
	})
}

// handleEdit 编辑自己的文本消息：5 分钟内、未撤回、仅文本。
func (h *Hub) handleEdit(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	// 编辑只校验归属 / 类型 / 时间，不读正文：ByIDMeta 不拉最多 5000 字的
	// content（edit 是逐条人工操作，仍省一次大字段传输与序列化）。
	msg, err := h.deps.Messages.ByIDMeta(uint(f.MessageID))
	if err != nil {
		slog.Error("ws edit lookup", "err", err.Error(), "uid", c.UserID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	if msg == nil || msg.SenderID != c.UserID {
		h.sendError(c, "只能编辑自己的消息")
		return
	}
	if msg.IsRecalled {
		h.sendError(c, "已撤回的消息不能编辑")
		return
	}
	if msg.MsgType != "text" {
		h.sendError(c, "只有文本消息可以编辑")
		return
	}
	if time.Since(msg.CreatedAt) > EditWindow {
		h.sendError(c, "超过 5 分钟，无法编辑")
		return
	}
	// 成员行一次取足：此前 requireMember + 下方 MemberOf 各查一次，重复两次。
	member, ok := h.memberForFrame(c, msg.ConversationID)
	if !ok {
		return
	}
	if h.memberMuted(member) {
		h.sendError(c, "你已被禁言，暂时不能编辑消息")
		return
	}
	ops := store.DefaultOps()
	if h.deps.Settings != nil {
		ops = h.deps.Settings.Ops()
	}
	maxLen := ops.MaxMessageLength
	content := []rune(f.Content)
	if len(content) < 1 || len(content) > maxLen {
		h.sendError(c, fmt.Sprintf("消息需为 1-%d 个字符", maxLen))
		return
	}
	if hitBlockedWord(ops.BlockedWords, string(content)) {
		h.sendError(c, "消息包含敏感词，请修改后重发")
		return
	}
	now, err := h.deps.Messages.UpdateContent(msg.ID, string(content))
	if err != nil {
		slog.Error("ws edit update", "err", err.Error(), "msg", msg.ID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	ids, err := h.MemberUserIDsCached(msg.ConversationID)
	if err != nil {
		return
	}
	h.ToUsers(ids, ginH{
		"type":     "message_update",
		"id":       msg.ID,
		"convId":   msg.ConversationID,
		"content":  string(content),
		"editedAt": now.UTC().Format(time.RFC3339Nano),
	})
	slog.Debug("message edited", "msg", msg.ID, "conv", msg.ConversationID, "uid", c.UserID)
}

// handleReaction 表情回应：每人每条消息最多一个表情；action=set|remove。
func (h *Hub) handleReaction(c *Client, f *clientFrame) {
	if f.MessageID == 0 || h.deps == nil || h.deps.Messages == nil {
		return
	}
	msg, err := h.deps.Messages.ByIDMeta(uint(f.MessageID))
	if err != nil || msg == nil || msg.IsRecalled {
		return
	}
	if !h.requireMember(c, msg.ConversationID) {
		return
	}
	emoji := strings.TrimSpace(f.Emoji)
	if n := len([]rune(emoji)); n < 1 || n > 4 {
		h.sendError(c, "表情不合法")
		return
	}
	ids, err := h.MemberUserIDsCached(msg.ConversationID)
	if err != nil {
		return
	}
	if f.Action == "remove" {
		if err := h.deps.Messages.RemoveReaction(msg.ID, c.UserID); err != nil {
			slog.Error("ws reaction remove", "err", err.Error())
			return
		}
		h.ToUsers(ids, ginH{
			"type": "reaction", "messageId": msg.ID, "convId": msg.ConversationID,
			"emoji": emoji, "userId": c.UserID, "action": "remove",
		})
		return
	}
	before, err := h.deps.Messages.SetReaction(msg.ID, c.UserID, emoji)
	if err != nil {
		slog.Error("ws reaction set", "err", err.Error())
		h.sendError(c, "服务器开小差了，请稍后再试")
		return
	}
	// 已经就是这个表情：幂等，不重复广播
	if before == emoji {
		return
	}
	h.ToUsers(ids, ginH{
		"type": "reaction", "messageId": msg.ID, "convId": msg.ConversationID,
		"emoji": emoji, "before": before, "userId": c.UserID, "action": "set",
	})
}

// checkConnState 连接级封禁 / token_version / 会话撤销复核（每个上行帧调用）。
//
// 修复缺陷：IsBanned / TokenVersion 此前只在 serve.go 握手段校验，连接建立后
// admin 封禁或重置密码（tv+1）对既有连接无效 —— 忽略 kicked 帧即可继续发消息、
// 写库、fan-out。本函数在每次上行帧分发前复核一次：
//   - 用户已封禁 → error 帧 + 断开；
//   - 库中 tv 与握手缓存 tv 不一致 → error 帧 + 断开（密码被重置 / 强制重登）；
//   - 会话表存在时，sid 已撤销（revoked_at 非空）→ error 帧 + 断开。
//
// 会话撤销此前只靠一次性的 KickSession 广播，广播丢失（多实例未配 Redis、
// pub/sub 故障）或与握手竞态漏踢时连接会永久有效；这里补上连接级兜底。
//
// deps.Users / sessions 都可能为 nil（NewHub(nil) 单测 / 未注入），此时跳过对应检查。
//
// 修复缺陷（F2 伴随优化）：本函数对每个上行帧都会执行，typing 等高频帧每帧
// 一次 Users.ByID 是可观的固定 DB 开销。成功复核结果在 connStateTTL 内复用，
// 失败路径（封禁 / tv 变更 / 用户已删除 / 会话撤销）不缓存且都会断开连接，不影响时效。
func (h *Hub) checkConnState(c *Client, convID uint) bool {
	if h.deps == nil && h.sessions.Load() == nil {
		return true
	}
	if time.Since(time.Unix(0, c.connStateAt.Load())) < connStateTTL {
		return true
	}
	reason, err := h.loadConnState(c)
	if err != nil {
		slog.Error("ws conn state lookup", "err", err.Error(), "uid", c.UserID)
		h.sendError(c, "服务器开小差了，请稍后再试")
		return false
	}
	if reason != "" {
		slog.Warn("ws frame rejected: conn state changed",
			"uid", c.UserID, "conv", convID, "sid", c.sid, "reason", reason)
		h.denyConn(c, convID, reason)
		return false
	}
	// 全部通过才缓存复核时刻；失败分支都要断连，无需缓存。
	c.connStateAt.Store(time.Now().UnixNano())
	return true
}

// loadConnState 查询并判断连接是否仍有效：reason 为空表示通过；
// err 非 nil 表示查询失败、本次无法判定（调用方不应据此断连）。
// 只读不发送，因此可安全地在分片锁外 / 定时巡检里调用。
func (h *Hub) loadConnState(c *Client) (reason string, err error) {
	if h.deps != nil && h.deps.Users != nil {
		// 连接状态复核只判封禁 / token 版本，用 Basic 查询避开 avatar 读放大
		u, uErr := h.deps.Users.ByIDBasic(c.UserID)
		if uErr != nil {
			return "", uErr
		}
		if u == nil {
			// 账号已被删除：旧连接必须立即失效
			return "登录已失效，请重新登录", nil
		}
		if u.IsBanned() {
			return "账号已被封禁", nil
		}
		if u.TokenVersion != c.tv {
			return "登录已失效，请重新登录", nil
		}
	}
	if sessions := h.sessions.Load(); sessions != nil && c.sid != "" {
		sess, sErr := sessions.ActiveBySID(c.sid)
		if sErr != nil {
			return "", sErr
		}
		if sess == nil {
			return "该设备已退出登录，请重新登录", nil
		}
	}
	return "", nil
}

// denyConn 回 error 帧并强制断开该连接（连接级复核失败时使用）。
// requestKick 幂等；这里不 close(c.send)，所有权仍属 Unregister。
func (h *Hub) denyConn(c *Client, convID uint, reason string) {
	h.sendError(c, reason)
	// 先冲刷队列把 error 帧送达，再关闭连接（与 KickUser 语义一致）。
	c.requestKick(true)
}

// memberMuted 该成员行是否处于禁言期。只做纯判定（成员行由调用方取好），
// 供 chat / edit 等上行路径复用同一行，避免同一帧重复查 MemberOf。
func (h *Hub) memberMuted(m *model.ConversationMember) bool {
	return m != nil && m.MuteUntil != nil && m.MuteUntil.After(time.Now())
}

// memberForFrame 上行帧的「取成员行 + 校验」二合一：只需要成员判空时用
// requireMember，需要成员行做禁言 / 角色判定时用本函数，避免同一帧查两次 MemberOf。
// clientMsgID 可选：chat 发送被拒时回传被拒帧的 clientMsgId（F33）。
func (h *Hub) memberForFrame(c *Client, convID uint, clientMsgID ...string) (*model.ConversationMember, bool) {
	if convID == 0 || h.deps == nil || h.deps.Convs == nil {
		h.sendError(c, "无权访问该会话", clientMsgID...)
		return nil, false
	}
	m, err := h.deps.Convs.MemberOf(convID, c.UserID)
	if err != nil || m == nil {
		// 非成员上行帧可能来自被踢后的残留客户端，也可能是越权探测：记 info 便于排查
		slog.Info("ws frame rejected: not a member", "uid", c.UserID, "conv", convID, "err", errStr(err))
		h.sendError(c, "无权访问该会话", clientMsgID...)
		return nil, false
	}
	return m, true
}

// requireMember 上行帧的会话成员校验（只要判空、不复用成员行时用）。
func (h *Hub) requireMember(c *Client, convID uint, clientMsgID ...string) bool {
	_, ok := h.memberForFrame(c, convID, clientMsgID...)
	return ok
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sendError 回 error 帧。clientMsgID 可选（F33）：chat 发送被拒时原样回传被拒
// 帧的 clientMsgId（camelCase，与 message 帧一致），前端据此把对应的乐观消息
// 立即置为失败，而不用等 8 秒超时兜底；其余帧不携带该字段。
func (h *Hub) sendError(c *Client, reason string, clientMsgID ...string) {
	frame := ginH{"type": "error", "reason": reason}
	if len(clientMsgID) > 0 && clientMsgID[0] != "" {
		frame["clientMsgId"] = clientMsgID[0]
	}
	c.trySend(mustJSON(frame))
}

// resolveMentions 过滤 @ 目标：只留会话成员、去重、排除发送者，上限 20 个。
//
// 不校验的话客户端可以 @ 任意 user id，把「有人@我」的提示推给无关的人。
func resolveMentions(memberIDs []uint, senderID uint, want []uint) []uint {
	if len(want) == 0 {
		return nil
	}
	allowed := make(map[uint]struct{}, len(memberIDs))
	for _, id := range memberIDs {
		allowed[id] = struct{}{}
	}
	out := make([]uint, 0, len(want))
	seen := make(map[uint]struct{}, len(want))
	for _, id := range want {
		if id == 0 || id == senderID {
			continue
		}
		if _, ok := allowed[id]; !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

func messageFrame(m *model.Message, clientMsgID string) ginH {
	frame := ginH{
		"type":      "message",
		"id":        m.ID,
		"convId":    m.ConversationID,
		"from":      ginH{"id": m.SenderID},
		"content":   m.Content,
		"msgType":   m.MsgType,
		"createdAt": m.CreatedAt,
	}
	if clientMsgID != "" {
		frame["clientMsgId"] = clientMsgID
	}
	// 引用摘要随帧下发：接收方不用再回查，直接能渲染引用块
	if m.ReplyTo != nil {
		frame["replyTo"] = m.ReplyTo
	}
	if m.ReplyToID != nil {
		frame["replyToId"] = *m.ReplyToID
	}
	if len(m.Mentions) > 0 {
		frame["mentions"] = m.Mentions
	}
	if m.Forwarded {
		frame["forwarded"] = true
	}
	return frame
}

// hitBlockedWord 文本是否命中后台配置的敏感词（子串匹配，大小写敏感）。
func hitBlockedWord(words []string, content string) bool {
	for _, w := range words {
		if w != "" && strings.Contains(content, w) {
			return true
		}
	}
	return false
}

// 合并转发卡片的落库快照（字段与前端 chat/mergeCard.ts 一一对应）。
// 用短键名是为了在 64KB 帧预算里塞下更多条目。
type chatCardPayload struct {
	V     int    `json:"v"`
	Title string `json:"title"`
	Items []struct {
		N string `json:"n"`
		T string `json:"t"`
		M string `json:"m"`
		C string `json:"c"`
	} `json:"items"`
}

// validChatCard 校验卡片快照：结构合法、条数不超上限、敏感词与媒体归属都过一遍。
// 解析失败一律拒绝——畸形 JSON 会让接收端渲染出空白卡片，不如当场报错。
func validChatCard(raw string, words []string, ownsMedia func(string) bool) bool {
	var p chatCardPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return false
	}
	if p.V != 1 || len(p.Items) == 0 || len(p.Items) > maxCardItems {
		return false
	}
	if hitBlockedWord(words, p.Title) {
		return false
	}
	for _, it := range p.Items {
		if hitBlockedWord(words, it.N) || hitBlockedWord(words, it.C) {
			return false
		}
		if utf8.RuneCountInString(it.C) > 1000 {
			return false
		}
		// 快照里的媒体若是外链，接收端会去加载——与媒体消息同样的 SSRF/打点面
		if it.M == "image" || it.M == "audio" || it.M == "sticker" || it.M == "video" {
			if !ownsMedia(it.C) {
				return false
			}
		}
	}
	return true
}

func isSafeMsgType(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		ok := b == '-' || b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z')
		if !ok {
			return false
		}
	}
	return utf8.RuneCountInString(s) > 0
}

func msgIDPtr(id uint64) *uint64 { return &id }

func withoutID(ids []uint, exclude uint) []uint {
	out := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id != exclude {
			out = append(out, id)
		}
	}
	return out
}

func ukey(uid uint) string {
	// 稳定字符串 key（避免 rune 转换歧义）
	const digits = "0123456789"
	if uid == 0 {
		return "u0"
	}
	buf := [20]byte{}
	i := len(buf)
	for uid > 0 {
		i--
		buf[i] = digits[uid%10]
		uid /= 10
	}
	return "u" + string(buf[i:])
}
