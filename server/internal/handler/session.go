package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
)

// SessionManager 消费方定义的窄接口（由 service.SessionService 实现）。
type SessionManager interface {
	List(uid uint, currentSID string) ([]model.SessionView, error)
	Revoke(uid, id uint) (string, error)
}

// SessionHandler 登录会话管理（列表 / 单设备下线）：只做传输层职责，业务规则在 service。
type SessionHandler struct {
	sessions SessionManager
	notifier Notifier
}

func NewSessionHandler(sessions SessionManager, notifier Notifier) *SessionHandler {
	return &SessionHandler{sessions: sessions, notifier: notifier}
}

// List 当前账号的活跃会话列表。
func (h *SessionHandler) List(c *gin.Context) {
	me := middleware.UserFrom(c)
	currentSID, _ := c.Get("sid")
	sid, _ := currentSID.(string)
	rows, err := h.sessions.List(me.ID, sid)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": rows})
}

// Revoke 撤销单条会话：内核完成原子撤销，handler 负责踢 WS 与状态码映射。
func (h *SessionHandler) Revoke(c *gin.Context) {
	me := middleware.UserFrom(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "会话 ID 无效")
		return
	}
	sid, err := h.sessions.Revoke(me.ID, uint(id))
	if errors.Is(err, service.ErrSessionNotFound) {
		fail(c, http.StatusNotFound, "会话不存在或已退出")
		return
	}
	if err != nil {
		internal(c, err)
		return
	}
	// 显式注入的 notifier：不再从 gin context 旁路获取。
	h.notifier.KickSession(sid, "该设备已被退出登录")
	slog.Info("session revoked", "uid", me.ID, "sessionId", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
