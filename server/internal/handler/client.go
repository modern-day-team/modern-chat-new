package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/version"
)

// ClientVersion 各端启动时探测版本兼容性（免鉴权）。
// minVersion 来自 MIN_CLIENT_VERSION；为空表示不限制。
func ClientVersion(minVersion string) gin.HandlerFunc {
	minVersion = strings.TrimSpace(minVersion)
	return func(c *gin.Context) {
		appVersion := strings.TrimSpace(c.Query("appVersion"))
		required := minVersion != "" && appVersion != "" && compareVersion(appVersion, minVersion) < 0
		resp := gin.H{
			"version":         version.Current().Version,
			"minVersion":      minVersion,
			"upgradeRequired": required,
		}
		if required {
			resp["message"] = "当前客户端版本过低，请升级后再使用"
		}
		c.JSON(http.StatusOK, resp)
	}
}

// compareVersion 点分数字逐段比较：a<b 返回 -1，相等 0，a>b 返回 1。
// 非数字段按 0 处理（如 "0.2.0-beta" 的 beta 段 → 0），避免解析失败误判。
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, as[i]))
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, bs[i]))
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}
