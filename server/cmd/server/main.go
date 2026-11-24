// ModernChat 单实例后端：Gin + GORM(MySQL) + gorilla/websocket。
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/logx"
	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/router"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
	"github.com/modern-day-team/modern-chat-new/server/internal/ws"
)

func main() {
	// 本地开发便利：存在 .env 时加载（已 gitignore）；环境变量优先于 .env
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	logCloser, err := setupLogger(cfg)
	if err != nil {
		slog.Error("logger init", "err", err)
		os.Exit(1)
	}
	gin.SetMode(cfg.GinMode)

	// 数据库（重试连接）
	db, err := store.Open(cfg.DBDSN, 10, 2*time.Second)
	if err != nil {
		slog.Error("db open", "err", err)
		os.Exit(1)
	}
	if cfg.Migrate {
		if err := store.RunMigrations(cfg.DBDSN); err != nil {
			slog.Error("migrate", "err", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	} else {
		if err := store.AutoMigrate(db); err != nil {
			slog.Error("automigrate", "err", err)
			os.Exit(1)
		}
	}
	// FULLTEXT ngram 索引：迁移 0022 在生产负责，这里幂等兜底（AutoMigrate 不建全文索引）
	if err := store.EnsureMessageFulltext(db); err != nil {
		slog.Warn("ensure message fulltext index", "err", err)
	}

	// 初始公开群：首次启动时创建并把现有用户加入；之后注册的新用户自动加入
	if err := store.BootstrapDefaultGroup(db); err != nil {
		slog.Warn("bootstrap default group failed", "err", err.Error())
	}

	// 限流后端：优先 Redis（多实例共享配额），不可用时降级为内存
	var (
		authLimiter   middleware.Limiter
		msgLimiter    middleware.Limiter
		pluginLimiter middleware.Limiter
	)
	rdb, redisErr := store.OpenRedis(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if rdb != nil {
		slog.Info("rate limiting backed by redis", "addr", cfg.Redis.Addr, "db", cfg.Redis.DB)
		authLimiter = middleware.NewRedisLimiter(rdb, 10)
		msgLimiter = middleware.NewRedisLimiter(rdb, 20)
		pluginLimiter = middleware.NewRedisLimiter(rdb, 5)
	} else {
		if cfg.Redis.Enabled() {
			slog.Warn("redis unavailable, falling back to in-memory limiter",
				"addr", cfg.Redis.Addr, "err", redisErr)
		} else {
			slog.Info("redis not configured (REDIS_ADDR empty), using in-memory limiter")
		}
		authLimiter = middleware.NewLimiterStore(10, 10)
		msgLimiter = middleware.NewLimiterStore(20, 20)
		pluginLimiter = middleware.NewLimiterStore(5, 5)
	}

	// 系统设置 + 上传存储：Hub 与 router 都要用，所以在装配处构造一次。
	// 存储类型取决于后台是否配了对象存储（数据库优先于环境变量）。
	box, boxErr := store.NewSecretBox(cfg.JWTSecret)
	if boxErr != nil {
		slog.Error("init secretbox failed", "err", boxErr.Error())
	}
	settings := store.NewSettingStore(db, box)

	storageCfg := service.StorageConfig{Kind: "local", DataDir: cfg.DataDir}
	if ss := settings.StorageSettings(); ss.Configured {
		storageCfg = service.StorageConfig{
			Kind:       "s3",
			DataDir:    cfg.DataDir,
			Provider:   ss.Provider,
			Endpoint:   ss.Endpoint,
			Region:     ss.Region,
			Bucket:     ss.Bucket,
			AccessKey:  ss.AccessKey,
			SecretKey:  ss.SecretKey,
			PublicBase: ss.PublicBase,
			PathStyle:  service.FindS3Provider(ss.Provider).PathStyle,
		}
	}
	uploadStorage, stErr := service.NewStorage(storageCfg)
	if stErr != nil {
		// 存储不可用不阻断启动：文本聊天仍可用，上传接口不注册
		slog.Error("init upload storage failed; upload API disabled", "err", stErr.Error())
		uploadStorage = nil
	} else {
		slog.Info("upload storage ready", "kind", uploadStorage.Kind())
	}

	// 数据库备份：手动接口随时可用；配置了间隔后额外开启定时备份
	backupDir := cfg.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(cfg.DataDir, "backups")
	}
	backup := service.NewBackupService(cfg.DBDSN, backupDir, cfg.BackupRetain, cfg.MysqldumpPath)
	if cfg.BackupIntervalHours > 0 {
		slog.Info("automatic backup enabled",
			"dir", backupDir, "hours", cfg.BackupIntervalHours, "retain", cfg.BackupRetain)
		go backup.Loop(time.Hour * time.Duration(cfg.BackupIntervalHours))
	}

	// Web Push（未配置 VAPID 密钥时 Enabled=false，所有推送静默跳过）
	pushService := service.NewPushService(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
	if pushService.Enabled() {
		slog.Info("web push enabled")
	}

	// 图形化更新（站长后台）：WEB_UPDATE=false 关闭；部署能力由 Updater.Available()
	// 检测（systemd 部署；更新源优先 RELEASE_API_URL/RELEASE_DL_URL，未配置时按
	// RELEASE_REPO_GITHUB 或默认官方仓库走 GitHub Release，git clone 部署仍走源码模式）。
	// Docker / 裸机自动不可用。
	var updater *service.Updater
	if cfg.WebUpdate {
		wd, wdErr := os.Getwd()
		root := ""
		if wdErr == nil {
			root = filepath.Dir(wd)
		}
		updater = service.NewUpdater(root, cfg.Port, service.ReleaseSource{
			API:   cfg.ReleaseAPIURL,
			DL:    cfg.ReleaseDLURL,
			Repo:  cfg.ReleaseRepoGithub,
			Token: cfg.GithubToken,
		})
		if updater.Available() {
			slog.Info("web update enabled", "mode", updater.Mode(), "repo", updater.Repo(), "root", updater.RootDir(), "port", updater.Port())
		}
	} else {
		updater = service.NewUpdater("", cfg.Port, service.ReleaseSource{})
		slog.Info("web update disabled (WEB_UPDATE=false)")
	}

	// 自动更新：定时检查 GitHub Release 的新 tag，命中则复用图形化更新的 Start()
	// 链路（下载 / sha256 / 原子替换 / 重启）。仅发行版 + systemd 部署生效，
	// 源码模式与 Docker / 裸机在 Start() 里自行跳过并写入 skipReason。
	auditStore := store.NewAuditStore(db)
	var autoScheduler *service.AutoScheduler
	if cfg.AutoUpdate && cfg.WebUpdate {
		autoScheduler = service.NewAutoScheduler(updater,
			time.Duration(cfg.AutoUpdateIntervalMin)*time.Minute,
			func(tag, local, remote string) {
				detail := "检测到新版本 " + tag + "（本地 " + local + "），自动触发升级"
				if err := auditStore.Record(store.AuditEntry{
					AdminID:    0, // 系统触发，非某位站长手动操作
					Action:     "auto_update",
					TargetType: "system",
					TargetName: "web_update",
					Detail:     detail,
					IP:         "system",
				}); err != nil {
					slog.Warn("auto update audit write failed", "err", err.Error())
				}
			})
		autoScheduler.Start()
	} else {
		slog.Info("auto update disabled", "autoUpdate", cfg.AutoUpdate, "webUpdate", cfg.WebUpdate)
		// 即使关闭也构造一个未启动的调度器，让后台能显示「已关闭」状态
		autoScheduler = service.NewAutoScheduler(updater, time.Duration(cfg.AutoUpdateIntervalMin)*time.Minute, nil)
	}

	// WebSocket Hub
	hub := ws.NewHub(&ws.Deps{
		Convs:      store.NewConversationStore(db),
		Messages:   store.NewMessageStore(db),
		Friends:    store.NewFriendStore(db),
		Users:      store.NewUserStore(db), // 连接级封禁 / tv 复核（task-4 修复点）
		Moderation: store.NewModerationStore(db),
		MsgLimiter: msgLimiter,
		Storage:    uploadStorage,
		Settings:   settings,
		Push:       pushService,
		PushSubs:   store.NewPushStore(db),
	})
	go hub.Run()

	r := router.New(router.Deps{
		Cfg:           cfg,
		DB:            db,
		Mailer:        service.NewMailer(cfg.SMTP),
		Notifier:      hub,
		WS:            hub,
		Settings:      settings,
		Storage:       uploadStorage,
		Backup:        backup,
		Push:          pushService,
		AuthLimiter:   authLimiter,
		PluginLimiter: pluginLimiter,
		Updater:       updater,
		AutoUpdater:   autoScheduler,
	})

	// 只设读头/空闲超时，不设 ReadTimeout / WriteTimeout：
	// WebSocket 与文件上传/下载都是长连接，整体读写超时会误杀正常会话。
	// ReadHeaderTimeout 防 Slowloris（只发一半请求头长期占连接），
	// IdleTimeout 回收 keep-alive 空闲连接（默认无上限）。
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	go func() {
		slog.Info("server started", "port", cfg.Port, "mode", cfg.GinMode, "tls", cfg.TLSCertFile != "" && cfg.TLSKeyFile != "")
		var serveErr error
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			// 配置了证书则直接启用 HTTPS，不再依赖前置反向代理
			serveErr = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			serveErr = srv.ListenAndServe()
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("listen", "err", serveErr)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down")
	autoScheduler.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown", "err", err)
	}
	hub.Close()
	if rdb != nil {
		_ = rdb.Close()
	}
	store.Close(db)
	slog.Info("bye")
	_ = logCloser.Close()
}

func setupLogger(cfg config.Config) (io.Closer, error) {
	var lv slog.Level
	switch cfg.LogLevel {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return logx.Setup(lv, cfg.LogDir, cfg.LogRetainDays)
}
