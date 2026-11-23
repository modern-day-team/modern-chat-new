package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// ClientPlatform 归一化客户端平台标识；非法值统一归到 "other"，
// 空值回退 "web"（浏览器默认，保持存量行为）。
func ClientPlatform(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "web", "desktop", "mobile", "tablet":
		return strings.ToLower(strings.TrimSpace(s))
	case "":
		return "web"
	default:
		return "other"
	}
}

// SanitizeHeader 客户端自定义头只取可展示的短 ASCII 片段，防日志注入 / 超长存储。
func SanitizeHeader(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 32 {
		s = s[:32]
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ClientInfo 从请求头采集客户端标识（供 WS 握手等非 Auth 路径使用）。
func ClientInfo(c *gin.Context) (platform, clientType, appVersion string) {
	return ClientPlatform(c.GetHeader("X-Client-Platform")),
		SanitizeHeader(c.GetHeader("X-Client-Type")),
		SanitizeHeader(c.GetHeader("X-Client-Version"))
}

// abort401With 带自定义中文消息的 401（如会话已撤销）。
func abort401With(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": msg})
}
