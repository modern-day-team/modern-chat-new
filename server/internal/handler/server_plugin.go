package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/serverplugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

// ServerPluginHandler 管理员服务端插件 + 登录用户页面/动态路由。
type ServerPluginHandler struct {
	store   *store.ServerPluginStore
	audits  *store.AuditStore
	runtime *serverplugin.Runtime
}

func NewServerPluginHandler(st *store.ServerPluginStore, audits *store.AuditStore, runtime *serverplugin.Runtime) *ServerPluginHandler {
	return &ServerPluginHandler{store: st, audits: audits, runtime: runtime}
}

// List GET /api/admin/server-plugins —— 简化视图，不下发 manifest。
func (h *ServerPluginHandler) List(c *gin.Context) {
	rows, err := h.store.All()
	if err != nil {
		internal(c, err)
		return
	}
	out := make([]model.ServerPluginView, 0, len(rows))
	for i := range rows {
		out = append(out, viewOf(&rows[i]))
	}
	c.JSON(http.StatusOK, out)
}

// Install POST /api/admin/server-plugins —— 请求体是 manifest JSON 原文。
// 同名即升级（保留 id 与启停状态），否则新建且默认停用，需管理员显式启用。
func (h *ServerPluginHandler) Install(c *gin.Context) {
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取请求体失败")
		return
	}
	if len(raw) > 1<<20 {
		fail(c, http.StatusRequestEntityTooLarge, "manifest 超过 1MB")
		return
	}
	m, err := serverplugin.ParseManifest(raw)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	me := middleware.UserFrom(c)
	now := time.Now()
	existing, err := h.store.ByName(m.Name)
	if err != nil {
		internal(c, err)
		return
	}
	action := "server_plugin_install"
	if existing != nil {
		action = "server_plugin_update"
		existing.Version = m.Version
		existing.Description = m.Description
		existing.ManifestJSON = string(raw)
		existing.InstalledBy = me.ID
		existing.UpdatedAt = now
		if err := h.store.Save(existing); err != nil {
			internal(c, err)
			return
		}
		h.audit(c, action, existing.ID, existing.Name, "version="+m.Version)
		h.reload()
		c.JSON(http.StatusOK, viewOf(existing))
		return
	}
	row := &model.ServerPlugin{
		Name: m.Name, Version: m.Version, Description: m.Description,
		ManifestJSON: string(raw), Enabled: false, InstalledBy: me.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := h.store.Create(row); err != nil {
		internal(c, err)
		return
	}
	h.audit(c, action, row.ID, row.Name, "version="+m.Version)
	h.reload()
	c.JSON(http.StatusOK, viewOf(row))
}

// UploadWASM POST /api/admin/server-plugins/wasm?id=:id —— 原始 WASM 二进制。
// 先安装 manifest，再上传 wasm；两者分离，避免把二进制塞进 JSON。
func (h *ServerPluginHandler) UploadWASM(c *gin.Context) {
	id, err := strconv.ParseUint(c.Query("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return
	}
	row, err := h.store.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return
	}
	if row == nil {
		fail(c, http.StatusNotFound, "插件不存在")
		return
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, (8<<20)+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取 WASM 失败")
		return
	}
	if len(data) > 8<<20 {
		fail(c, http.StatusRequestEntityTooLarge, "WASM 超过 8MB")
		return
	}
	if len(data) < 4 || string(data[:4]) != "\x00asm" {
		fail(c, http.StatusBadRequest, "不是有效的 WASM 文件")
		return
	}
	if err := serverplugin.ValidateWASM(c.Request.Context(), data); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	sum := sha256.Sum256(data)
	row.WasmData = data
	row.WasmHash = hex.EncodeToString(sum[:])
	row.UpdatedAt = time.Now()
	if err := h.store.Save(row); err != nil {
		internal(c, err)
		return
	}
	h.audit(c, "server_plugin_wasm_upload", row.ID, row.Name, "size="+strconv.Itoa(len(data)))
	h.reload()
	c.JSON(http.StatusOK, viewOf(row))
}

// SetEnabled PATCH /api/admin/server-plugins/:id body {enabled:bool}
func (h *ServerPluginHandler) SetEnabled(c *gin.Context) {
	row, ok := h.byID(c)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !bind(c, &req) {
		return
	}
	if req.Enabled {
		m, err := serverplugin.ParseManifest([]byte(row.ManifestJSON))
		if err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
		if m.Runtime == "wasm" && len(row.WasmData) == 0 {
			fail(c, http.StatusBadRequest, "WASM 插件尚未上传 wasm 文件")
			return
		}
	}
	row.Enabled = req.Enabled
	row.UpdatedAt = time.Now()
	if err := h.store.Save(row); err != nil {
		internal(c, err)
		return
	}
	h.audit(c, "server_plugin_toggle", row.ID, row.Name, "enabled="+strconv.FormatBool(req.Enabled))
	h.reload()
	c.JSON(http.StatusOK, viewOf(row))
}

// Delete DELETE /api/admin/server-plugins/:id
func (h *ServerPluginHandler) Delete(c *gin.Context) {
	row, ok := h.byID(c)
	if !ok {
		return
	}
	if err := h.store.Delete(row.ID); err != nil {
		internal(c, err)
		return
	}
	if h.runtime != nil {
		if err := h.runtime.ClearKV(row.ID); err != nil {
			slog.Warn("server plugin kv cleanup failed", "plugin", row.Name, "err", err.Error())
		}
	}
	h.audit(c, "server_plugin_delete", row.ID, row.Name, "")
	h.reload()
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Pages GET /api/server-plugins/pages —— 登录用户取启用插件的页面声明。
func (h *ServerPluginHandler) Pages(c *gin.Context) {
	if h.runtime == nil {
		c.JSON(http.StatusOK, []serverplugin.PageView{})
		return
	}
	c.JSON(http.StatusOK, h.runtime.Pages())
}

// Dynamic 处理 /api/ext/:plugin/*path：静态响应、HTTPS 代理或 WASM。
// 只透传 Content-Type 与请求体，绝不转发用户 JWT / Cookie。
func (h *ServerPluginHandler) Dynamic(c *gin.Context) {
	h.dynamic(c, false)
}

// DynamicPublic 处理 /api/ext-public/:plugin/*path：只允许 manifest 标记 public 的路由。
// 扫码首屏、第三方回调等免登录场景使用；未标记 public 的插件路由一律 404。
func (h *ServerPluginHandler) DynamicPublic(c *gin.Context) {
	h.dynamic(c, true)
}

func (h *ServerPluginHandler) dynamic(c *gin.Context, public bool) {
	if h.runtime == nil {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	name := c.Param("plugin")
	path := c.Param("path")
	if path == "" {
		path = "/"
	}
	res, ok := h.runtime.MatchRoute(name, c.Request.Method, path)
	if !ok || (public && !res.Public) || (!public && res.Public && middleware.UserFrom(c) == nil) {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	if res.Handler {
		h.wasm(c, name, path, public)
		return
	}
	if res.Response != nil {
		status := res.Response.Status
		if status < 100 || status > 599 {
			status = http.StatusOK
		}
		c.Data(status, "application/json; charset=utf-8", res.Response.Body)
		return
	}
	if res.Proxy == "" {
		fail(c, http.StatusNotFound, "接口不存在")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取请求体失败")
		return
	}
	headers := make(map[string]string, 1)
	if ct := c.GetHeader("Content-Type"); ct != "" && !strings.ContainsAny(ct, "\r\n") {
		headers["Content-Type"] = ct
	}
	status, resp, err := plugin.SafeRequest(c.Request.Method, res.Proxy, body, headers, 2<<20)
	if err != nil {
		slog.Warn("server plugin proxy failed", "plugin", name, "path", path, "err", err.Error())
		fail(c, http.StatusBadGateway, "上游服务不可用")
		return
	}
	ct := http.DetectContentType(resp)
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}
	c.Data(status, ct, resp)
}

// wasm 把请求交给管理员安装的 WASM 沙箱处理。
func (h *ServerPluginHandler) wasm(c *gin.Context, name, path string, public bool) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, (1<<20)+1))
	if err != nil {
		fail(c, http.StatusBadRequest, "读取请求体失败")
		return
	}
	if len(body) > 1<<20 {
		fail(c, http.StatusRequestEntityTooLarge, "请求体超过 1MB")
		return
	}
	headers := map[string]string{}
	for _, k := range []string{"Content-Type", "Accept"} {
		if v := c.GetHeader(k); v != "" && !strings.ContainsAny(v, "\r\n") {
			headers[k] = v
		}
	}
	req := serverplugin.WASMRequest{
		Kind: "http", Method: c.Request.Method, Path: path,
		Query: c.Request.URL.RawQuery, Headers: headers,
	}
	if len(body) > 0 {
		req.Body = json.RawMessage(body)
	}
	if !public {
		if u := middleware.UserFrom(c); u != nil {
			req.User = &serverplugin.WASMUser{ID: u.ID, Username: u.Username, Role: u.Role}
		}
	}
	resp, err := h.runtime.HandleWASM(c.Request.Context(), name, req)
	if err != nil {
		slog.Warn("server plugin wasm call failed", "plugin", name, "path", path, "err", err.Error())
		fail(c, http.StatusBadGateway, "插件执行失败")
		return
	}
	contentType := "application/json; charset=utf-8"
	for k, v := range resp.Headers {
		if strings.EqualFold(k, "Content-Type") && v != "" && !strings.ContainsAny(v, "\r\n") {
			contentType = v
		}
	}
	payload := resp.Body
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	c.Data(resp.Status, contentType, payload)
}

// byID 解析并查找插件。
func (h *ServerPluginHandler) byID(c *gin.Context) (*model.ServerPlugin, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return nil, false
	}
	row, err := h.store.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return nil, false
	}
	if row == nil {
		fail(c, http.StatusNotFound, "插件不存在")
		return nil, false
	}
	return row, true
}

// viewOf 组装列表行：只带声明段数量，不下发 manifest 原文。
func viewOf(row *model.ServerPlugin) model.ServerPluginView {
	v := row.View()
	if m, err := serverplugin.ParseManifest([]byte(row.ManifestJSON)); err == nil {
		v.Pages, v.Routes = len(m.Pages), len(m.Routes)
		v.Events, v.Jobs = len(m.Events), len(m.Jobs)
		v.Runtime = m.Runtime
	}
	return v
}

func (h *ServerPluginHandler) reload() {
	if h.runtime == nil {
		return
	}
	if err := h.runtime.Reload(); err != nil {
		slog.Warn("server plugin runtime reload failed", "err", err.Error())
	}
}

func (h *ServerPluginHandler) audit(c *gin.Context, action string, targetID uint, targetName, detail string) {
	if h.audits == nil {
		return
	}
	me := middleware.UserFrom(c)
	if me == nil {
		return
	}
	if err := h.audits.Record(store.AuditEntry{
		AdminID: me.ID, Action: action, TargetType: "server_plugin",
		TargetID: targetID, TargetName: targetName, Detail: detail, IP: c.ClientIP(),
	}); err != nil {
		slog.Warn("server plugin audit write failed", "action", action, "err", err.Error())
	}
}
