// Package router 汇总全部路由与中间件装配。
package router

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/gzip"
	"gorm.io/gorm"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/handler"
	"github.com/modern-day-team/modern-chat-new/server/internal/metrics"
	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/serverplugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
	"github.com/modern-day-team/modern-chat-new/server/internal/ws"
)

// Deps 路由依赖容器。
type Deps struct {
	Cfg           config.Config
	DB            *gorm.DB
	Mailer        *service.Mailer
	Notifier      handler.Notifier
	WS            *ws.Hub
	AuthLimiter   middleware.Limiter // 认证类接口按 IP 限流
	PluginLimiter middleware.Limiter // 插件安装按用户限流
	MsgLimiter    middleware.Limiter // 消息限流（WS 与服务端插件宿主共用）
	// LimiterFactory 为其余按用途隔离的限流点创建后端。main 在 Redis 可用时
	// 注入共享配额工厂，否则回退内存；测试未注入时按内存处理。
	LimiterFactory middleware.LimiterFactory
	// Settings 与 Storage 由 main 装配（Hub 也要用同一个 Storage 做 URL 归属校验）
	Settings *store.SettingStore
	Storage  service.Storage
	// Backup 备份服务（可为 nil：测试环境或未启用）
	Backup *service.BackupService
	// Updater 站长后台图形化更新（可为 nil：测试环境；Docker / 裸机自动不可用）
	Updater *service.Updater
	// AutoUpdater 自动更新调度器（可为 nil：未启用；状态并入 /update/status）
	AutoUpdater *service.AutoScheduler
	// Push Web Push 服务（可为 nil 或未配置，此时前端显示「未启用」）
	Push *service.PushService
	// ServerPlugins 服务端插件运行时（main 构造并启动；测试可为 nil）
	ServerPlugins *serverplugin.Runtime
}

// New 装配引擎。
func New(d Deps) *gin.Engine {
	// 限流后端按用途隔离：每个用途一个独立键前缀，避免同一 IP/UID 在不同
	// 接口间串配额。未注入工厂（测试）时回退内存版，保持既有行为。
	limiterFactory := d.LimiterFactory
	if limiterFactory == nil {
		limiterFactory = middleware.MemoryLimiterFactory
	}
	newLimiter := func(name string, perMin, burst int) middleware.Limiter {
		return limiterFactory(name, perMin, burst)
	}

	r := gin.New()
	// 修复缺陷：gin 默认 trustedProxies 为 0.0.0.0/0 + ::/0（信任所有代理），
	// 客户端可伪造 X-Forwarded-For 冒充任意 IP，从而绕过按 IP 的认证接口限流。
	// 这里显式限定为配置的可信代理（默认仅回环）；配置非法时告警并退化为
	// 不信任任何代理 —— 传 nil 的语义是「不使用任何转发头」，ClientIP() 只取 RemoteAddr。
	if err := r.SetTrustedProxies(d.Cfg.TrustedProxies); err != nil {
		slog.Warn("invalid TRUSTED_PROXIES, falling back to distrusting all proxies",
			"value", d.Cfg.TrustedProxies, "err", err.Error())
		_ = r.SetTrustedProxies(nil)
	}
	r.Use(gin.Recovery(), middleware.Logger())
	// 跨源（CORS）：按 FRONTEND_URL 白名单放行 Web / 桌面 WebView / 移动 WebView 的
	// 浏览器来源，预检 OPTIONS 直接 204。必须在路由与限流之前——否则预检会被
	// 认证接口的 10/min 限流误伤。原生客户端无 Origin，不受影响。
	r.Use(middleware.CORS(d.Cfg.FrontendURLs))
	// Prometheus 指标：记录路由级请求计数，并导出 /api/metrics
	metrics.Init(d.DB, func() int {
		if d.WS == nil {
			return 0
		}
		return d.WS.OnlineCount()
	})
	r.Use(metrics.Middleware())
	// 安全响应头 + 请求体上限（防超大 JSON 撑爆内存）。
	// 插件上传是 octet-stream，按 PLUGIN_MAX_SIZE 单独放宽。
	r.Use(middleware.SecurityHeaders())
	r.Use(middleware.BodyLimit(1<<20, map[string]int64{
		"/api/plugins/upload": d.Cfg.PluginMaxSize + 64*1024,
		// 干跑校验同样是裸源码上传，与 upload 同上限。
		"/api/plugins/validate": d.Cfg.PluginMaxSize + 64*1024,
		// 管理员上传 WASM：二进制单独放宽到 8MB + 1。
		"/api/admin/server-plugins/wasm": 8<<20 + 1,
		// 上传走 multipart，单独放宽。这里用「配置允许的最大值」而不是当前值：
		// 站长随时可能在后台调大上限，中间件在启动时构造，用当前值会立刻卡住。
		// 真正的逐类型限额由 UploadHandler 用 MaxBytesReader 在读之前卡住。
		"/api/upload": service.MaxAllowedUploadBytes + (1 << 20),
	}))
	// REST 响应压缩（设计文档 §9.2）。
	// /ws 必须排除：WebSocket 升级依赖 Hijack，被 gzip 包装的 ResponseWriter 会破坏握手；
	// WS 侧用 permessage-deflate 单独压缩（见 internal/ws 的 upgraderFor）。
	// /uploads 也排除：JPEG/视频/音频本身已是压缩格式，再 gzip 白耗 CPU 且几乎不再变小。
	// MinLength=1024：小于该长度的响应压缩后反而更大（gzip 头 + 熵不足）。
	r.Use(gzip.Gzip(
		gzip.DefaultCompression,
		// /api/ext 的插件响应可能只有几十字节；gzip 中间件会给小响应错误地打上
		// Content-Encoding: gzip（浏览器解压失败），统一排除动态插件接口。
		gzip.WithExcludedPaths([]string{"/ws", "/uploads", "/api/ext", "/api/ext-public"}),
		gzip.WithMinLength(1024),
	))
	if d.Notifier != nil {
		r.Use(func(c *gin.Context) {
			c.Set("notifier", d.Notifier)
			c.Next()
		})
	}

	users := store.NewUserStore(d.DB)
	friends := store.NewFriendStore(d.DB)
	convs := store.NewConversationStore(d.DB)
	msgs := store.NewMessageStore(d.DB)
	stickers := store.NewStickerStore(d.DB)
	files := store.NewFileStore(d.DB)
	invites := store.NewInviteStore(d.DB)
	loginAttempts := store.NewLoginAttemptStore(d.DB)
	moderation := store.NewModerationStore(d.DB)
	pushSubs := store.NewPushStore(d.DB)
	plugins := store.NewPluginStore(d.DB)
	pluginData := store.NewPluginDataStore(d.DB)
	serverPluginStore := store.NewServerPluginStore(d.DB)
	// main 拥有并启动同一个 runtime（ws.Emit 也用它）；测试未注入时用本地实例兜底。
	spRuntime := d.ServerPlugins
	if spRuntime == nil {
		spRuntime = serverplugin.NewRuntime(d.Cfg.JWTSecret, serverPluginStore)
	}
	spRuntime.SetKVStore(store.NewServerPluginKVStore(d.DB))
	if d.Cfg.AIEnabled() {
		spRuntime.SetAIConfig(serverplugin.AIConfig{
			BaseURL: d.Cfg.AIBaseURL, APIKey: d.Cfg.AIAPIKey, Model: d.Cfg.AIModel,
			Timeout: time.Duration(d.Cfg.AITimeoutSeconds) * time.Second, AllowPrivate: d.Cfg.AIAllowPrivate,
		})
	}
	// 注入宿主业务能力（message.send / conv.* / user.* / notification.push）。
	// 权限边界与限流见 plugin_host.go；未配置的服务注入 nil，mc_call 返回明确错误。
	WireServerPluginHost(spRuntime, users, convs, msgs, d.WS, d.Settings, d.Storage, pushSubs, d.Push, d.MsgLimiter)
	resets := store.NewPasswordResetStore(d.DB)
	audits := store.NewAuditStore(d.DB)
	sessions := store.NewSessionStore(d.DB)

	// 系统设置与上传存储由 main 装配（Hub 也要用同一个 Storage 做 URL 归属校验）
	settings := d.Settings
	// 启动时用库里的配置覆盖环境变量：后台改过的值重启后继续生效
	if cfg, ok := settings.SMTPConfig(d.Cfg.SMTP.LogResetLink); ok {
		d.Mailer.SetConfig(cfg)
		slog.Info("smtp config loaded from database", "host", cfg.Host, "port", cfg.Port)
	}

	// 注册邮箱验证码：哈希密钥由 JWT_SECRET 派生（换密钥后旧码自动失效）
	emailCodes := store.NewEmailCodeStore(d.DB, d.Cfg.JWTSecret)
	// 旧站迁移：旧库连接懒建（首次请求才 dial），功能关闭时不连接、不影响启动
	migrateSvc := service.NewMigrateService(d.Cfg, d.DB, users, convs, msgs)
	twofaCodes := store.NewTwofaRecoveryStore(d.DB)
	authH := handler.NewAuthHandler(d.Cfg, users, resets, d.Mailer, convs, msgs, settings, invites, loginAttempts, emailCodes, twofaCodes, migrateSvc)
	// 登录签发按设备指纹激活会话，避免重复登录刷出多条相同设备。
	authH.SetSessions(sessions)
	// 头像转存：跟随当前存储实现（本地磁盘 / 对象存储），与上传接口共用一套。
	authH.SetStorage(d.Storage)
	inviteSvc := service.NewInviteService(invites)
	inviteH := handler.NewInviteHandler(inviteSvc)
	sessionSvc := service.NewSessionService(sessions)
	userH := handler.NewUserHandler(users)
	userH.SetSessions(sessionSvc)
	sessionH := handler.NewSessionHandler(sessionSvc, d.WS)
	friendH := handler.NewFriendHandler(users, friends, convs, moderation)
	convH := handler.NewConversationHandler(users, friends, convs, msgs, moderation, settings, d.Storage)
	// 收藏域业务规则归位 service 内核：handler 依赖窄接口而非具体 store。
	convH.SetFavorites(service.NewFavoriteService(msgs))
	modH := handler.NewModerationHandler(users, convs, msgs, moderation)
	stickerH := handler.NewStickerHandler(stickers, d.Storage)
	fileH := handler.NewFileHandler(files, convs, d.Storage)
	pluginH := handler.NewPluginHandler(d.Cfg, plugins, audits)
	pluginH.SetPluginData(pluginData)
	pluginDataH := handler.NewPluginDataHandler(plugins, pluginData)
	serverPluginH := handler.NewServerPluginHandler(serverPluginStore, audits, spRuntime)
	aiH := handler.NewAIHandler(d.Cfg)
	adminH := handler.NewAdminHandler(d.Cfg, users, convs, msgs, plugins, audits, settings, d.Mailer)
	opsH := handler.NewOpsHandler(d.Cfg, d.DB, d.Backup)
	pushH := handler.NewPushHandler(d.Push, pushSubs)
	// 旧站迁移接口（PHP 侧服务端调用，靠 X-Migrate-Secret 鉴权，不走 JWT）
	migrateH := handler.NewMigrateHandler(d.Cfg, migrateSvc)
	// 站长后台群聊管理（只读）
	adminGroupsH := handler.NewAdminGroupsHandler(convs, msgs)

	// 上传文件存储由 main 依据配置构造（本地或 S3 兼容），这里只挂接口与静态服务
	storage := d.Storage
	var uploadH *handler.UploadHandler
	if storage != nil {
		uploadH = handler.NewUploadHandler(storage, settings)
		// 静态服务**始终**挂载：改用对象存储后，历史消息里的 /uploads/... 仍要能打开
		// （否则老图片/语音全部 404，而数据库里还留着这些 URL）。
		// 新文件走当前存储，本地目录只服务历史文件，留空也只是 404，无副作用。
		uploads := r.Group("/uploads",
			// 必须登录才能读取：历史消息里的 URL 不带签名，浏览器 <img>/<video>
			// 依靠同源 HttpOnly cookie 通过校验，旧数据无需迁移。
			middleware.UploadAuth(users, d.Cfg.JWTSecret, sessions),
			// 维护模式下与 API 一致的受控行为（管理员仍可访问）
			middleware.Maintenance(settings),
		)
		serveUpload := func(c *gin.Context) {
			// 文件名随机且内容不可变（上传后从不覆盖），可安全长期缓存；
			// 仍用 private：资源受 cookie 保护，禁止代理/CDN 共享缓存。
			// immutable 让浏览器命中缓存时不再发条件请求，减少往返。
			c.Header("Cache-Control", "private, max-age=31536000, immutable")
			c.Header("X-Content-Type-Options", "nosniff")

			// 强 ETag（mtime+size）：http.FileServer 自己不生成 ETag，
			// 若不做条件请求，浏览器每次 stale 都要重新下载整文件。
			// 这里先 stat 出 ETag，再交给 FileServer 处理 Range/206 与 304。
			rel := strings.TrimPrefix(c.Param("filepath"), "/")
			base := service.UploadsDir(d.Cfg.DataDir)
			if st, err := os.Stat(filepath.Join(base, filepath.FromSlash(rel))); err == nil && !st.IsDir() {
				etag := `"` + strconv.FormatInt(st.ModTime().UnixNano(), 36) + `-` + strconv.FormatInt(st.Size(), 36) + `"`
				c.Header("ETag", etag)
			}

			fs := onlyFilesFS{http.Dir(base)}
			http.StripPrefix("/uploads", http.FileServer(fs)).ServeHTTP(c.Writer, c.Request)
		}
		uploads.GET("/*filepath", serveUpload)
		uploads.HEAD("/*filepath", serveUpload)
	}

	api := r.Group("/api")
	api.GET("/health", handler.Health)
	api.GET("/version", handler.Version)
	// Prometheus 指标（可通过 METRICS_TOKEN 加 Bearer 保护）
	api.GET("/metrics", metrics.Handler(d.Cfg.MetricsToken))
	// 站点信息（免鉴权）：站点名、注册开关、消息长度/撤回时限/上传上限。
	// 不含任何密钥类配置。
	siteH := handler.NewSiteHandler(settings, d.Mailer, users, d.Cfg.Call)
	api.GET("/site", siteH.Get)
	// 客户端版本探测（免鉴权）：各端启动时提示升级，不阻断旧端登录
	api.GET("/client/version", handler.ClientVersion(d.Cfg.MinClientVersion))
	// 群邀请落地页信息（免登录可看；实际加入需要登录）
	api.GET("/invite/:token", convH.InviteInfo)

	// 认证（免鉴权，按 IP 限流 10/min）
	authLimit := middleware.RateLimit(d.AuthLimiter, middleware.KeyByIP)
	// login 不挂维护中间件：必须给管理员留一条登录进来关掉维护模式的路
	api.POST("/auth/login", authLimit, authH.Login)
	// 清登录 cookie（HttpOnly，前端无法自行删除）。放在未登录组，
	// 维护模式下也要能调用；不鉴权，token 已失效时同样允许清理。
	api.POST("/auth/logout", authH.Logout)
	api.POST("/auth/register", authLimit, middleware.Maintenance(settings), authH.Register)
	api.POST("/auth/forgot", authLimit, middleware.Maintenance(settings), authH.Forgot)
	api.POST("/auth/reset", authLimit, middleware.Maintenance(settings), authH.Reset)
	// 注册验证码：与注册同组限流（IP 10/min），另外 store 内对同一邮箱有 60 秒冷却
	api.POST("/auth/email-code", authLimit, middleware.Maintenance(settings), authH.EmailCode)

	// 旧站迁移（契约 §4）：除 migrate-login 外都校验请求头 X-Migrate-Secret；
	// 由旧站 PHP 服务端调用，不要求 JWT。迁移功能未启用时统一 503。
	// 这几条是不带 JWT 的公网端点，单独按 IP 限流：既能挡密钥爆破，
	// 也能兜住旧站 PHP 的重试风暴（一个大用户的 messages 阶段会有几十次 tick）。
	// 120/min + burst 20 足够正常迁移跑满，又不至于把带宽/CPU 打满。
	migrateLimit := middleware.RateLimit(newLimiter("migrate", 120, 20), middleware.KeyByIP)
	api.POST("/migrate/preview", migrateLimit, migrateH.Preview)
	api.POST("/migrate/start", migrateLimit, migrateH.Start)
	api.POST("/migrate/tick", migrateLimit, migrateH.Tick)
	api.POST("/migrate/complete", migrateLimit, migrateH.Complete)
	api.POST("/migrate/complete-existing", migrateLimit, migrateH.CompleteExisting)
	// 迁移完成后的免密登录：公开接口，按 IP 限流，一次性 token+key 校验
	api.POST("/auth/migrate-login", authLimit, authH.MigrateLogin)

	// 统一登录（聚合登录中心，handler/sso.go）：一次性票据防泄漏，state 防 CSRF。
	// 未配置 SSO_* 时全部 404，不影响正常使用；start/callback 是浏览器跳转链路。
	api.GET("/auth/sso/status", authH.SSOStatus)
	api.GET("/auth/sso/start", authLimit, authH.SSOStart)
	api.GET("/auth/sso/callback", authLimit, authH.SSOCallback)
	api.POST("/auth/sso/exchange", authLimit, authH.SSOExchange)

	// 扫码登录（handler/auth_qr.go）：桌面展示二维码，手机端已登录会话扫码确认。
	// 桌面未登录态轮询 status（POST 传 body，票据不进访问日志），专用限流 60/min
	// —— authLimit 的 10/min 会被 2.5s 一次的轮询打满。
	qrLimit := middleware.RateLimit(newLimiter("qr", 60, 20), middleware.KeyByIP)
	api.POST("/auth/qr/start", authLimit, authH.QRStart)
	api.POST("/auth/qr/status", qrLimit, authH.QRStatus)
	// 服务端插件的公开动态接口：只放行 manifest 标记 public 的路由（扫码首屏/回调）。
	// 独立限流，避免匿名脚本打爆 WASM 实例。
	extPublicLimit := middleware.RateLimit(newLimiter("ext-public", 120, 30), middleware.KeyByIP)
	api.Any("/ext-public/:plugin/*path", extPublicLimit, serverPluginH.DynamicPublic)

	// 两步验证（handler/auth_2fa.go，默认关闭）：登录密码正确后若开了 2FA，
	// 只发短寿命票据，verify 通过才签 JWT。verify/email-code 免鉴权靠票据，
	// 每票据最多试 5 次 + authLimit 双重限流；设置页接口走 authed 组。
	api.POST("/auth/2fa/verify", authLimit, authH.TwofaVerify)
	api.POST("/auth/2fa/email-code", authLimit, authH.TwofaEmailCodeForLogin)

	// 需登录（Auth → Ban → Maintenance：维护模式要能读到用户角色）
	authed := api.Group(
		"",
		middleware.Auth(users, d.Cfg.JWTSecret, sessions),
		middleware.Ban(),
		middleware.Maintenance(settings),
	)
	{
		authed.GET("/me", authH.Me)
		authed.PATCH("/me", authH.UpdateMe)
		// 扫码登录：手机端（已登录）确认授权桌面登录；维护模式随组内中间件 503
		authed.POST("/auth/qr/confirm", authH.QRConfirm)
		// 两步验证：状态 / 开启（TOTP、邮箱）/ 关闭（需密码再认证）
		authed.GET("/me/2fa", authH.TwofaStatus)
		authed.POST("/me/2fa/totp/setup", authH.TwofaTotpSetup)
		authed.POST("/me/2fa/totp/enable", authH.TwofaTotpEnable)
		authed.POST("/me/2fa/email/code", authH.TwofaEmailCode)
		authed.POST("/me/2fa/email/enable", authH.TwofaEmailEnable)
		authed.POST("/me/2fa/disable", authH.TwofaDisable)
		// 换绑邮箱：给「新邮箱」发验证码（按用户限流，60 秒冷却由 store 保证）
		emailCodeLimit := middleware.RateLimit(newLimiter("email-code-change", 5, 5), middleware.KeyByUID)
		authed.POST("/me/email-code", emailCodeLimit, authH.EmailCodeChange)

		authed.GET("/users/search", userH.Search)
		// 登录会话：查看各端设备 / 单独退出某台
		authed.GET("/me/sessions", sessionH.List)
		authed.DELETE("/me/sessions/:id", sessionH.Revoke)
		// 退出所有设备：旧 JWT 全部失效（需重新登录）
		authed.POST("/me/logout-others", userH.LogoutOthers)

		authed.GET("/friends", friendH.List)
		authed.GET("/friends/requests", friendH.Requests)
		authed.POST("/friends/apply", friendH.Apply)
		authed.POST("/friends/handle", friendH.Handle)
		authed.DELETE("/friends/:friendId", friendH.Remove)

		authed.GET("/conversations", convH.List)
		authed.GET("/conversations/search", convH.SearchGroups)
		authed.POST("/conversations/single", convH.CreateSingle)
		authed.POST("/conversations/group", convH.CreateGroup)
		authed.GET("/conversations/:id/messages", convH.Messages)
		authed.DELETE("/conversations/:id/messages/:messageId", convH.DeleteMessage)
		authed.GET("/conversations/:id/members", convH.Members)
		authed.POST("/conversations/:id/join", convH.Join)
		authed.POST("/conversations/:id/invite", convH.Invite)
		authed.POST("/conversations/:id/invite-link", convH.CreateInviteLink)
		authed.POST("/invite/:token/join", convH.JoinByInvite)
		authed.POST("/conversations/:id/leave", convH.Leave)
		authed.POST("/conversations/:id/read", convH.Read)
		authed.PATCH("/conversations/:id", convH.Rename)
		authed.DELETE("/conversations/:id", convH.Delete)
		authed.POST("/conversations/:id/clear", convH.Clear)
		authed.POST("/conversations/:id/mute-all", convH.SetMuteAll)
		authed.PUT("/conversations/:id/avatar", convH.SetAvatar)
		authed.POST("/conversations/:id/kick", convH.Kick)
		authed.POST("/conversations/:id/admin", convH.SetAdmin)
		authed.POST("/conversations/:id/mute", convH.Mute)
		authed.POST("/conversations/:id/transfer", convH.Transfer)

		// 收藏夹（消息收藏）
		// 会话个人设置（置顶 / 免打扰）
		authed.PATCH("/conversations/:id/prefs", convH.SetPrefs)
		// 群公告（仅群主）
		authed.PUT("/conversations/:id/announcement", convH.SetAnnouncement)
		// 群文件（文件本体走 /api/upload?kind=file）
		authed.GET("/conversations/:id/files", fileH.List)
		authed.POST("/conversations/:id/files", fileH.Add)
		authed.DELETE("/files/:id", fileH.Delete)
		authed.GET("/favorites", convH.Favorites)
		authed.GET("/search/messages", convH.SearchMessages)

		// 个人表情库（表情文件走 /api/upload?kind=sticker）
		authed.GET("/stickers", stickerH.List)
		authed.POST("/stickers", stickerH.Add)
		authed.DELETE("/stickers/:id", stickerH.Delete)
		authed.POST("/messages/:messageId/favorite", convH.Favorite)
		authed.DELETE("/messages/:messageId/favorite", convH.Unfavorite)
		// 治理：拉黑 / 举报 / 管理员删除违规消息
		authed.GET("/users/:id/block", modH.BlockStatus)
		authed.POST("/users/:id/block", modH.Block)
		authed.DELETE("/users/:id/block", modH.Unblock)
		authed.POST("/messages/:messageId/report", modH.Report)
		authed.POST("/messages/:messageId/admin-delete", modH.AdminDeleteMessage)
		// Web Push：公钥下发与订阅管理
		authed.GET("/push/key", pushH.Key)
		authed.POST("/push/subscribe", pushH.Subscribe)
		authed.DELETE("/push/subscribe", pushH.Unsubscribe)

		authed.GET("/plugins", pluginH.List)
		authed.GET("/plugins/active", pluginH.Active)
		authed.GET("/plugins/:id/code", pluginH.Code)
		pluginLimit := middleware.RateLimit(d.PluginLimiter, middleware.KeyByUID)
		authed.POST("/plugins/upload", pluginLimit, pluginH.Upload)
		// 干跑校验：开发工具（scripts/dev-plugin.mjs）与安装走同一套 Go 规则。
		// 与 upload 共用限流桶——它同样要跑 esbuild 转译，算力开销一样。
		authed.POST("/plugins/validate", pluginLimit, pluginH.Validate)

		// 图片 / 语音上传：按用户限流（10 次/分），避免被当图床刷
		if uploadH != nil {
			uploadLimit := middleware.RateLimit(newLimiter("upload", 10, 10), middleware.KeyByUID)
			authed.POST("/upload", uploadLimit, uploadH.Upload)
		}
		authed.POST("/plugins/from-url", pluginLimit, pluginH.FromURL)
		authed.PATCH("/plugins/:id", pluginH.SetEnabled)
		authed.DELETE("/plugins/:id", pluginH.Remove)
		// 用户插件服务端私有 KV（按「当前用户 + 插件 id」隔离）
		authed.GET("/plugins/:id/data", pluginDataH.Get)
		authed.PUT("/plugins/:id/data", pluginDataH.Set)
		authed.DELETE("/plugins/:id/data", pluginDataH.Delete)
		authed.GET("/plugins/:id/data/keys", pluginDataH.Keys)
		// 服务端统一 AI 代理：密钥只在服务端；按用户限流，避免用户插件刷爆上游
		aiLimit := middleware.RateLimit(newLimiter("ai", 20, 5), middleware.KeyByUID)
		// WebRTC ICE 配置（TURN 凭据只经此登录接口下发）
		authed.GET("/call/config", siteH.CallConfig)
		authed.GET("/ai/status", aiH.Status)
		authed.POST("/ai/chat", aiLimit, aiH.Chat)
		// 服务端插件：页面声明与动态接口（仍需登录）
		authed.GET("/server-plugins/pages", serverPluginH.Pages)
		authed.Any("/ext/:plugin/*path", serverPluginH.Dynamic)
	}

	// 站长
	admin := api.Group("/admin", middleware.Auth(users, d.Cfg.JWTSecret, sessions), middleware.Ban(), middleware.Admin())
	{
		admin.GET("/stats", adminH.Stats)
		admin.GET("/users", adminH.Users)
		admin.GET("/users/:id", adminH.UserDetail)
		admin.POST("/users/:id/ban", adminH.Ban)
		admin.POST("/users/:id/unban", adminH.Unban)
		admin.POST("/users/:id/kick", adminH.Kick)
		admin.POST("/users/:id/reset-password", adminH.ResetPassword)

		admin.GET("/plugins", adminH.Plugins)
		admin.POST("/plugins/global-disable", adminH.SetPluginGlobalDisabled)
		admin.POST("/plugins/purge", adminH.PurgePlugin)
		// 服务端声明式插件（manifest 安装 / 启停 / 删除）
		admin.GET("/server-plugins", serverPluginH.List)
		admin.POST("/server-plugins", serverPluginH.Install)
		admin.POST("/server-plugins/wasm", serverPluginH.UploadWASM)
		admin.PATCH("/server-plugins/:id", serverPluginH.SetEnabled)
		admin.DELETE("/server-plugins/:id", serverPluginH.Delete)
		admin.POST("/users/:id/role", adminH.SetRole)
		// 注册邀请码
		admin.GET("/invites", inviteH.List)
		admin.POST("/invites", inviteH.Create)
		admin.POST("/invites/:id/disable", inviteH.SetDisabled)
		admin.POST("/announce", adminH.Announce)
		admin.GET("/audits", adminH.Audits)
		// 群聊管理（只读；契约 docs/旧站迁移方案.md §8）
		admin.GET("/groups", adminGroupsH.Groups)
		admin.GET("/groups/:id", adminGroupsH.GroupDetail)
		admin.GET("/groups/:id/messages", adminGroupsH.GroupMessages)
		// 系统设置（SMTP / Redis / 对象存储）
		admin.GET("/settings", adminH.GetSettings)
		admin.PUT("/settings", adminH.SaveSettings)
		admin.POST("/settings/smtp/test", adminH.TestSMTP)
		// 对象存储服务商预设 + 只读连通性自检（HEAD Bucket，不写对象）
		admin.GET("/settings/storage/providers", adminH.StorageProviders)
		admin.POST("/settings/storage/test", adminH.TestStorage)

		// 运维：附件统计/孤儿清理、数据库备份
		admin.GET("/storage/stats", opsH.StorageStats)
		admin.POST("/storage/gc", opsH.GC)
		admin.GET("/backups", opsH.Backups)
		admin.POST("/backups", opsH.RunBackup)

		// 在线更新（图形化；Docker / 裸机部署自动不可用，WEB_UPDATE=false 可整体关闭）
		updateH := handler.NewAdminUpdateHandler(d.Updater, audits, d.AutoUpdater)
		admin.GET("/update/status", updateH.Status)
		admin.GET("/update/check", updateH.Check)
		admin.POST("/update/start", updateH.Start)
	}

	// WebSocket（握手自行校验 JWT 与 Origin，不走中间件组）
	if d.WS != nil {
		r.GET("/ws", func(c *gin.Context) {
			ws.Serve(d.Cfg.JWTSecret, d.Cfg.FrontendURLs, users, d.WS, settings, c.Writer, c.Request, sessions)
		})
	}

	return r
}

// onlyFilesFS 只允许打开普通文件，目录一律返回不存在。
//
// http.FileServer 对目录会列出文件名；上传目录里全是私人图片/语音/视频，
// 一旦可枚举就等于把全站附件公开。这里让目录 Open 直接失败，列表页变成 404。
type onlyFilesFS struct{ http.FileSystem }

func (o onlyFilesFS) Open(name string) (http.File, error) {
	f, err := o.FileSystem.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if st.IsDir() {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}
