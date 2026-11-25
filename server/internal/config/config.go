// Package config 负责环境变量解析与默认值（见 .env.example）。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config 全部运行配置，来源仅环境变量。
type Config struct {
	Port      string
	GinMode   string
	DBDSN     string
	JWTSecret string
	LogLevel  string
	// LegacyAPISecret 旧站迁移接口共享密钥（请求头 X-Migrate-Secret）；
	// 与旧站 chat/migrate_config.php 保持一致。为空 = 迁移功能整体关闭。
	LegacyAPISecret string
	// LegacyDBDSN 旧站 MySQL 只读 DSN（LEGACY_DB_DSN）。
	// 为空 = 迁移功能整体关闭；服务只读旧库，绝不写入。
	LegacyDBDSN   string
	Migrate       bool
	PluginMaxSize int64
	FrontendURLs  []string // 允许的前端来源（WS Origin + CORS 白名单 + 找回密码链接）
	// MinClientVersion 最低客户端版本（如 0.2.0）；空表示不限制。
	// 仅作提示：各端通过 /api/client/version 探测，不阻断旧端登录。
	MinClientVersion string
	TrustedProxies   []string // 可信反向代理网段；空表示不信任任何代理（不使用 XFF 判定 ClientIP）
	LogDir           string   // 日志目录；留空则只输出 stdout
	DataDir          string   // 数据目录（图片/语音等上传文件），默认 ./data
	LogRetainDays    int      // 日志保留天数（默认 15）
	// MetricsToken 非空时 /api/metrics 需要 Bearer token
	MetricsToken string
	// BackupDir 备份目录；默认 <DataDir>/backups
	BackupDir string
	// BackupIntervalHours 自动备份间隔（0=关闭）
	BackupIntervalHours int
	// BackupRetain 保留最近多少份备份
	BackupRetain int
	// MysqldumpPath mysqldump 可执行文件路径（默认取 PATH）
	MysqldumpPath string
	// WebUpdate 站长后台图形化更新开关（WEB_UPDATE，默认开）
	WebUpdate bool
	// AutoUpdate 自动更新开关（AUTO_UPDATE，默认开）：定时查询 GitHub Release，
	// 发现新 tag 自动触发升级。仅发行版 + systemd 部署生效；源码模式不自动
	// git pull（本地构建风险更高，且 tag 在源码模式没有语义）。
	AutoUpdate bool
	// AutoUpdateIntervalMin 自动更新检查间隔分钟（AUTO_UPDATE_INTERVAL，默认 30，
	// 下限 5 分钟防止打爆 GitHub 匿名 60 次/小时的 API 限额）。
	AutoUpdateIntervalMin int
	// ReleaseAPIURL / ReleaseDLURL 发行版模式：releases/latest API 与下载前缀
	// （RELEASE_API_URL / RELEASE_DL_URL，install.sh 写入 .env）。都为空时按
	// ReleaseRepoGithub 派生（未配置则用官方仓库），仅 git clone 部署仍走源码模式。
	ReleaseAPIURL string
	ReleaseDLURL  string
	// ReleaseRepoGithub 发行版托管仓库（RELEASE_REPO_GITHUB，格式 owner/repo）。
	// RELEASE_API_URL / RELEASE_DL_URL 未配置时用它派生两者；为空用官方仓库。
	// 用于把 Release 托管在 fork / 自建镜像仓库的场景。
	ReleaseRepoGithub string
	// GithubToken 访问 GitHub API 的令牌（GITHUB_TOKEN，可选）。
	// 匿名调用 api.github.com 限流 60 次/小时/IP，配置后提升到 5000 次/小时，
	// 并支持私有仓库的 Release 检查。仅用于更新请求头，不落日志。
	GithubToken string
	// SSO 统一登录（聚合登录中心，见 handler/sso.go）：
	//   - SSOSiteURL 本站对外地址（如 https://chat.example.com，用于拼回调与回跳页）
	//   - SSOAppBase 前端挂载前缀（默认 /app；前端部署在站点根时设为空）
	//   - SSOHubURL 聚合登录中心地址（/oauth/authorize、/api/oauth/token 等）
	//   - SSOClientID / SSOClientSecret 中心为本站签发的客户端凭据
	SSOSiteURL      string
	SSOAppBase      string
	SSOHubURL       string
	SSOClientID     string
	SSOClientSecret string
	// 服务端统一 AI 代理（OpenAI 兼容 /chat/completions）：
	// BaseURL 填完整端点，APIKey 只存在服务端，Model 为默认模型。
	AIBaseURL        string
	AIAPIKey         string
	AIModel          string
	AITimeoutSeconds int
	// AIAllowPrivate 允许 AI 端点访问私网（本地 Ollama 等）；默认关闭。
	AIAllowPrivate bool
	// AIEnabled 是否已配置可用的服务端 AI 代理。

	// Web Push（VAPID）；两项都配置才启用
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// Call WebRTC ICE：STUN 默认公开地址；TURN 三项都配才下发（凭据只经 /api/site 给登录用户）。
	Call  CallConfig
	Redis RedisConfig
	SMTP  SMTPConfig
	// TLSCertFile / TLSKeyFile：可选 TLS 证书路径。两项都设置时服务端启用 HTTPS，
	// 否则以明文 HTTP 运行（依赖前置反向代理终止 TLS）。
	TLSCertFile string
	TLSKeyFile  string
}

// CallConfig 一对一 WebRTC 通话的 ICE 配置；TURN 三项都配置才启用。
type CallConfig struct {
	STUNServer string
	TURNServer string
	TURNUser   string
	TURNPass   string
}

// TurnEnabled TURN 凭据完整（credential 仅经 /api/site 下发给登录用户，不写日志）。
func (c CallConfig) TurnEnabled() bool {
	return c.TURNServer != "" && c.TURNUser != "" && c.TURNPass != ""
}

// RedisConfig 可选：配置后限流使用 Redis 共享配额，连接失败自动降级为内存。
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// Enabled 是否配置了 Redis（Addr 为空即禁用）。
func (r RedisConfig) Enabled() bool { return r.Addr != "" }

// SMTPConfig 邮件发送配置；为空时打日志（开发期）。
// Provider=smtp（默认）走原有 SMTP 实现；Provider=api 走通用 HTTP 邮件 API。
type SMTPConfig struct {
	Provider                     string // smtp | api
	Host, Port, User, Pass, From string
	// HTTP API 通道（Provider=api 时必填）：
	// APIURL 形如 https://api.resend.com/emails，APIKey 走 Bearer 头。
	// 只做通用 JSON 投递，不绑定任何厂商私有字段。
	APIURL string
	APIKey string
	// LogResetLink 未配置邮件服务时，是否把完整的重置链接写进日志。
	// 默认仅在非 release 模式开启：日志里出现可用令牌等于把账户接管入口
	// 暴露给所有能看日志的人（含日志采集平台）。
	LogResetLink bool
}

func (c SMTPConfig) Enabled() bool {
	if c.Provider == "api" {
		return c.APIURL != "" && c.APIKey != ""
	}
	return c.Host != ""
}

// LegacyEnabled 旧站迁移功能是否启用：两个配置项都非空才启用。
// 任一为空时迁移相关接口统一返回 503，且不会建立旧库连接（不影响正常启动）。
func (c Config) LegacyEnabled() bool {
	return strings.TrimSpace(c.LegacyAPISecret) != "" && strings.TrimSpace(c.LegacyDBDSN) != ""
}

// SSOEnabled 统一登录是否启用：站点地址、中心地址与客户端凭据都非空才启用。
// 未启用时 /api/auth/sso/* 一律 404，不影响正常启动。
func (c Config) SSOEnabled() bool {
	return strings.TrimSpace(c.SSOSiteURL) != "" &&
		strings.TrimSpace(c.SSOHubURL) != "" &&
		strings.TrimSpace(c.SSOClientID) != "" &&
		strings.TrimSpace(c.SSOClientSecret) != ""
}

// AIEnabled 是否配置了完整的服务端 AI 代理。
func (c Config) AIEnabled() bool {
	return strings.TrimSpace(c.AIBaseURL) != "" &&
		strings.TrimSpace(c.AIAPIKey) != "" &&
		strings.TrimSpace(c.AIModel) != ""
}

// Load 读取环境变量。JWT_SECRET 规则：
//   - 生产（GIN_MODE=release）必须显式设置且 ≥32 字节，否则拒绝启动；
//   - 开发模式缺省时生成随机值并告警（重启即全端失效）。
//
// DB_DSN 规则：无弱默认值，必须显式配置（开发模式同样要求），避免静默以 root:root 连接。
func Load() (Config, error) {
	cfg := Config{
		Port:                getv("PORT", "8080"),
		GinMode:             getv("GIN_MODE", "release"),
		DBDSN:               getv("DB_DSN", ""),
		JWTSecret:           os.Getenv("JWT_SECRET"),
		LogLevel:            getv("LOG_LEVEL", "info"),
		LegacyAPISecret:     getv("LEGACY_API_SECRET", ""),
		LegacyDBDSN:         getv("LEGACY_DB_DSN", ""),
		Migrate:             getv("MIGRATE", "false") == "true",
		PluginMaxSize:       geti("PLUGIN_MAX_SIZE", 1<<20),
		MinClientVersion:    strings.TrimSpace(os.Getenv("MIN_CLIENT_VERSION")),
		LogDir:              getv("LOG_DIR", "logs"),
		DataDir:             getv("DATA_DIR", "data"),
		LogRetainDays:       int(geti("LOG_RETAIN_DAYS", 15)),
		MetricsToken:        getv("METRICS_TOKEN", ""),
		BackupDir:           getv("BACKUP_DIR", ""),
		BackupIntervalHours: int(geti("BACKUP_INTERVAL_HOURS", 0)),
		BackupRetain:        int(geti("BACKUP_RETAIN", 7)),
		MysqldumpPath:       getv("MYSQLDUMP_PATH", "mysqldump"),
		// WebUpdate 站长后台图形化更新开关（默认开）。关闭后 /api/admin/update/*
		// 一律不可用；仅支持 install.sh 的 systemd 部署，Docker / 裸机自动不可用。
		WebUpdate: getBool("WEB_UPDATE", true),
		// 自动更新默认开启；间隔下限 5 分钟，非法值回退 30 分钟
		AutoUpdate:            getBool("AUTO_UPDATE", true),
		AutoUpdateIntervalMin: autoUpdateInterval(),
		// 发行版模式（install.sh 写入 .env）：非空即从 Release 下载预编译包更新
		ReleaseAPIURL:     os.Getenv("RELEASE_API_URL"),
		ReleaseDLURL:      os.Getenv("RELEASE_DL_URL"),
		ReleaseRepoGithub: os.Getenv("RELEASE_REPO_GITHUB"),
		GithubToken:       os.Getenv("GITHUB_TOKEN"),
		// 统一登录（聚合登录中心）：全部配置才启用，见 SSOEnabled
		SSOSiteURL:       os.Getenv("SSO_SITE_URL"),
		SSOAppBase:       getv("SSO_APP_BASE", "/app"),
		SSOHubURL:        os.Getenv("SSO_HUB_URL"),
		SSOClientID:      os.Getenv("SSO_CLIENT_ID"),
		SSOClientSecret:  os.Getenv("SSO_CLIENT_SECRET"),
		AIBaseURL:        os.Getenv("AI_BASE_URL"),
		AIAPIKey:         os.Getenv("AI_API_KEY"),
		AIModel:          getv("AI_MODEL", "gpt-4o-mini"),
		AITimeoutSeconds: int(geti("AI_TIMEOUT_SECONDS", 60)),
		AIAllowPrivate:   getBool("AI_ALLOW_PRIVATE", false),
		VAPIDPublicKey:   getv("VAPID_PUBLIC_KEY", ""),
		VAPIDPrivateKey:  getv("VAPID_PRIVATE_KEY", ""),
		VAPIDSubject:     getv("VAPID_SUBJECT", "mailto:admin@localhost"),
		Call: CallConfig{
			STUNServer: getv("STUN_SERVER", "stun:stun.l.google.com:19302"),
			TURNServer: os.Getenv("TURN_SERVER"),
			TURNUser:   os.Getenv("TURN_USER"),
			TURNPass:   os.Getenv("TURN_PASS"),
		},
		Redis: RedisConfig{
			Addr:     os.Getenv("REDIS_ADDR"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       int(geti("REDIS_DB", 0)),
		},
		SMTP: SMTPConfig{
			Provider: getv("MAIL_PROVIDER", "smtp"),
			APIURL:   os.Getenv("MAIL_API_URL"),
			APIKey:   os.Getenv("MAIL_API_KEY"),
			Host:     os.Getenv("SMTP_HOST"),
			Port:     getv("SMTP_PORT", "587"),
			User:     os.Getenv("SMTP_USER"),
			Pass:     os.Getenv("SMTP_PASS"),
			From:     getv("SMTP_FROM", "modernchat@localhost"),
			// 生产默认不把可用令牌写进日志；本地开发保留便利
			LogResetLink: getBool("LOG_RESET_LINK", getv("GIN_MODE", "release") != "release"),
		},
		TLSCertFile: os.Getenv("TLS_CERT_FILE"),
		TLSKeyFile:  os.Getenv("TLS_KEY_FILE"),
	}
	// 前端来源：逗号分隔，用于 WS Origin 白名单与找回密码链接。
	// 必须先 TrimSpace 再 TrimSuffix：配置项写成 "https://a.com/, https://b.com/ " 时，
	// 第二个值的尾部是空格，若先 TrimSuffix 会漏掉斜杠 —— 浏览器 Origin 不带尾斜杠，
	// 残留的斜杠会让 WS 握手被 Origin 白名单拒绝。
	for _, u := range strings.Split(getv("FRONTEND_URL", "http://localhost:5173"), ",") {
		if u = strings.TrimSuffix(strings.TrimSpace(u), "/"); u != "" {
			cfg.FrontendURLs = append(cfg.FrontendURLs, u)
		}
	}
	// 解析后为空（例如 FRONTEND_URL="," / 全空白项）：WS Origin 白名单会拒绝
	// 所有浏览器来源，找回密码链接也取不到 prefix（曾导致 index out of range panic）。
	// 启动时直接报错，让部署者知道配置有问题。
	if len(cfg.FrontendURLs) == 0 {
		return cfg, fmt.Errorf("FRONTEND_URL 解析后为空，请至少配置一个前端来源（如 http://localhost:5173）")
	}
	// 可信反向代理：默认只信任本机回环（nginx 同机部署）。
	// 修复缺陷：gin 默认信任所有代理，攻击者伪造 X-Forwarded-For 即可换 IP，
	// 绕过按 IP 的登录/注册限流。显式设为空串（TRUSTED_PROXIES=）表示不信任任何代理。
	if v, ok := os.LookupEnv("TRUSTED_PROXIES"); ok {
		if v = strings.TrimSpace(v); v == "" {
			cfg.TrustedProxies = []string{}
		} else {
			for _, p := range strings.Split(v, ",") {
				if p = strings.TrimSpace(p); p != "" {
					cfg.TrustedProxies = append(cfg.TrustedProxies, p)
				}
			}
		}
	} else {
		cfg.TrustedProxies = []string{"127.0.0.1", "::1"}
	}
	if len(cfg.JWTSecret) < 32 {
		if cfg.JWTSecret != "" {
			return cfg, fmt.Errorf("JWT_SECRET 长度不足 32 字节")
		}
		if cfg.GinMode == "release" {
			return cfg, fmt.Errorf("生产模式（GIN_MODE=release）必须显式设置 JWT_SECRET（≥32 字节，如 openssl rand -hex 32）")
		}
		cfg.JWTSecret = randomHex(32)
		fmt.Fprintln(os.Stderr, "WARN: JWT_SECRET 未设置，已生成随机值（重启后所有登录态失效；仅限本地开发）")
	}
	// 生产模式开启 LOG_RESET_LINK 会把含可用令牌的重置链接写入日志，
	// 任何能读日志的人都能拿到该链接重置密码。默认强制关闭；确需开启
	// （如临时排查）必须显式设置 ALLOW_RESET_LINK_IN_LOGS=true。
	if cfg.GinMode == "release" && cfg.SMTP.LogResetLink {
		if os.Getenv("ALLOW_RESET_LINK_IN_LOGS") != "true" {
			cfg.SMTP.LogResetLink = false
			fmt.Fprintln(os.Stderr, "WARN: 生产模式已强制关闭 LOG_RESET_LINK（含可用令牌的重置链接会写入日志）；确需开启请显式设置 ALLOW_RESET_LINK_IN_LOGS=true 并限制日志访问。")
		} else {
			fmt.Fprintln(os.Stderr, "WARN: 生产模式 LOG_RESET_LINK=true 且已显式允许，日志将含可用密码重置链接，请确保日志访问权限受控。")
		}
	}
	// DB_DSN 无弱默认值：未显式设置时拒绝启动，避免静默以 root:root 等弱凭据连接数据库
	if strings.TrimSpace(cfg.DBDSN) == "" {
		return cfg, fmt.Errorf("必须显式设置 DB_DSN 环境变量（连接字符串，如 root:密码@tcp(127.0.0.1:3306)/modernchat）")
	}
	return cfg, nil
}

func getv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func geti(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// autoUpdateInterval 自动更新检查间隔：下限 5 分钟（GitHub 匿名 API 60 次/小时，
// 低于 5 分钟容易在多个实例上叠加触发限流）；非法值回退 30 分钟。
func autoUpdateInterval() int {
	const def = 30
	n := geti("AUTO_UPDATE_INTERVAL", def)
	if n < 5 || n > 1440 {
		return def
	}
	return int(n)
}

// getBool 解析布尔环境变量；空值或无法识别时回退 def。
func getBool(key string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}
