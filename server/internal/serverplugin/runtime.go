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

// PageView 下发给登录用户的插件页面声明。admin scope 由前端按角色过滤，
// 服务端只负责下发已启用插件的全部页面。
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
}

type routeKey struct {
	plugin string
	method string
	path   string
}

type subscription struct {
	plugin   string
	endpoint string
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

// Runtime 服务端插件运行时：从 DB 加载启用插件，提供页面/路由查询、事件广播与定时任务。
// 所有出站均为异步，失败只记日志，不影响主流程。
type Runtime struct {
	secret string
	store  *store.ServerPluginStore
	kv     *store.ServerPluginKVStore
	ai     AIConfig
	snap   atomic.Value // *snapshot
	jobsMu sync.Mutex
	jobs   []scheduledJob
}

// SetKVStore 注入服务端插件 KV（WASM 宿主 kv.*）。
func (r *Runtime) SetKVStore(kv *store.ServerPluginKVStore) { r.kv = kv }

// SetAIConfig 注入服务端统一 AI 代理配置（WASM 宿主 ai.chat）。
func (r *Runtime) SetAIConfig(ai AIConfig) { r.ai = ai }

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
				snap.routes[routeKey{plugin: row.Name, method: method, path: rt.Path}] =
					RouteResult{Response: rt.Response, Proxy: rt.Proxy, Handler: rt.Handler, Public: rt.Public}
			}
			for _, ev := range m.Events {
				snap.events[ev.Event] = append(snap.events[ev.Event], subscription{
					plugin: row.Name, endpoint: ev.Endpoint,
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
			status, resp, err := plugin.SafePostJSONWithHeaders(target.endpoint, body, headers, outboundMaxBytes)
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
			status, resp, err := plugin.SafePostJSONWithHeaders(job.endpoint, job.body, headers, outboundMaxBytes)
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
