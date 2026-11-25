package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
)

// AIHandler 服务端统一 AI 代理：密钥只存在服务端，用户插件通过登录接口调用。
// 当前为 OpenAI 兼容的非流式 /chat/completions，后续可在不暴露密钥的前提下加 SSE。
type AIHandler struct{ cfg config.Config }

func NewAIHandler(cfg config.Config) *AIHandler { return &AIHandler{cfg: cfg} }

type aiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Status GET /api/ai/status —— 前端据此决定是否展示 AI 页面。
func (h *AIHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.cfg.AIEnabled(), "model": h.cfg.AIModel})
}

// Chat POST /api/ai/chat body {messages:[{role,content}]} -> {content}
func (h *AIHandler) Chat(c *gin.Context) {
	if !h.cfg.AIEnabled() {
		fail(c, http.StatusServiceUnavailable, "服务端 AI 未配置")
		return
	}
	var req struct {
		Messages []aiMessage `json:"messages"`
	}
	if !bind(c, &req) {
		return
	}
	if len(req.Messages) == 0 || len(req.Messages) > 40 {
		fail(c, http.StatusBadRequest, "messages 数量需为 1-40")
		return
	}
	total := 0
	for _, m := range req.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			fail(c, http.StatusBadRequest, "非法消息角色")
			return
		}
		if len(m.Content) > 32*1024 {
			fail(c, http.StatusBadRequest, "单条消息超过 32KB")
			return
		}
		total += len(m.Content)
	}
	if total > 96*1024 {
		fail(c, http.StatusBadRequest, "上下文超过 96KB")
		return
	}
	body, _ := json.Marshal(map[string]any{
		"model":       h.cfg.AIModel,
		"messages":    req.Messages,
		"temperature": 0.7,
		"stream":      false,
	})
	timeout := time.Duration(h.cfg.AITimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	status, raw, err := plugin.SafeRequestWithAuthTimeout(http.MethodPost, h.cfg.AIBaseURL, body,
		map[string]string{"Authorization": "Bearer " + h.cfg.AIAPIKey, "Content-Type": "application/json"},
		2<<20, timeout, h.cfg.AIAllowPrivate)
	if err != nil {
		fail(c, http.StatusBadGateway, "AI 上游不可用")
		return
	}
	if status < 200 || status >= 300 {
		fail(c, http.StatusBadGateway, fmt.Sprintf("AI 上游返回 %d", status))
		return
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		fail(c, http.StatusBadGateway, "AI 响应解析失败")
		return
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		fail(c, http.StatusBadGateway, "AI 没有返回内容")
		return
	}
	c.JSON(http.StatusOK, gin.H{"content": out.Choices[0].Message.Content})
}
