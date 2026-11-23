package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨源资源共享：按 FRONTEND_URL 白名单精确放行浏览器来源。
//
// 背景：Web 部署在 /app/、官网在根路径，桌面端 WebView 开发期是 localhost:1420，
// 移动端 WebView 调试又是另一个端口。这些来源调 /api 时浏览器会先发 OPTIONS 预检，
// 不带允许头就整站不可用。原生客户端（无 Origin）不受影响。
//
// 安全边界：
//   - 只回显白名单里的 Origin，绝不用 "*"（本站点带 mc_auth cookie，必须 Allow-Credentials）；
//   - 只有精确命中的来源才写 Allow-Origin / Allow-Credentials，未命中时不写任何头，
//     浏览器自行拦截；与 WS Origin 白名单共用一个配置，口径一致；
//   - 预检请求直接 204，不进入业务路由，避免误触发限流 / 写操作。
func CORS(allowed []string) gin.HandlerFunc {
	origins := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		if o := normalizeOrigin(a); o != "" {
			origins[o] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := origins[normalizeOrigin(origin)]; ok {
				// Vary 必须写：否则代理 / 浏览器可能把带 Allow-Origin 的响应缓存下来
				// 给另一个 Origin 复用，等于白名单失效。
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Headers",
					"Authorization, Content-Type, X-Client-Platform, X-Client-Type, X-Client-Version, X-Migrate-Secret")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Access-Control-Max-Age", "600")
				if c.Request.Method == http.MethodOptions {
					c.Status(http.StatusNoContent)
					c.Abort()
					return
				}
			} else if c.Request.Method == http.MethodOptions {
				// 非白名单来源的预检：直接拒绝，别让请求落到业务路由产生副作用。
				c.Status(http.StatusForbidden)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

// NormalizeOrigin 归一化来源：小写、去首尾空格、去一个尾斜杠。
// CORS 与 WS Origin 是同一安全边界，共用本函数避免两套实现漂移。
func NormalizeOrigin(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "/"))
}

// normalizeOrigin 包内简写（CORS 中间件自身使用）。
func normalizeOrigin(s string) string { return NormalizeOrigin(s) }

// OriginAllowed 判断来源是否在白名单（忽略大小写与尾斜杠）；
// 空 Origin（原生客户端 / curl）由调用方自行决定，本函数只判非空来源。
func OriginAllowed(allowed []string, origin string) bool {
	o := NormalizeOrigin(origin)
	for _, a := range allowed {
		if o == NormalizeOrigin(a) {
			return true
		}
	}
	return false
}
