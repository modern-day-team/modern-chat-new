package handler

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/service/qrsession"
)

// ---- 扫码登录（桌面展示二维码，手机端已登录会话确认）----
//
// 流程：
//
//	桌面（未登录）：POST /api/auth/qr/start → {token, expiresAt}
//	               前端把 https://站点/app/qr/<token> 渲染成二维码轮播
//	               POST /api/auth/qr/status（2.5s 轮询）→ pending / confirmed(token,user) / expired
//	手机（已登录）：扫码打开确认页 → POST /api/auth/qr/confirm（需手机端 JWT）→ 桌面下一次轮询拿到登录态
//
// 安全边界：
//   - token 256 位随机，不可枚举；TTL 5 分钟，过期即作废
//   - 登录态一次性消费：confirmed 后首个成功轮询拿走 token，第二个轮询（或网络重试）拿不到
//   - confirm 必须持有效手机端 JWT（走 authed 组中间件：Auth + Ban + Maintenance）
//   - status 用 POST 传 body：token 不进 URL，不落访问日志
//
// 会话态存进程内（单实例部署；重启后未完成登录自然作废，与 ssoTickets 同口径）。

// 兼容旧代码与测试：具体状态机在 service/qrsession，serverplugin 也使用同一实例。
func qrIssueSession() (string, time.Time, error) { return qrsession.Default().Issue() }
func qrConfirmSession(token string, userID uint) bool {
	return qrsession.Default().Confirm(token, userID)
}
func qrTakeSession(token string) (int, uint) { return qrsession.Default().Take(token) }
func qrInvalidateSession(token string)       { qrsession.Default().Invalidate(token) }

// QRStart 桌面端发起扫码登录。
func (h *AuthHandler) QRStart(c *gin.Context) {
	token, expiresAt, err := qrIssueSession()
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token, "expiresAt": expiresAt.UTC().Format(time.RFC3339)})
}

// QRStatus 桌面端轮询扫码结果（专用限流 60/min，POST 传 body 避免票据进访问日志）。
func (h *AuthHandler) QRStatus(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if !bind(c, &req) {
		return
	}
	state, userID := qrTakeSession(strings.TrimSpace(req.Token))
	switch state {
	case 1:
		u, err := h.users.ByID(userID)
		if err != nil {
			internal(c, err)
			return
		}
		if u == nil || u.IsBanned() {
			// 确认人与封禁状态之间有时间差（扫码在先、封禁在后）：作废本次登录
			qrInvalidateSession(strings.TrimSpace(req.Token))
			c.JSON(http.StatusOK, gin.H{"status": "expired"})
			return
		}
		jwtToken, err := h.issueLoginToken(c, u)
		if err != nil {
			internal(c, err)
			return
		}
		slog.Info("qr login", "uid", u.ID, "username", u.Username)
		c.JSON(http.StatusOK, gin.H{"status": "confirmed", "token": jwtToken, "user": u})
	case 2:
		c.JSON(http.StatusOK, gin.H{"status": "expired"})
	default:
		c.JSON(http.StatusOK, gin.H{"status": "pending"})
	}
}

// QRConfirm 手机端确认授权桌面登录（需本机 JWT；维护模式随 authed 组 503）。
func (h *AuthHandler) QRConfirm(c *gin.Context) {
	var req struct {
		Token string `json:"token"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	if me == nil {
		fail(c, http.StatusUnauthorized, "请先在本设备登录")
		return
	}
	if !qrConfirmSession(strings.TrimSpace(req.Token), me.ID) {
		fail(c, http.StatusBadRequest, "二维码无效或已过期，请在桌面端重新生成")
		return
	}
	slog.Info("qr confirm", "uid", me.ID, "username", me.Username)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
