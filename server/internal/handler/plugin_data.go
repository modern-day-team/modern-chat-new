package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

const (
	pluginDataMaxKeyLen   = 120       // 字符数
	pluginDataMaxValueLen = 64 * 1024 // 字节
)

// PluginDataHandler 用户插件的服务端私有 KV。
// 所有接口都要求「当前登录用户安装了该插件」；全局下架的插件只读不可写。
type PluginDataHandler struct {
	plugins *store.PluginStore
	data    *store.PluginDataStore
}

func NewPluginDataHandler(plugins *store.PluginStore, data *store.PluginDataStore) *PluginDataHandler {
	return &PluginDataHandler{plugins: plugins, data: data}
}

// ownedPlugin 校验 :id 是当前用户安装的插件，返回插件 id。
func (h *PluginDataHandler) ownedPlugin(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return 0, false
	}
	me := middleware.UserFrom(c)
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return 0, false
	}
	if p == nil || me == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return 0, false
	}
	return uint(id), true
}

// writablePlugin ownedPlugin + 全局下架检查（写操作）。
func (h *PluginDataHandler) writablePlugin(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		fail(c, http.StatusBadRequest, "插件 id 不合法")
		return 0, false
	}
	me := middleware.UserFrom(c)
	p, err := h.plugins.ByID(uint(id))
	if err != nil {
		internal(c, err)
		return 0, false
	}
	if p == nil || me == nil || p.InstalledBy != me.ID {
		fail(c, http.StatusNotFound, "插件不存在")
		return 0, false
	}
	if p.GlobalDisabled {
		fail(c, http.StatusForbidden, "插件已被站长下架")
		return 0, false
	}
	return uint(id), true
}

func validPluginDataKey(key string) bool {
	n := len([]rune(key))
	return n >= 1 && n <= pluginDataMaxKeyLen
}

// Get GET /api/plugins/:id/data?key=...
func (h *PluginDataHandler) Get(c *gin.Context) {
	id, ok := h.ownedPlugin(c)
	if !ok {
		return
	}
	key := c.Query("key")
	if !validPluginDataKey(key) {
		fail(c, http.StatusBadRequest, "key 需为 1-120 个字符")
		return
	}
	me := middleware.UserFrom(c)
	value, _, err := h.data.Get(me.ID, id, key)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"value": value})
}

// Set PUT /api/plugins/:id/data body {key,value}
func (h *PluginDataHandler) Set(c *gin.Context) {
	id, ok := h.writablePlugin(c)
	if !ok {
		return
	}
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if !bind(c, &req) {
		return
	}
	if !validPluginDataKey(req.Key) {
		fail(c, http.StatusBadRequest, "key 需为 1-120 个字符")
		return
	}
	if len(req.Value) > pluginDataMaxValueLen {
		fail(c, http.StatusRequestEntityTooLarge, "value 不能超过 64KB")
		return
	}
	me := middleware.UserFrom(c)
	if err := h.data.Set(me.ID, id, req.Key, req.Value); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Delete DELETE /api/plugins/:id/data?key=...
func (h *PluginDataHandler) Delete(c *gin.Context) {
	id, ok := h.writablePlugin(c)
	if !ok {
		return
	}
	key := c.Query("key")
	if !validPluginDataKey(key) {
		fail(c, http.StatusBadRequest, "key 需为 1-120 个字符")
		return
	}
	me := middleware.UserFrom(c)
	if err := h.data.Delete(me.ID, id, key); err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Keys GET /api/plugins/:id/data/keys
func (h *PluginDataHandler) Keys(c *gin.Context) {
	id, ok := h.ownedPlugin(c)
	if !ok {
		return
	}
	me := middleware.UserFrom(c)
	keys, err := h.data.Keys(me.ID, id)
	if err != nil {
		internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"keys": keys})
}
