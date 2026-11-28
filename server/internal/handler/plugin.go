package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// PluginHandler 插件安装 / 管理。
type PluginHandler struct {
	cfg     config.Config
	plugins *store.PluginStore
	audits  *store.AuditStore
	// pluginData 卸载插件时清理该插件的用户数据（可为 nil：测试环境）
	pluginData *store.PluginDataStore
}

func NewPluginHandler(cfg config.Config, plugins *store.PluginStore, audits *store.AuditStore) *PluginHandler {
	return &PluginHandler{cfg: cfg, plugins: plugins, audits: audits}
}

// SetPluginData 注入插件数据存储，卸载时连带清理私有 KV。
func (h *PluginHandler) SetPluginData(data *store.PluginDataStore) { h.pluginData = data }

// List 我的插件。
func (h *PluginHandler) List(c *gin.Context) {
	rows, err := h.plugins.ByUser(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	out := make([]model.PluginView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].View())
	}
	c.JSON(http.StatusOK, out)
}

// Active 启动拉取的启用清单。
func (h *PluginHandler) Active(c *gin.Context) {
	rows, err := h.plugins.Active(middleware.UserFrom(c).ID)
	if err != nil {
		internal(c, err)
		return
	}
	out := make([]model.PluginView, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].View())
	}
	c.JSON(http.StatusOK, out)
}

// Code 下发插件 JS 源码。
func (h *PluginHandler) Code(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	me := middleware.UserFrom(c)
	if p == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	if p.GlobalDisabled {
		fail(c, http.StatusForbidden, "插件已被站长下架")
		return
	}
	c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(p.CodeText))
}

// Upload 上传插件文件安装（application/octet-stream，≤PLUGIN_MAX_SIZE）。
// `?lang=ts`（或文件名 .ts）时先转译成 JS 再入库；缺省按源码特征自动判断。
func (h *PluginHandler) Upload(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, h.cfg.PluginMaxSize+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取上传内容失败")
		return
	}
	if int64(len(body)) > h.cfg.PluginMaxSize {
		fail(c, http.StatusRequestEntityTooLarge, "插件文件超过 1MB")
		return
	}
	h.createFromCode(c, string(body), "upload", c.Query("lang"))
}

// FromURL URL 安装（服务端拉取前执行 SSRF 防护）。
func (h *PluginHandler) FromURL(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if !bind(c, &req) {
		return
	}
	code, err := plugin.Fetch(req.URL, h.cfg.PluginMaxSize)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalid) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		fail(c, http.StatusBadRequest, "插件拉取失败")
		return
	}
	// 按 URL 路径后缀判断语言：xxx.ts 直接当 TypeScript 转译
	lang := plugin.LangFromFilename(req.URL)
	h.createFromCode(c, code, "url", lang)
}

// Validate 干跑校验（开发工具用）：编译 + 清单校验，但不落库、不改任何状态。
//
// 单独开这个接口是为了让 scripts/dev-plugin.mjs 与服务端用同一套规则：两处
// 各写一份校验必然漂移，作者会陷入「本地通过、安装 400」的试错。返回 200 +
// 结构化结果（而不是 400）是为了让 CLI 一次列清全部问题，而不是修一条报一条。
func (h *PluginHandler) Validate(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, h.cfg.PluginMaxSize+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取上传内容失败")
		return
	}
	if int64(len(body)) > h.cfg.PluginMaxSize {
		fail(c, http.StatusRequestEntityTooLarge, "插件文件超过 1MB")
		return
	}
	compiled, m, err := plugin.Compile(string(body), c.Query("lang"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "errors": []string{err.Error()}})
		return
	}
	resp := gin.H{"ok": true, "compiledBytes": len(compiled), "manifest": m}
	// SDK 不满足等于装不上，按错误报告；依赖未就绪只是「现在还不能启用」，
	// 安装本来允许先装使用者，所以放在 notes 里，不判失败。
	if eErr := m.CheckEngines(plugin.SDKLevel); eErr != nil {
		resp["ok"] = false
		resp["errors"] = []string{eErr.Error()}
	}
	if unmet := m.CheckDependencies(h.depLookup(middleware.UserFrom(c).ID)); len(unmet) > 0 {
		resp["notes"] = []string{"依赖尚未就绪（安装不受影响，启用前需装好并启用）：" + strings.Join(unmet, "、")}
	}
	c.JSON(http.StatusOK, resp)
}

// depLookup 把「当前用户的已安装插件」折成依赖查询函数。
// 一次列表查询代替逐条查：依赖上限 10 条，但没必要换 10 次库往返。
func (h *PluginHandler) depLookup(userID uint) func(string) plugin.DepState {
	rows, err := h.plugins.ByUser(userID)
	if err != nil {
		slog.Warn("plugin deps lookup failed", "uid", userID, "err", err.Error())
		return func(string) plugin.DepState { return plugin.DepState{} }
	}
	byName := make(map[string]plugin.DepState, len(rows))
	for _, row := range rows {
		v, _ := plugin.ParseVersion(row.Version)
		byName[row.Name] = plugin.DepState{Found: true, Enabled: row.Enabled, Version: v}
	}
	return func(name string) plugin.DepState { return byName[name] }
}

// decodeStringList 解析用 JSON 数组串存储的列（历史行为空串，按空列表处理）。
func decodeStringList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func (h *PluginHandler) createFromCode(c *gin.Context, code string, source string, lang string) {
	compiled, m, err := plugin.Compile(code, lang)
	if err != nil {
		if errors.Is(err, plugin.ErrInvalid) {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		internal(c, err)
		return
	}
	code = compiled
	// SDK 兼容性是「相对当前宿主」的判断，必须在落库前挡住；不满足的插件装上
	// 只会到处抛 undefined，让作者以为是自己的代码问题。
	if err := m.CheckEngines(plugin.SDKLevel); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	perms, _ := json.Marshal(m.Permissions)
	hosts, _ := json.Marshal(m.AllowedHosts)
	engines, _ := json.Marshal(m.Engines)
	deps, _ := json.Marshal(m.Dependencies)
	me := middleware.UserFrom(c).ID
	detail := fmt.Sprintf("version=%s; perms=[%s]; hosts=[%s]; source=%s",
		m.Version, strings.Join(m.Permissions, ","), strings.Join(m.AllowedHosts, ","), source)
	if len(m.Dependencies) > 0 {
		detail += "; deps=[" + strings.Join(m.Dependencies, ",") + "]"
	}

	// 同名插件已安装：视为升级覆盖（保留 id / 启停状态 / 全局下架状态）
	if existing, err := h.plugins.ByUserAndName(me, m.Name); err != nil {
		internal(c, err)
		return
	} else if existing != nil {
		// 权限 / 域名白名单与旧版不同时，旧授权不能默认延续到新代码上。
		// 服务端在这里直接停用：只靠前端弹窗再补一次 PATCH 的话，从代码落库
		// 到用户点「取消」之间，任何会话的 Active 拉取都会拿到新代码并执行。
		// 停用后新代码只有在用户主动重新启用（SetEnabled 会复查依赖）时才运行；
		// 代价是旧代码也同步停止，真正的「保留旧版运行、确认后原子换入」需要
		// pending 暂存列与确认接口，属于模型/路由层的改动。
		permsChanged := !jsonSetEqual(existing.Permissions, string(perms)) ||
			!jsonSetEqual(existing.AllowedHosts, string(hosts))
		detail = fmt.Sprintf("version=%s->%s; %s", existing.Version, m.Version, detail)
		// 旧版本号随响应头带回：前端确认框展示「v旧 → v新」过渡，降级时额外警告
		oldVersion := existing.Version
		existing.Version = m.Version
		existing.Description = m.Description
		existing.CodeText = code
		existing.Permissions = string(perms)
		existing.AllowedHosts = string(hosts)
		existing.Engines = string(engines)
		existing.Dependencies = string(deps)
		if permsChanged {
			existing.Enabled = false
			detail += "; disabled=perms-changed"
		}
		// 升级后依赖可能不再满足（新版本新增了依赖、或依赖被停用/降级）。
		// 此时必须停用：让新代码在缺少依赖的环境里继续跑，只会在运行时炸，
		// 而且用户会以为是升级把它弄坏了。前端确认框会展示「依赖未就绪」，
		// 依赖装好后一键重新启用即可。
		if existing.Enabled {
			if depList, dErr := plugin.ParseDependencies(m.Dependencies, m.Name); dErr == nil {
				if unmet := plugin.UnmetDependencies(depList, h.depLookup(me)); len(unmet) > 0 {
					existing.Enabled = false
					detail += "; disabled=deps-unmet[" + strings.Join(unmet, ",") + "]"
				}
			}
		}
		if err := h.plugins.Save(existing); err != nil {
			internal(c, err)
			return
		}
		h.audit(c, "plugin_update", existing.ID, m.Name, detail)
		slog.Info("plugin updated",
			"uid", me, "plugin", existing.ID, "name", m.Name, "version", m.Version,
			"permsChanged", permsChanged, "source", source)
		c.Header("X-Plugin-Updated", "true")
		c.Header("X-Plugin-Old-Version", oldVersion)
		if permsChanged {
			c.Header("X-Plugin-Perms-Changed", "true")
		}
		c.JSON(http.StatusOK, existing.View())
		return
	}

	// 新建记录必须继承同名插件的全局下架状态，
	// 否则「站长下架 → 用户卸载 → 重新安装」即可绕过下架（新记录默认 not disabled）。
	globalDisabled, err := h.plugins.GlobalDisabledByName(m.Name)
	if err != nil {
		internal(c, err)
		return
	}

	p := &model.Plugin{
		Name:           m.Name,
		Version:        m.Version,
		Description:    m.Description,
		CodeText:       code,
		Permissions:    string(perms),
		AllowedHosts:   string(hosts),
		Engines:        string(engines),
		Dependencies:   string(deps),
		Enabled:        false,
		GlobalDisabled: globalDisabled,
		InstalledBy:    me,
	}
	if err := h.plugins.Create(p); err != nil {
		internal(c, err)
		return
	}
	slog.Info("plugin installed",
		"uid", me, "plugin", p.ID, "name", p.Name, "version", p.Version,
		"perms", len(m.Permissions), "hosts", len(m.AllowedHosts), "source", source,
		"globalDisabled", p.GlobalDisabled)
	h.audit(c, "plugin_install", p.ID, p.Name, detail)
	c.JSON(http.StatusOK, p.View())
}

// audit 记录插件安装 / 升级到审计表（与站长操作同表）：审计的动作语义是
// 「谁在什么时候装了什么权限的什么代码」，这是站长全局下架能发挥作用的前提。
// 写失败只打日志，不影响安装主流程。
func (h *PluginHandler) audit(c *gin.Context, action string, targetID uint, targetName, detail string) {
	me := middleware.UserFrom(c)
	if me == nil {
		return
	}
	if err := h.audits.Record(store.AuditEntry{
		AdminID:    me.ID,
		Action:     action,
		TargetType: "plugin",
		TargetID:   targetID,
		TargetName: targetName,
		Detail:     detail,
		IP:         c.ClientIP(),
	}); err != nil {
		slog.Warn("plugin audit write failed", "action", action, "target", targetID, "err", err.Error())
	}
}

// jsonSetEqual 比较两个 JSON 字符串数组是否为同一集合（顺序无关）：
// permissions / allowedHosts 在库里按 JSON 存储，集合相同即视为未变化。
func jsonSetEqual(a, b string) bool {
	var av, bv []string
	_ = json.Unmarshal([]byte(a), &av)
	_ = json.Unmarshal([]byte(b), &bv)
	if len(av) != len(bv) {
		return false
	}
	sort.Strings(av)
	sort.Strings(bv)
	for i := range av {
		if av[i] != bv[i] {
			return false
		}
	}
	return true
}

// SetEnabled 启用 / 禁用（仅本人）。
func (h *PluginHandler) SetEnabled(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !bind(c, &req) {
		return
	}
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	me := middleware.UserFrom(c)
	if p == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	if p.GlobalDisabled && req.Enabled {
		fail(c, http.StatusForbidden, "插件已被站长下架")
		return
	}
	// 依赖检查放在启用而不是安装：一次导入多个插件时，使用者可能先落库。
	// 只要依赖未就绪（缺失 / 版本不符 / 装了没开），就不让启用——否则插件会在
	// 运行时对着 undefined 崩，作者很难判断是依赖的问题。
	if req.Enabled {
		deps, dErr := plugin.ParseDependencies(decodeStringList(p.Dependencies), p.Name)
		if dErr != nil {
			fail(c, http.StatusBadRequest, dErr.Error())
			return
		}
		if unmet := plugin.UnmetDependencies(deps, h.depLookup(me.ID)); len(unmet) > 0 {
			fail(c, http.StatusForbidden, "依赖未就绪："+strings.Join(unmet, "、"))
			return
		}
	}
	p.Enabled = req.Enabled
	if err := h.plugins.Save(p); err != nil {
		internal(c, err)
		return
	}
	slog.Info("plugin toggled", "uid", me.ID, "plugin", p.ID, "name", p.Name, "enabled", p.Enabled)
	c.JSON(http.StatusOK, p.View())
}

// Remove 卸载（仅本人）。
func (h *PluginHandler) Remove(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	me := middleware.UserFrom(c)
	if err := h.plugins.Delete(uint(id), me.ID); err != nil {
		internal(c, err)
		return
	}
	if h.pluginData != nil {
		if err := h.pluginData.ClearForPlugin(uint(id)); err != nil {
			slog.Warn("plugin data cleanup failed", "plugin", id, "err", err.Error())
		}
	}
	slog.Info("plugin uninstalled", "uid", me.ID, "plugin", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
