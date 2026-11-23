package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modern-day-team/modern-chat-new/server/internal/config"
)

// HTTP 邮件 API 通用通道。
//
// 只绑定「通用 JSON 投递」这一个最小公约数：POST {apiUrl}，
// body = {from,to,subject,text}，Authorization: Bearer {apiKey}。
// SendGrid / Resend / Mailgun 等都能通过填对应的 endpoint 与 API Key 接入，
// 不引入任何厂商 SDK，也不绑定私有字段（如 SendGrid 的 personalizations）。
// 需要厂商专属能力（模板 ID / 批量）时再在此处扩展，不污染上层调用点。

type apiMailRequest struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// sendAPIMail 返回 (rawBody, error)；rawBody 供失败排查（不含 API Key）。
func sendAPIMail(cfg config.SMTPConfig, to, subject, text string) error {
	payload, err := json.Marshal(apiMailRequest{
		From: cfg.From, To: to, Subject: subject, Text: text,
	})
	if err != nil {
		return fmt.Errorf("encode mail api request: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, cfg.APIURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build mail api request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: mailTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("mail api request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 不回显 API Key；只截取响应片段帮助判断配置错误（如 401/404/422）
		msg := strings.TrimSpace(string(body))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("mail api status %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// apiConfigured 校验 API 通道配置是否完整（与 config.SMTPConfig.Enabled 保持一致）。
func apiConfigured(cfg config.SMTPConfig) bool {
	return cfg.Provider == "api" && cfg.APIURL != "" && cfg.APIKey != ""
}
