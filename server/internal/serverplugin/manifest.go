// Package serverplugin 管理员安装的服务端声明式插件：manifest 解析校验与运行时编排。
// 不在服务端执行插件 JS：插件只能声明页面、静态响应、代理、事件订阅与定时任务。
package serverplugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
)

// ErrInvalid manifest 非法（可直接 400 展示）。
var ErrInvalid = errors.New("invalid server plugin manifest")

const (
	maxItems       = 20
	maxPageBody    = 64 * 1024
	maxNameChars   = 30
	maxJobInterval = 86400 // 秒；避免 time.Duration 溢出，也防止分钟级刷屏
)

var (
	versionRe   = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	pageIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,60}$`)
	routePathRe = regexp.MustCompile(`^/[A-Za-z0-9/_.-]*$`)
)

// Manifest 服务端插件声明。
type Manifest struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Runtime      string   `json:"runtime"`     // declarative（默认）| wasm
	Permissions  []string `json:"permissions"` // host 能力：auth:qr / ai:chat / server:message / server:conv / server:user
	Pages        []Page   `json:"pages"`
	Routes       []Route  `json:"routes"`
	Events       []Event  `json:"events"`
	Jobs         []Job    `json:"jobs"`
	AllowedHosts []string `json:"allowedHosts"`
	// Dependencies 依赖的其它服务端插件（名字 + 版本范围），与用户插件共用
	// plugin.ParseDependencies 的语法。安装不阻断，管理员启用时才检查：
	// 一次导入多个插件时，被依赖者可能后装。
	Dependencies []string `json:"dependencies"`
}

// Page 注入宿主界面的页面。scope=admin 的页面由 Pages 接口按角色过滤，
// 普通用户不会拿到它的 body（其中可能含仅管理员该看到的操作入口）。
type Page struct {
	ID    string          `json:"id"`
	Title string          `json:"title"`
	Icon  string          `json:"icon"`
	Scope string          `json:"scope"`
	Body  json.RawMessage `json:"body"`
}

// Route 动态接口：response 为静态响应，proxy 为上游转发，二选一。
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	// Scope 声明调用者角色：user（缺省，任何登录用户）| admin（仅站长）。
	// admin 页面上的 action 落到这里，必须显式声明 scope=admin，否则普通用户
	// 拿到页面 body 后仍可直接 POST /api/ext/... 触发管理动作。
	Scope    string         `json:"scope,omitempty"`
	Response *RouteResponse `json:"response,omitempty"`
	Proxy    string         `json:"proxy,omitempty"`
	Handler  bool           `json:"handler,omitempty"` // runtime=wasm：交给 WASM 处理
	Public   bool           `json:"public,omitempty"`  // 免登录（扫码首屏/第三方回调）
}

// RouteResponse 静态响应规范。
type RouteResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

// Event 事件订阅（当前仅允许 message:new）。
type Event struct {
	Event    string `json:"event"`
	Endpoint string `json:"endpoint"`
}

// Job 定时任务。
type Job struct {
	Name         string          `json:"name"`
	Endpoint     string          `json:"endpoint"`
	EverySeconds int             `json:"everySeconds"`
	Body         json.RawMessage `json:"body"`
}

// ParseManifest 解析并校验 manifest 原文（就是 admin 安装接口的请求体）。
func ParseManifest(raw []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%w: 不是合法 JSON: %v", ErrInvalid, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate 全量校验：名称/版本规则与用户插件一致，数量、路径、域名与体积有硬上限。
func (m *Manifest) Validate() error {
	if n := len([]rune(m.Name)); n < 1 || n > maxNameChars {
		return fmt.Errorf("%w: name 需为 1-%d 个字符", ErrInvalid, maxNameChars)
	}
	if !versionRe.MatchString(m.Version) {
		return fmt.Errorf("%w: version 需为 x.y.z 形式", ErrInvalid)
	}
	if len([]rune(m.Description)) > 255 {
		return fmt.Errorf("%w: description 过长", ErrInvalid)
	}
	if m.Runtime == "" {
		m.Runtime = "declarative"
	}
	if m.Runtime != "declarative" && m.Runtime != "wasm" {
		return fmt.Errorf("%w: runtime 只能是 declarative 或 wasm", ErrInvalid)
	}
	for _, p := range m.Permissions {
		switch p {
		case "auth:qr", "ai:chat", "server:message", "server:conv", "server:user":
		default:
			return fmt.Errorf("%w: 未知服务端宿主权限 %q", ErrInvalid, p)
		}
	}
	if len(m.Pages) > maxItems || len(m.Routes) > maxItems ||
		len(m.Events) > maxItems || len(m.Jobs) > maxItems {
		return fmt.Errorf("%w: pages/routes/events/jobs 各最多 %d 项", ErrInvalid, maxItems)
	}
	if len(m.AllowedHosts) > maxItems {
		return fmt.Errorf("%w: allowedHosts 最多 %d 项", ErrInvalid, maxItems)
	}
	// 先校验白名单本身，再校验引用它的 endpoint/proxy。
	for _, h := range m.AllowedHosts {
		if !plugin.ValidHost(h) {
			return fmt.Errorf("%w: allowedHosts 含非法域名 %q", ErrInvalid, h)
		}
	}
	// 依赖语法与用户插件完全共用一套解析（自依赖 / 重复 / 版本约束在其中拒掉），
	// 具体是否满足留到管理员启用时判断。
	if _, err := plugin.ParseDependencies(m.Dependencies, m.Name); err != nil {
		return err
	}

	for i, p := range m.Pages {
		if !pageIDRe.MatchString(p.ID) {
			return fmt.Errorf("%w: pages[%d].id 需为 1-60 位字母数字下划线连字符", ErrInvalid, i)
		}
		if n := len([]rune(p.Title)); n < 1 || n > 60 {
			return fmt.Errorf("%w: pages[%d].title 需为 1-60 个字符", ErrInvalid, i)
		}
		if len([]rune(p.Icon)) > 120 {
			return fmt.Errorf("%w: pages[%d].icon 过长", ErrInvalid, i)
		}
		if p.Scope != "user" && p.Scope != "admin" {
			return fmt.Errorf("%w: pages[%d].scope 只能是 user 或 admin", ErrInvalid, i)
		}
		if len(p.Body) > maxPageBody {
			return fmt.Errorf("%w: pages[%d].body 超过 64KB", ErrInvalid, i)
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(p.Body, &arr); err != nil {
			return fmt.Errorf("%w: pages[%d].body 必须是 JSON 数组", ErrInvalid, i)
		}
	}

	for i, rt := range m.Routes {
		switch strings.ToUpper(rt.Method) {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return fmt.Errorf("%w: routes[%d].method 不合法", ErrInvalid, i)
		}
		if !routePathRe.MatchString(rt.Path) || strings.Contains(rt.Path, "..") || strings.Contains(rt.Path, "//") {
			return fmt.Errorf("%w: routes[%d].path 需以 / 开头且只含安全字符", ErrInvalid, i)
		}
		if rt.Scope != "" && rt.Scope != "user" && rt.Scope != "admin" {
			return fmt.Errorf("%w: routes[%d].scope 只能是 user 或 admin", ErrInvalid, i)
		}
		// admin-only 与免登录互斥：public 路由没有任何身份，不能承载管理动作。
		if rt.Scope == "admin" && rt.Public {
			return fmt.Errorf("%w: routes[%d] 不能同时声明 scope=admin 与 public", ErrInvalid, i)
		}
		handlers := 0
		if rt.Response != nil {
			handlers++
		}
		if rt.Proxy != "" {
			handlers++
		}
		if rt.Handler {
			handlers++
		}
		if handlers != 1 {
			return fmt.Errorf("%w: routes[%d] 必须且只能配置 response / proxy / handler 之一", ErrInvalid, i)
		}
		if rt.Handler && m.Runtime != "wasm" {
			return fmt.Errorf("%w: routes[%d].handler 仅 runtime=wasm 可用", ErrInvalid, i)
		}
		if rt.Response != nil {
			if rt.Response.Status < 100 || rt.Response.Status > 599 {
				return fmt.Errorf("%w: routes[%d].response.status 不合法", ErrInvalid, i)
			}
			if !json.Valid(rt.Response.Body) || len(rt.Response.Body) > maxPageBody {
				return fmt.Errorf("%w: routes[%d].response.body 需为合法 JSON 且不超过 64KB", ErrInvalid, i)
			}
		}
		if rt.Proxy != "" {
			if err := checkEndpoint(rt.Proxy, m.AllowedHosts); err != nil {
				return fmt.Errorf("%w: routes[%d].proxy %v", ErrInvalid, i, err)
			}
		}
	}

	for i, ev := range m.Events {
		if ev.Event != "message:new" {
			return fmt.Errorf("%w: events[%d].event 仅支持 message:new", ErrInvalid, i)
		}
		if err := checkEndpoint(ev.Endpoint, m.AllowedHosts); err != nil {
			return fmt.Errorf("%w: events[%d].endpoint %v", ErrInvalid, i, err)
		}
	}

	for i, j := range m.Jobs {
		if n := len([]rune(j.Name)); n < 1 || n > 60 {
			return fmt.Errorf("%w: jobs[%d].name 需为 1-60 个字符", ErrInvalid, i)
		}
		if err := checkEndpoint(j.Endpoint, m.AllowedHosts); err != nil {
			return fmt.Errorf("%w: jobs[%d].endpoint %v", ErrInvalid, i, err)
		}
		if j.EverySeconds <= 0 || j.EverySeconds > maxJobInterval {
			return fmt.Errorf("%w: jobs[%d].everySeconds 需在 1-%d 之间", ErrInvalid, i, maxJobInterval)
		}
		if len(j.Body) > maxPageBody {
			return fmt.Errorf("%w: jobs[%d].body 超过 64KB", ErrInvalid, i)
		}
		if len(j.Body) == 0 {
			j.Body = json.RawMessage("{}")
			m.Jobs[i] = j
		} else if !json.Valid(j.Body) {
			return fmt.Errorf("%w: jobs[%d].body 需为合法 JSON", ErrInvalid, i)
		}
	}
	return nil
}

// checkEndpoint 要求 https、无 userinfo，且主机命中 allowedHosts
// （支持 "*.example.com" 通配子域）。复用 plugin.ValidHost 的域名规则。
func checkEndpoint(raw string, allowed []string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return errors.New("必须是 https 且不能携带用户名密码")
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range allowed {
		h = strings.ToLower(strings.TrimSpace(h))
		if strings.HasPrefix(h, "*.") {
			base := h[2:]
			if host == base || strings.HasSuffix(host, "."+base) {
				return nil
			}
		} else if host == h {
			return nil
		}
	}
	return fmt.Errorf("域名 %s 不在 allowedHosts 中", host)
}

// CheckDependencies 按「已安装的服务端插件」检查依赖，返回未满足项（空表示满足）。
// 服务端插件同样允许先装后启用，所以调用点在启停接口，不在安装接口。
func (m *Manifest) CheckDependencies(lookup func(string) plugin.DepState) []string {
	deps, err := plugin.ParseDependencies(m.Dependencies, m.Name)
	if err != nil {
		// 清单在 Validate 阶段已拒过非法依赖，这里只防御调用顺序变化。
		return []string{err.Error()}
	}
	return plugin.UnmetDependencies(deps, lookup)
}
