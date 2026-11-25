package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// SiteHandler 公开的站点信息（无需登录）。
//
// 前端用它做三件事：显示站点名、决定要不要露出注册入口、
// 以及把消息长度 / 撤回时限 / 上传体积这些规则同步到输入区。
// 只暴露「能公开」的字段——SMTP、Redis、对象存储密钥一律不在这里。
type SiteHandler struct {
	settings *store.SettingStore
	mailer   *service.Mailer
	users    *store.UserStore
	call     config.CallConfig
}

func NewSiteHandler(settings *store.SettingStore, mailer *service.Mailer, users *store.UserStore, call config.CallConfig) *SiteHandler {
	return &SiteHandler{settings: settings, mailer: mailer, users: users, call: call}
}

// Get GET /api/site
func (h *SiteHandler) Get(c *gin.Context) {
	ops := store.DefaultOps()
	if h.settings != nil {
		ops = h.settings.Ops()
	}
	// 邮箱相关报「实际生效值」，前端据此决定渲染哪些字段：
	//   requireEmail       —— 注册是否必填邮箱（开关开启即必填，首个账号也是）
	//   requireEmailVerify —— 是否还需要验证码（开关 && SMTP 可用 && 已有账号；
	//                         首个账号豁免、未配 SMTP 也不强制，否则会把部署者锁在门外）
	mailerReady := h.mailer != nil && h.mailer.Config().Enabled()
	// 只需要「是否已有账号」这一个布尔值：用 EXISTS/ LIMIT 1，避免每 30 秒
	// 每个在线客户端都触发一次 users 全表 COUNT
	hasUsers := false
	if h.users != nil {
		if ok, err := h.users.HasUsers(); err == nil {
			hasUsers = ok
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"name":               ops.SiteName,
		"allowRegister":      ops.AllowRegister,
		"maintenanceMode":    ops.MaintenanceMode,
		"maintenanceMessage": ops.MaintenanceMessage,
		"requireInviteCode":  ops.RequireInviteCode,
		"requireEmail":       ops.RequireEmailVerify,
		"requireEmailVerify": ops.RequireEmailVerify && mailerReady && hasUsers,
		"maxMessageLength":   ops.MaxMessageLength,
		"recallMinutes":      ops.RecallMinutes,
		"maxGroupMembers":    ops.MaxGroupMembers,
		"maxImageMB":         ops.MaxImageMB,
		"maxAudioMB":         ops.MaxAudioMB,
		"maxFileMB":          ops.MaxFileMB,
		"maxVideoMB":         ops.MaxVideoMB,
		// 只暴露开关，TURN 凭据绝不进公开接口（需登录的 /api/call/config 才下发）
		"call": gin.H{"enabled": true},
	})
}

// CallConfig GET /api/call/config（需登录）。
// TURN 凭据只经此接口下发给登录用户，不写日志、不放公开接口。
func (h *SiteHandler) CallConfig(c *gin.Context) {
	servers := []gin.H{}
	if h.call.STUNServer != "" {
		servers = append(servers, gin.H{"urls": h.call.STUNServer})
	}
	if h.call.TurnEnabled() {
		servers = append(servers, gin.H{
			"urls":       h.call.TURNServer,
			"username":   h.call.TURNUser,
			"credential": h.call.TURNPass,
		})
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true, "iceServers": servers})
}
