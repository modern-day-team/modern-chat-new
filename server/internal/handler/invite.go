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

// InviteManager 消费方定义的窄接口（由 service.InviteService 实现）。
type InviteManager interface {
	List() ([]model.InviteCode, error)
	Create(maxUses, expiresDays, count int, createdBy uint, creatorNickname string) ([]model.InviteCode, error)
	Disable(id uint, disabled bool) error
}

// InviteHandler 注册邀请码管理（仅站长）：业务规则在 service，本层只做传输。
type InviteHandler struct {
	invites InviteManager
}

func NewInviteHandler(invites InviteManager) *InviteHandler {
	return &InviteHandler{invites: invites}
}

// List 列出邀请码（新→旧）。
func (h *InviteHandler) List(c *gin.Context) {
	rows, err := h.invites.List()
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, rows)
}

// Create 生成一批邀请码。
func (h *InviteHandler) Create(c *gin.Context) {
	var req struct {
		MaxUses     int `json:"maxUses"`
		ExpiresDays int `json:"expiresDays"`
		Count       int `json:"count"`
	}
	if !bind(c, &req) {
		return
	}
	me := middleware.UserFrom(c)
	out, err := h.invites.Create(req.MaxUses, req.ExpiresDays, req.Count, me.ID, me.Nickname)
	switch {
	case errors.Is(err, service.ErrInviteMaxUses), errors.Is(err, service.ErrInviteExpires),
		errors.Is(err, service.ErrInviteCount):
		fail(c, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		internal(c, err)
		return
	}
	slog.Info("invite codes created", "by", me.ID, "count", len(out))
	c.JSON(http.StatusOK, out)
}

// SetDisabled 停用 / 启用一个邀请码（不删除，保留审计线索）。
func (h *InviteHandler) SetDisabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "邀请码 id 不合法")
		return
	}
	var req struct {
		Disabled bool `json:"disabled"`
	}
	if !bind(c, &req) {
		return
	}
	if err := h.invites.Disable(uint(id), req.Disabled); err != nil {
		if errors.Is(err, service.ErrInviteNotFound) {
			fail(c, http.StatusNotFound, err.Error())
			return
		}
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
