package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// SessionHandler 登录会话管理（列表 / 单设备下线）。
type SessionHandler struct {
	sessions *store.SessionStore
}

func NewSessionHandler(sessions *store.SessionStore, _ Notifier) *SessionHandler {
	return &SessionHandler{sessions: sessions}
}

// List 当前账号的活跃会话列表。
func (h *SessionHandler) List(c *gin.Context) {
	me := middleware.UserFrom(c)
	rows, err := h.sessions.List(me.ID)
	if err != nil {
		internal(c, err)
		return
	}
	currentSID, _ := c.Get("sid")
	out := make([]model.SessionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.SessionView{
			ID: row.ID, Platform: row.Platform, ClientType: row.ClientType,
			AppVersion: row.AppVersion, IP: row.IP,
			CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt,
			Current: currentSID != nil && row.SID == currentSID.(string),
		})
	}
	c.JSON(http.StatusOK, gin.H{"sessions": out})
}

// Revoke 撤销单条会话：标记 revoked_at + 踢该 sid 的 WS。
func (h *SessionHandler) Revoke(c *gin.Context) {
	me := middleware.UserFrom(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "会话 ID 无效")
		return
	}
	sid, err := h.sessions.SIDByID(me.ID, uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if sid == "" {
		fail(c, http.StatusNotFound, "会话不存在")
		return
	}
	ok, err := h.sessions.Revoke(me.ID, uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if !ok {
		fail(c, http.StatusNotFound, "会话不存在或已退出")
		return
	}
	notifyFor(c).KickSession(sid, "该设备已被退出登录")
	slog.Info("session revoked", "uid", me.ID, "sessionId", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
