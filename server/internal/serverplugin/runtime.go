package serverplugin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// 出站上限：事件/定时任务响应读 64KB 足够记录错误，防止恶意端点用超大响应撑爆内存。
const outboundMaxBytes = 64 * 1024

// PageView 下发给登录用户的插件页面声明。admin scope 由 Pages handler
// 按当前用户角色过滤后再下发，客户端渲染层仍做一次兜底隐藏。
type PageView struct {
	// PluginID 即 manifest 的 name；Plugin 为兼容别名，两者取值相同。
	PluginID string          `json:"pluginId"`
	Plugin   string          `json:"plugin"`
	Version  string          `json:"version"`
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Icon     string          `json:"icon"`
	Scope    string          `json:"scope"`
	Body     json.RawMessage `json:"body"`
}

// RouteResult MatchRoute 的结果：静态响应、代理 URL 或 WASM 处理器。
type RouteResult struct {
	Response *RouteResponse `json:"response,omitempty"`
	Proxy    string         `json:"proxy,omitempty"`
	Handler  bool           `json:"handler,omitempty"`
	Public   bool           `json:"public,omitempty"`
	// Scope 为 admin 时只允许站长调用（admin 页面上的动作必须声明它）。
	Scope string `json:"scope,omitempty"`
	// AllowedHosts 仅供代理请求的重定向逐跳校验，不属于对外契约。
	AllowedHosts []string `json:"-"`
}

type routeKey struct {
	plugin string
	method string
	path   string
}

type subscription struct {
	plugin       string
	endpoint     string
	allowedHosts []string
}

type wasmPlugin struct {
	id          uint
	name        string
	manifest    *Manifest
	wasm        []byte
	kv          *store.ServerPluginKVStore
	ai          AIConfig
	permissions map[string]bool
}

type snapshot struct {
	pages  []PageView
	routes map[routeKey]RouteResult
	wasm   map[string]*wasmPlugin
	events map[string][]subscription
}

type scheduledJob struct {
	plugin   string
	name     string
	endpoint string
	body     []byte
	every    time.Duration
	nextAt   time.Time
	// allowedHosts 用于 POST 跟随重定向时逐跳复检 manifest 白名单
	allowedHosts []string
}

// AIConfig 服务端统一 AI 代理配置（OpenAI 兼容 /chat/completions）。
type AIConfig struct {
	BaseURL      string
	APIKey       string
	Model        string
	Timeout      time.Duration
	AllowPrivate bool
}

// Enabled 是否配置了可用的 AI 代理。
func (c AIConfig) Enabled() bool { return c.BaseURL != "" && c.APIKey != "" && c.Model != "" }

// 以下是宿主注入给 WASM 插件的轻量业务结构。刻意只暴露展示所需的最小字段集，
// 不直接复用 model.*View，避免插件拿到头像 URL、邮箱、角色之外的敏感列。
// 字段口径与 model.UserBrief / model.ConversationView / model.MemberView 对齐。

// ConvBrief 会话摘要（conv.list 返回）。
type ConvBrief struct {
	ID           uint    `json:"id"`
	Type         int8    `json:"type"`
	Name         *string `json:"name,omitempty"`
	PeerNickname string  `json:"peerNickname,omitempty"` // 单聊对方昵称；群聊为空
	MemberCount  int     `json:"memberCount"`
	Unread       int     `json:"unread"`
}

// MemberBrief 会话成员摘要（conv.members 返回）。
type MemberBrief struct {
	UserID   uint   `json:"userId"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"` // owner | admin | member
}

// UserBrief 用户摘要（user.get / user.search 返回）。
type UserBrief struct {
	ID          uint   `json:"id"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	AvatarColor string `json:"avatarColor"`
}

// HostServices WASM 宿主可注入的业务能力。所有回调都由注入方（组合根）负责
// 权限边界：只能以「当前 WASM 用户」身份读取自己的会话 / 自己所在会话的成员、
// 以自己身份发消息、给自己推通知，不能伪造系统身份或读取他人数据。
// 未注入时对应 host call 返回明确错误，不影响 kv/http/ai/qr 等既有能力。
type HostServices struct {
	// MessageSender 以 from 身份向 convID 发消息；payload 为插件自定义结构化数据
	// （≤32KB，仅实时帧携带，不落库）。
	MessageSender func(from uint, convID uint, content string, msgType string, payload json.RawMessage) error
	// ConvLister 列出 userID 可见的会话。
	ConvLister func(userID uint) ([]ConvBrief, error)
	// ConvMembers 列出 userID 所在会话 convID 的成员；非成员必须拒绝。
	ConvMembers func(userID, convID uint) ([]MemberBrief, error)
	// UserLookup 按 ID 查用户摘要，未找到返回 (nil, nil)。
	UserLookup func(userID uint) (*UserBrief, error)
	// UserSearch 按关键词搜索用户摘要。
	UserSearch func(kw string, limit int) ([]UserBrief, error)
	// NotificationPusher 给 userID 推一条浏览器通知；未配置时为 nil。
	NotificationPusher func(userID uint, title, body string) error
}

// Runtime 服务端插件运行时：从 DB 加载启用插件，提供页面/路由查询、事件广播与定时任务。
// 所有出站均为异步，失败只记日志，不影响主流程。
type Runtime struct {
	secret string
	store  *store.ServerPluginStore
	kv     *store.ServerPluginKVStore
	ai     AIConfig
	host   HostServices // 注入的宿主业务能力（WASM 宿主 server:* / notification.push）
	snap   atomic.Value // *snapshot
	jobsMu sync.Mutex
	jobs   []scheduledJob
}

// SetKVStore 注入服务端插件 KV（WASM 宿主 kv.*）。
func (r *Runtime) SetKVStore(kv *store.ServerPluginKVStore) { r.kv = kv }

// SetAIConfig 注入服务端统一 AI 代理配置（WASM 宿主 ai.chat）。
func (r *Runtime) SetAIConfig(ai AIConfig) { r.ai = ai }

// SetHost 注入宿主业务能力（WASM 宿主 message.send / conv.* / user.* / notification.push）。
// 组合根应在 Reload/Start 之前调用；未注入的回调在 mc_call 里返回「未配置」错误。
func (r *Runtime) SetHost(services HostServices) { r.host = services }

// ClearKV 删除插件全部 KV（卸载时调用）。
func (r *Runtime) ClearKV(pluginID uint) error {
	if r.kv == nil {
		return nil
	}
	return r.kv.Clear(pluginID)
}

// NewRuntime 构造运行时。secret 用 JWT_SECRET（HMAC 签名用），store 可为 nil（测试）。
func NewRuntime(secret string, st *store.ServerPluginStore) *Runtime {
	r := &Runtime{secret: secret, store: st}
	r.snap.Store(emptySnapshot())
	return r
}

func emptySnapshot() *snapshot {
	return &snapshot{
		pages:  make([]PageView, 0),
		routes: make(map[routeKey]RouteResult),
		wasm:   make(map[string]*wasmPlugin),
		events: make(map[string][]subscription),
	}
}

// Reload 从 DB 重新加载所有启用插件，重建页面/路由/事件索引与定时任务下次触发时间。
// manifest 解析失败的插件只告警跳过，不影响其它插件。
func (r *Runtime) Reload() error {
	snap := emptySnapshot()
	jobs := make([]scheduledJob, 0)
	if r.store != nil {
		rows, err := r.store.Enabled()
		if err != nil {
			return err
		}
		now := time.Now()
		for i := range rows {
			row := &rows[i]
			m, err := ParseManifest([]byte(row.ManifestJSON))
			if err != nil {
				slog.Warn("server plugin manifest invalid, skipped",
					"id", row.ID, "name", row.Name, "err", err.Error())
				continue
			}
			wp := &wasmPlugin{
				id: row.ID, name: row.Name, manifest: m, wasm: row.WasmData, kv: r.kv, ai: r.ai,
				permissions: make(map[string]bool, len(m.Permissions)),
			}
			for _, p := range m.Permissions {
				wp.permissions[p] = true
			}
			if m.Runtime == "wasm" {
				if len(row.WasmData) == 0 {
					slog.Warn("server plugin wasm missing, skipped", "id", row.ID, "name", row.Name)
					continue
				}
				snap.wasm[row.Name] = wp
			}
			for _, p := range m.Pages {
				snap.pages = append(snap.pages, PageView{
					PluginID: row.Name, Plugin: row.Name, Version: row.Version,
					ID: p.ID, Title: p.Title, Icon: p.Icon, Scope: p.Scope, Body: p.Body,
				})
			}
			for _, rt := range m.Routes {
				method := strings.ToUpper(rt.Method)
				scope := rt.Scope
				if scope == "" {
					scope = "user"
				}
				snap.routes[routeKey{plugin: row.Name, method: method, path: rt.Path}] =
					RouteResult{
						Response: rt.Response, Proxy: rt.Proxy, Handler: rt.Handler, Public: rt.Public,
						Scope: scope, AllowedHosts: m.AllowedHosts,
					}
			}
			for _, ev := range m.Events {
				snap.events[ev.Event] = append(snap.events[ev.Event], subscription{
					plugin: row.Name, endpoint: ev.Endpoint, allowedHosts: m.AllowedHosts,
				})
			}
			for _, j := range m.Jobs {
				every := time.Duration(j.EverySeconds) * time.Second
				body := j.Body
				if len(body) == 0 {
					body = json.RawMessage("{}")
				}
				jobs = append(jobs, scheduledJob{
					plugin: row.Name, name: j.Name, endpoint: j.Endpoint,
					body: body, every: every, nextAt: now.Add(every),
					allowedHosts: m.AllowedHosts,
				})
			}
		}
	}
	r.snap.Store(snap)
	r.jobsMu.Lock()
	r.jobs = jobs
	r.jobsMu.Unlock()
	return nil
}

// Pages 返回全部启用插件的页面（始终非 nil）。
func (r *Runtime) Pages() []PageView {
	snap, _ := r.snap.Load().(*snapshot)
	if snap == nil {
		return []PageView{}
	}
	out := make([]PageView, len(snap.pages))
	copy(out, snap.pages)
	return out
}

// MatchRoute 按插件名 + 方法 + 路径匹配声明的接口。
func (r *Runtime) MatchRoute(pluginName, method, path string) (RouteResult, bool) {
	snap, _ := r.snap.Load().(*snapshot)
	if snap == nil {
		return RouteResult{}, false
	}
	res, ok := snap.routes[routeKey{plugin: pluginName, method: strings.ToUpper(method), path: path}]
	return res, ok
}

// Emit 异步向所有订阅该事件的插件端点 POST JSON，携带 HMAC 签名头。
// 签名口径：HMAC-SHA256(secret, plugin + "." + event + "." + body)，hex 编码。
func (r *Runtime) Emit(event string, payload any) {
	snap, _ := r.snap.Load().(*snapshot)
	if snap == nil {
		return
	}
	subs := snap.events[event]
	if len(subs) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("server plugin event marshal failed", "event", event, "err", err.Error())
		return
	}
	for _, sub := range subs {
		target := sub
		go func() {
			headers := map[string]string{
				"X-MC-Plugin":    target.plugin,
				"X-MC-Signature": r.sign(target.plugin, event, body),
			}
			status, resp, err := plugin.SafePostJSONWithAllowedHosts(target.endpoint, body, headers, outboundMaxBytes, target.allowedHosts)
			if err != nil {
				slog.Warn("server plugin event delivery failed",
					"plugin", target.plugin, "event", event, "err", err.Error())
				return
			}
			if status < 200 || status >= 300 {
				slog.Warn("server plugin event endpoint rejected",
					"plugin", target.plugin, "event", event, "status", status, "resp", string(resp))
			}
		}()
	}
}

// Start 每 10 秒扫描到期的定时任务并异步触发；ctx 取消时退出。
func (r *Runtime) Start(ctx context.Context) {
	// 启动时兜底加载一次（main 通常已显式 Reload），失败只告警。
	if err := r.Reload(); err != nil {
		slog.Warn("server plugin initial reload failed", "err", err.Error())
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.fireDue(now)
		}
	}
}

// fireDue 取出所有到期任务并推后下次时间（在锁内完成，避免重复触发）。
func (r *Runtime) fireDue(now time.Time) {
	r.jobsMu.Lock()
	due := make([]scheduledJob, 0)
	for i := range r.jobs {
		if !now.Before(r.jobs[i].nextAt) {
			r.jobs[i].nextAt = now.Add(r.jobs[i].every)
			due = append(due, r.jobs[i])
		}
	}
	r.jobsMu.Unlock()

	for _, j := range due {
		job := j
		go func() {
			headers := map[string]string{
				"X-MC-Plugin":    job.plugin,
				"X-MC-Signature": r.sign(job.plugin, "job:"+job.name, job.body),
			}
			status, resp, err := plugin.SafePostJSONWithAllowedHosts(job.endpoint, job.body, headers, outboundMaxBytes, job.allowedHosts)
			if err != nil {
				slog.Warn("server plugin job failed",
					"plugin", job.plugin, "job", job.name, "err", err.Error())
				return
			}
			if status < 200 || status >= 300 {
				slog.Warn("server plugin job endpoint rejected",
					"plugin", job.plugin, "job", job.name, "status", status, "resp", string(resp))
			}
		}()
	}
}

// sign 计算事件 / 任务请求的 HMAC-SHA256 签名（hex）。
func (r *Runtime) sign(pluginName, event string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(r.secret))
	_, _ = mac.Write([]byte(pluginName + "." + event + "."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
