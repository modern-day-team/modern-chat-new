// Package version 构建版本信息：优先编译期 -ldflags，缺省时读取同名环境变量。
// 这样本地 go run / 直接启动二进制也能在 /api/version 显示真实版本，而不只是 dev·unknown。
package version

import "os"

// 以下变量由构建脚本注入；未注入时 Current 会读取 VERSION / COMMIT / BUILD_TIME 环境变量。
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func envOr(current, fallback, key string) string {
	if current != "" && current != fallback {
		return current
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return current
}

// Info GET /api/version 的响应体。
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"buildTime"`
}

// Current 当前构建信息。
func Current() Info {
	return Info{
		Version:   envOr(Version, "dev", "VERSION"),
		Commit:    envOr(Commit, "unknown", "COMMIT"),
		BuildTime: envOr(BuildTime, "unknown", "BUILD_TIME"),
	}
}
