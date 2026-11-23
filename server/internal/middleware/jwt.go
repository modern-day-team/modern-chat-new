package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"

	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

const TokenTTL = 7 * 24 * time.Hour

// AuthCookieName 与 localStorage 里的 token 是同一个 JWT。
// 浏览器 <img>/<video> 无法附带 Authorization 头，上传文件访问只能靠同源 cookie；
// WebSocket 握手也支持用它替代 query token（避免 token 出现在代理访问日志里）。
const AuthCookieName = "mc_auth"

type userCtxKey struct{}

// UserFrom 从上下文取当前用户（Auth 之后可用）。
func UserFrom(c *gin.Context) *model.User {
	u, _ := c.Get("user")
	if u == nil {
		return nil
	}
	return u.(*model.User)
}

// Claims 解析出的身份。
type Claims struct {
	UID uint
	TV  int64
	// SID 会话 ID（128 位随机）。老 token 无此字段时为空串：
	// 为兼容存量登录态，空 SID 不做会话校验，退化为 tv 单因子。
	SID string
}

// IssueToken 签发 HS256 JWT（claims: uid, tv, sid）。
// sid 在签发时生成，但会话行由 Auth / WS 握手首次见到时 lazy 落库——
// 这样登录链路（密码 / 2FA / 扫码 / SSO / 迁移）7 处签发点不用逐个改。
func IssueToken(uid uint, tv int64, secret string) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": uid,
		"tv":  tv,
		"sid": newSessionID(),
		"iat": now.Unix(),
		"exp": now.Add(TokenTTL).Unix(),
	})
	return t.SignedString([]byte(secret))
}

// newSessionID 生成 128 位随机会话 ID（hex，32 字符）。
func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败是系统级故障；退化为时间+计数也只影响会话粒度，
		// 不降低 JWT 签名强度（签名仍是 HS256）。
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

// ParseToken 校验并解析 JWT（REST 中间件与 WS 握手共用）。
func ParseToken(tokenStr, secret string) (*Claims, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(secret), nil
	})
	if err != nil || !tok.Valid {
		return nil, err
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, jwt.ErrTokenInvalidClaims
	}
	uidF, okU := claims["uid"].(float64)
	tvF, okT := claims["tv"].(float64)
	if !okU || !okT {
		return nil, jwt.ErrTokenInvalidClaims
	}
	sid, _ := claims["sid"].(string) // 老 token 无 sid，容错为空
	return &Claims{UID: uint(uidF), TV: int64(tvF), SID: sid}, nil
}

// Auth 认证中间件：解析 Bearer token、校验 token_version 与会话、装载 user。
// 任何失败一律 401（前端统一跳登录）。
//
// sessions 为可选参数（保持既有调用点兼容）：非 nil 时做会话登记与撤销校验。
// 会话登记是 lazy 的——签发 JWT 的 7 个入口不用逐个改，首次请求自然落库。
func Auth(users *store.UserStore, secret string, sessions ...*store.SessionStore) gin.HandlerFunc {
	var sessionStore *store.SessionStore
	if len(sessions) > 0 {
		sessionStore = sessions[0]
	}
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			abort401(c)
			return
		}
		cl, err := ParseToken(strings.TrimPrefix(h, "Bearer "), secret)
		if err != nil {
			abort401(c)
			return
		}
		// 必须加载完整行：2FA 的 Save(me) 会写回所有列，半缺字段会把头像 /
		// password_hash 写成空值。头像已改为存 Storage URL（短字符串），
		// 不再有此前 256KB data URL 的读放大问题。
		u, err := users.ByID(cl.UID)
		if err != nil {
			slog.Error("auth load user", "err", err)
			abort401(c)
			return
		}
		if u == nil || u.TokenVersion != cl.TV {
			abort401(c)
			return
		}
		// 会话校验：sid 已撤销 → 401（单设备下线立即生效）。
		// 安全收紧：启用会话表后，无 sid 的存量老 token 一律拒绝——
		// 否则单设备撤销只改 revoked_at、不改 tv，老 token 在 7 天 TTL 内
		// 仍可绕过撤销。升级后需重新登录一次（文档已说明）。
		if sessionStore != nil && cl.SID == "" {
			abort401With(c, "登录态需要更新，请重新登录")
			return
		}
		if sessionStore != nil {
			sess, err := sessionStore.ActiveBySID(cl.SID)
			if err != nil {
				slog.Error("auth load session", "err", err.Error())
				abort401(c)
				return
			}
			if sess == nil {
				abort401With(c, "该设备已退出登录，请重新登录")
				return
			}
			// 首次请求落库 + 后续刷新活跃时间；IP / 客户端标识只在此处采集，
			// 不经由 handler，避免每个 handler 重复解析请求头。
			if _, err := sessionStore.Touch(u.ID, cl.SID,
				ClientPlatform(c.GetHeader("X-Client-Platform")),
				SanitizeHeader(c.GetHeader("X-Client-Type")),
				SanitizeHeader(c.GetHeader("X-Client-Version")),
				c.ClientIP()); err != nil {
				// 会话登记失败不阻断业务（老部署未跑新迁移时表不存在也能继续登录）
				slog.Warn("auth touch session failed", "err", err.Error())
			}
			c.Set("sid", cl.SID)
		}
		// 顺带写入 HttpOnly cookie，供同源的 <img>/<video> 读取 /uploads 时鉴权。
		// 只用于只读文件访问；REST 接口仍只认 Authorization，避免引入 CSRF 面。
		setAuthCookie(c, strings.TrimPrefix(h, "Bearer "))
		c.Set("user", u)
		c.Set("uid", u.ID)
		c.Next()
	}
}

// UploadAuth 上传文件访问鉴权。
//
// 与 Auth 的区别：额外接受 HttpOnly cookie（浏览器加载 <img>/<video> 时无法带
// Authorization 头）。历史消息里的 /uploads/... 不含签名，登录用户打开旧消息时
// 同源请求会自动带上 cookie，因此不需要迁移数据；未登录访问一律 401。
func UploadAuth(users *store.UserStore, secret string, sessions ...*store.SessionStore) gin.HandlerFunc {
	var sessionStore *store.SessionStore
	if len(sessions) > 0 {
		sessionStore = sessions[0]
	}
	return func(c *gin.Context) {
		token := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		} else if v, err := c.Cookie(AuthCookieName); err == nil {
			token = v
		}
		if token == "" {
			abort401(c)
			return
		}
		cl, err := ParseToken(token, secret)
		if err != nil {
			abort401(c)
			return
		}
		// 媒体请求是最热的一条鉴权路径（一页图片消息 = N 次 /uploads 鉴权），
		// 用 Basic 查询避开 avatar MEDIUMTEXT 与密码哈希的读放大；
		// 该链路只判封禁态与身份，不返回资料。
		u, err := users.ByIDBasic(cl.UID)
		if err != nil {
			slog.Error("upload auth load user", "err", err)
			abort401(c)
			return
		}
		if u == nil || u.TokenVersion != cl.TV {
			abort401(c)
			return
		}
		if u.IsBanned() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "账号已被封禁"})
			return
		}
		// 安全收紧：会话已撤销（单设备下线）时，即使 cookie 仍在也不允许读附件。
		// 与 Auth / WS 同一口径，避免被撤设备的浏览器凭 mc_auth 继续读 /uploads。
		if sessionStore != nil {
			if cl.SID == "" {
				abort401With(c, "登录态需要更新，请重新登录")
				return
			}
			sess, err := sessionStore.ActiveBySID(cl.SID)
			if err != nil || sess == nil {
				abort401With(c, "该设备已退出登录，请重新登录")
				return
			}
		}
		// 用 Authorization 头访问文件的客户端（下载器 / 旧前端）也补发 cookie，
		// 下次 <img>/<video> 直接加载同源地址时即可带上。
		setAuthCookie(c, token)
		c.Set("user", u)
		c.Set("uid", u.ID)
		c.Next()
	}
}

// setAuthCookie 写入与 Bearer token 相同的 HttpOnly cookie。
// Secure 仅在 HTTPS（含反代 X-Forwarded-Proto）时开启，避免 HTTP 部署拿不到 cookie。
func setAuthCookie(c *gin.Context, token string) {
	if v, err := c.Cookie(AuthCookieName); err == nil && v == token {
		return
	}
	SetAuthCookie(c.Writer, c.Request, token)
}

// SetAuthCookie 写入登录 cookie（gin 之外的场景：WS 握手、登录/注册响应）。
// 每次带 Authorization 的已登录请求也会经 Auth 中间件补发一次，
// 因此存量用户（localStorage 有 token、浏览器无 cookie）在升级后
// 第一次调用任意已登录接口就会自动获得 cookie，无需重新登录。
func SetAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	if token == "" {
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(TokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearAuthCookie 清除登录 cookie（主动退出登录时调用）。
// 只清 localStorage 的话 cookie 仍有效，退出后 7 天内 /uploads 仍能被同浏览器访问。
func ClearAuthCookie(c *gin.Context) {
	secure := c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(AuthCookieName, "", -1, "/", "", secure, true)
}

func abort401(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录"})
}
