// Package plugin 插件代码的 manifest 解析校验与 URL 安装（SSRF 防护）。
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// ErrInvalid 统一 400 错误（消息可直接展示）。
var ErrInvalid = errors.New("invalid plugin")

// SDKLevel 宿主当前支持的插件 SDK 级别。
//
// 真源是 web/src/composables/plugin/sandbox.ts 里 api.version 的字面量。
// 这里存一份是为了在安装时挡住「插件要求更高 SDK」的清单：旧宿主装上依赖新 API
// 的插件只会在运行时抛 undefined，比安装时报错难查得多。两处必须同提交更新。
const SDKLevel = 5

// Manifest 插件声明，约定写在代码首个 /* {...} */ 块注释里。
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Permissions  []string `json:"permissions"`
	AllowedHosts []string `json:"allowedHosts"`
	// Engines 宿主能力要求，当前只认 sdk 一个键。
	Engines map[string]string `json:"engines"`
	// Dependencies 依赖的同名插件及版本范围。安装不阻断、启用才检查：
	// 先装依赖再装使用者是常见顺序，安装时就拦截会让「一次导入多个插件」没法做。
	Dependencies []string `json:"dependencies"`
}

// sdkEngineKey engines 里唯一合法的键。
const sdkEngineKey = "sdk"

// SDK 权限全集（§8.3）。
var ValidPermissions = map[string]bool{
	"message:read": true, "message:send": true, "message:render": true,
	"ui:panel": true, "storage": true, "net": true, "ui:theme": true,
	// 会话只读数据：当前会话概览 / 成员 / 最近消息（做总结、导出、统计类插件）
	"conv:read": true,
	// 往宿主界面注入动作：消息右键菜单项（输入区钩子尚未提供，不要在这里写）
	"ui:actions": true,
	// 宿主侧动作：跳转会话 / 定位消息 / 轻提示
	"ui:host": true,
	// SDK v3：完整页面 + 用户级服务端数据
	"ui:page": true, "data": true,
	// 服务端统一 AI 代理（密钥不下发到浏览器）
	"ai": true,
	// SDK v5：会话写操作 + 宿主文件上传。前端权限表（web/src/composables/plugin/permissions.ts）
	// 与官方 power-kit 示例已使用这两个权限名，白名单必须同步，否则示例安装即 400。
	"conv:write": true, "file:upload": true,
}

var (
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	hostRe    = regexp.MustCompile(`^(\*\.)?([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// Parse 从插件代码提取并校验 manifest。
func Parse(code string) (*Manifest, error) {
	start := strings.Index(code, "/*")
	if start < 0 {
		return nil, fmt.Errorf("%w: 缺少 manifest 注释", ErrInvalid)
	}
	end := strings.Index(code[start:], "*/")
	if end < 0 {
		return nil, fmt.Errorf("%w: manifest 注释未闭合", ErrInvalid)
	}
	raw := strings.TrimSpace(code[start+2 : start+end])
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("%w: manifest 不是合法 JSON: %v", ErrInvalid, err)
	}
	return &m, m.Validate()
}

// Validate 校验各字段（§4 校验规则）。
func (m *Manifest) Validate() error {
	if n := len([]rune(m.Name)); n < 1 || n > 30 {
		return fmt.Errorf("%w: name 需为 1-30 个字符", ErrInvalid)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("%w: version 需为 x.y.z 形式", ErrInvalid)
	}
	if len([]rune(m.Description)) > 255 {
		return fmt.Errorf("%w: description 过长", ErrInvalid)
	}
	if len(m.Permissions) == 0 {
		return fmt.Errorf("%w: permissions 不能为空", ErrInvalid)
	}
	for _, p := range m.Permissions {
		if !ValidPermissions[p] {
			return fmt.Errorf("%w: 未知权限 %q", ErrInvalid, p)
		}
	}
	if len(m.AllowedHosts) > 20 {
		return fmt.Errorf("%w: allowedHosts 最多 20 项", ErrInvalid)
	}
	for _, h := range m.AllowedHosts {
		if !ValidHost(h) {
			return fmt.Errorf("%w: allowedHosts 含非法域名 %q", ErrInvalid, h)
		}
	}
	if err := m.validateEngines(); err != nil {
		return err
	}
	// 依赖与自依赖/重复的校验放在这里，具体「是否满足」留给启用时的 CheckDependencies：
	// 清单本身合法就允许安装，这对「先装被依赖者」的顺序最友好。
	if _, err := ParseDependencies(m.Dependencies, m.Name); err != nil {
		return err
	}
	return nil
}

// validateEngines 校验 engines 的形状。未知键直接报错而不是忽略：
// engines 是新字段，忽略拼错的键（例如 "sdks"）会让作者以为兼容性检查生效了，
// 实际什么都没校验——宁可装不上，也不要静默失效。
func (m *Manifest) validateEngines() error {
	if len(m.Engines) == 0 {
		return nil
	}
	for key := range m.Engines {
		if key != sdkEngineKey {
			return fmt.Errorf("%w: engines 只支持 %q，未知键 %q", ErrInvalid, sdkEngineKey, key)
		}
	}
	spec, ok := m.Engines[sdkEngineKey]
	if !ok || strings.TrimSpace(spec) == "" {
		return fmt.Errorf("%w: engines 声明了但缺少 %q", ErrInvalid, sdkEngineKey)
	}
	if _, err := ParseRequirement(spec); err != nil {
		return fmt.Errorf("%w: engines.sdk 版本约束非法", ErrInvalid)
	}
	return nil
}

// CheckEngines 校验宿主 SDK 级别是否满足清单要求。
//
// 与 Validate 分开：Validate 只判清单自身合法性（开发工具离线也能跑），
// 兼容性是「相对当前宿主」的判断，只有服务器知道自己的 SDKLevel。
func (m *Manifest) CheckEngines(sdkLevel int) error {
	spec, ok := m.Engines[sdkEngineKey]
	if !ok {
		return nil
	}
	req, err := ParseRequirement(spec)
	if err != nil {
		return err
	}
	host, _ := ParseVersion(fmt.Sprint(sdkLevel))
	if !req.Satisfies(host) {
		return fmt.Errorf("%w: 插件要求 SDK %s，当前宿主为 %s", ErrInvalid, req, host)
	}
	return nil
}

// CheckDependencies 按已安装清单检查依赖；返回未满足项（空表示全部满足）。
func (m *Manifest) CheckDependencies(lookup func(name string) DepState) []string {
	deps, err := ParseDependencies(m.Dependencies, m.Name)
	if err != nil {
		// 清单已在 Validate 阶段拒过非法依赖，这里只防御代码路径顺序变化。
		return []string{err.Error()}
	}
	return UnmetDependencies(deps, lookup)
}

// ValidHost 校验域名（允许 "*.example.com" 通配子域）。
// 导出给服务端插件 manifest 复用同一套域名规则，避免两处各写一份正则。
//
// 拒绝本机 / 私网名字与 IP 字面量：allowedHosts 是插件出站的信任边界，
// 若允许 localhost、*.local、192.168.x.x，插件就能借宿主（或浏览器）的
// 网络位置访问用户内网服务。公网 IP 也一律拒绝——IP 无法与域名做稳定的
// 白名单比较，还可用十进制/八进制写法绕过私网判断。
func ValidHost(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if h == "" || len(h) > 253 || !hostRe.MatchString(h) {
		return false
	}
	if net.ParseIP(h) != nil {
		return false
	}
	base := strings.TrimPrefix(h, "*.")
	return base != "localhost" && base != "local" && !strings.HasSuffix(base, ".localhost") &&
		!strings.HasSuffix(base, ".local")
}
