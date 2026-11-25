package router

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/serverplugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
	"github.com/modern-day-team/modern-chat-new/server/internal/ws"
)

// WireServerPluginHost 为服务端插件运行时注入宿主业务能力。
//
// 安全边界（所有回调共同遵守）：
//   - 只接受「当前 WASM 用户」身份；公开路由无从注入，userID=0 一律拒绝；
//   - 会话列表只列本人的，成员列表先校验本人是成员，绝不越权读他人会话；
//   - 发消息必须本人是会话成员、未被禁言、群未全员禁言，且不能伪造系统身份；
//   - 推送只能发给本人，不给插件指定任意 userId 的能力；
//   - 敏感词/媒体归属校验与 WS 主链路一致。
//
// rt / 各 store 允许为 nil：对应能力注入空回调，mc_call 会返回明确「未配置」错误。
func WireServerPluginHost(
	rt *serverplugin.Runtime,
	users *store.UserStore,
	convs *store.ConversationStore,
	msgs *store.MessageStore,
	hub *ws.Hub,
	settings *store.SettingStore,
	storage service.Storage,
	pushSubs *store.PushStore,
	push *service.PushService,
	msgLimiter ...middleware.Limiter,
) {
	if rt == nil {
		return
	}
	// 插件发消息与 WS 主链路共用同一限流后端（20 条/分/用户），多实例下走
	// Redis 共享配额，避免插件成为绕过限流的群发通道。未注入时内存兜底。
	hostLimit := middleware.Limiter(middleware.NewLimiterStore(20, 20))
	if len(msgLimiter) > 0 && msgLimiter[0] != nil {
		hostLimit = msgLimiter[0]
	}
	host := serverplugin.HostServices{}

	// notification.push：只推给当前用户；未配置 VAPID / 订阅表时注入 nil。
	var pusher func(uint, string, string) error
	if push != nil && push.Enabled() && pushSubs != nil {
		pusher = func(userID uint, title, body string) error {
			if userID == 0 {
				return fmt.Errorf("推送目标用户不合法")
			}
			subs, err := pushSubs.ForUser(userID)
			if err != nil {
				return err
			}
			payload := map[string]any{"title": title, "body": body, "url": "/chat"}
			for i := range subs {
				if err := push.Send(&subs[i], payload); err != nil {
					if errors.Is(err, service.ErrPushGone) {
						_ = pushSubs.Remove(subs[i].Endpoint)
						continue
					}
					slog.Warn("server plugin push failed", "uid", userID, "err", err.Error())
				}
			}
			return nil
		}
	}
	host.NotificationPusher = pusher

	// message.send：以当前用户身份落库并广播，复用收件箱事务。
	if convs != nil && msgs != nil {
		host.MessageSender = func(from, convID uint, content, msgType string, payload json.RawMessage) error {
			if from == 0 || convID == 0 {
				return fmt.Errorf("发送者与会话不合法")
			}
			// key 与 ws.ukey 一致（"u"+十进制 uid），确保与 WS 主链路共享同一配额。
			if !hostLimit.Allow("u" + strconv.FormatUint(uint64(from), 10)) {
				return fmt.Errorf("操作过于频繁，请稍后再试")
			}
			// 成员边界：不是会话成员一律拒绝，不能冒充系统 / 他人身份。
			member, err := convs.MemberOf(convID, from)
			if err != nil {
				return err
			}
			if member == nil {
				return fmt.Errorf("你不是该会话成员")
			}
			if member.MuteUntil != nil && member.MuteUntil.After(time.Now()) {
				return fmt.Errorf("你已被禁言，暂时不能发送消息")
			}
			conv, err := convs.ByID(convID)
			if err != nil {
				return err
			}
			if conv == nil {
				return fmt.Errorf("会话不存在")
			}
			if conv.MuteAll && member.Role != "owner" && member.Role != "admin" {
				return fmt.Errorf("群已开启全员禁言，只有群主和管理员可以发言")
			}
			// 媒体消息的 content 必须是本站上传地址，防止借插件加载外部资源。
			switch msgType {
			case "image", "audio", "sticker", "video":
				if storage == nil || !storage.Owns(content) {
					return fmt.Errorf("媒体消息的内容必须是本站上传地址")
				}
			}
			// 敏感词规则与聊天主链路同源（后台可配）。
			ops := store.DefaultOps()
			if settings != nil {
				ops = settings.Ops()
			}
			for _, w := range ops.BlockedWords {
				if w != "" && strings.Contains(content, w) {
					return fmt.Errorf("消息包含敏感词，请修改后重发")
				}
			}

			memberIDs, err := convs.MemberUserIDs(convID)
			if err != nil {
				return err
			}
			// clientMsgId 走服务端幂等键：插件不需要关心，由宿主生成随机值。
			var b [12]byte
			if _, err := rand.Read(b[:]); err != nil {
				return err
			}
			clientMsgID := "sp-" + hex.EncodeToString(b[:])
			msg := &model.Message{
				ConversationID: convID,
				SenderID:       from,
				ClientMsgID:    clientMsgID,
				MsgType:        msgType,
				Content:        content,
				CreatedAt:      time.Now(),
			}
			if err := msgs.CreateWithInbox(msg, memberIDs); err != nil {
				return err
			}
			if hub != nil {
				frame := map[string]any{
					"type":        "message",
					"id":          msg.ID,
					"convId":      msg.ConversationID,
					"from":        map[string]any{"id": msg.SenderID},
					"content":     msg.Content,
					"msgType":     msg.MsgType,
					"createdAt":   msg.CreatedAt,
					"clientMsgId": clientMsgID,
				}
				// payload 是插件自定义结构化数据：只随实时帧下发，不落库，
				// 老客户端忽略该字段，不影响既有 message 帧解析。
				if len(payload) > 0 {
					frame["pluginPayload"] = payload
				}
				hub.ToUsers(memberIDs, frame)
				others := make([]uint, 0, len(memberIDs))
				for _, id := range memberIDs {
					if id != from {
						others = append(others, id)
					}
				}
				hub.ToUsers(others, map[string]any{"type": "conv_change", "convId": convID})
				// 离线成员补一条浏览器通知，与聊天主链路一致；失败只记日志。
				if pusher != nil {
					go func(ids []uint, preview string) {
						for _, id := range ids {
							if id == 0 || hub.IsOnline(id) {
								continue
							}
							if err := pusher(id, "ModernChat", preview); err != nil {
								slog.Warn("server plugin message push failed", "uid", id, "err", err.Error())
							}
						}
					}(others, pushPreview(content, msgType))
				}
			}
			return nil
		}
	}

	// conv.list：只列当前用户自己的会话。
	if convs != nil {
		host.ConvLister = func(userID uint) ([]serverplugin.ConvBrief, error) {
			if userID == 0 {
				return nil, fmt.Errorf("用户不合法")
			}
			rows, err := convs.ListFor(userID)
			if err != nil {
				return nil, err
			}
			out := make([]serverplugin.ConvBrief, 0, len(rows))
			for i := range rows {
				r := &rows[i]
				brief := serverplugin.ConvBrief{
					ID: r.ID, Type: r.Type, Name: r.Name,
					MemberCount: r.MemberCount, Unread: r.UnreadCount,
				}
				if r.Peer != nil {
					brief.PeerNickname = r.Peer.Nickname
				}
				out = append(out, brief)
			}
			return out, nil
		}
		// conv.members：先校验本人是成员，否则拒绝。
		host.ConvMembers = func(userID, convID uint) ([]serverplugin.MemberBrief, error) {
			if userID == 0 || convID == 0 {
				return nil, fmt.Errorf("用户或会话不合法")
			}
			member, err := convs.MemberOf(convID, userID)
			if err != nil {
				return nil, err
			}
			if member == nil {
				return nil, fmt.Errorf("你不是该会话成员")
			}
			rows, err := convs.MembersView(convID)
			if err != nil {
				return nil, err
			}
			out := make([]serverplugin.MemberBrief, 0, len(rows))
			for _, r := range rows {
				out = append(out, serverplugin.MemberBrief{
					UserID: r.UserID, Nickname: r.User.Nickname, Role: r.Role,
				})
			}
			return out, nil
		}
	}

	// user.get / user.search：只下发展示所需的摘要字段。
	if users != nil {
		host.UserLookup = func(userID uint) (*serverplugin.UserBrief, error) {
			if userID == 0 {
				return nil, fmt.Errorf("用户不合法")
			}
			u, err := users.ByID(userID)
			if err != nil || u == nil {
				return nil, err
			}
			return &serverplugin.UserBrief{
				ID: u.ID, Username: u.Username, Nickname: u.Nickname, AvatarColor: u.AvatarColor,
			}, nil
		}
		host.UserSearch = func(kw string, limit int) ([]serverplugin.UserBrief, error) {
			rows, err := users.Search(kw, limit)
			if err != nil {
				return nil, err
			}
			out := make([]serverplugin.UserBrief, 0, len(rows))
			for _, r := range rows {
				out = append(out, serverplugin.UserBrief{
					ID: r.ID, Username: r.Username, Nickname: r.Nickname, AvatarColor: r.AvatarColor,
				})
			}
			return out, nil
		}
	}

	rt.SetHost(host)
}

// pushPreview 与 ws.pushPreview 同口径：媒体消息给占位文案，文本截前 60 字符。
func pushPreview(content, msgType string) string {
	switch msgType {
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
	if r := []rune(content); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return content
}
