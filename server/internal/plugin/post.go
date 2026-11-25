package plugin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 出站请求超时与响应体默认上限。
const outboundTimeout = 10 * time.Second

var outboundMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true,
}

// SafePostJSON 以 JSON POST 调用外部端点，复用与 Fetch 相同的 SSRF 防护：
// 拨号时解析并校验公网地址（防 DNS rebinding）、逐跳复检重定向、响应体限长。
// 供服务端插件的事件 / 定时任务使用。
func SafePostJSON(rawURL string, body []byte, maxBytes int64) (int, []byte, error) {
	return SafePostJSONWithHeaders(rawURL, body, nil, maxBytes)
}

// SafePostJSONWithHeaders 同 SafePostJSON，额外允许传入少量安全请求头
// （服务端插件用来带 X-MC-Plugin / X-MC-Signature）。
// Authorization / Cookie / Host 等身份类头一律忽略：绝不把用户 JWT 转发给第三方。
func SafePostJSONWithHeaders(rawURL string, body []byte, headers map[string]string, maxBytes int64) (int, []byte, error) {
	return SafeRequest(http.MethodPost, rawURL, body, headers, maxBytes)
}

// SafeRequest 是 SafePostJSON 的通用版本，动态路由代理用它透传方法 / 请求体。
func SafeRequest(method, rawURL string, body []byte, headers map[string]string, maxBytes int64) (int, []byte, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return 0, nil, fmt.Errorf("%w: 仅支持 http/https 链接且不能携带用户名密码", ErrInvalid)
	}
	m := strings.ToUpper(method)
	if !outboundMethods[m] {
		return 0, nil, fmt.Errorf("%w: 不支持的请求方法 %q", ErrInvalid, method)
	}
	if maxBytes <= 0 {
		maxBytes = 2 << 20
	}

	dialer := &net.Dialer{Timeout: outboundTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		// 不继承环境代理：否则请求可能交给内网代理，绕过地址校验。
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, e := net.SplitHostPort(addr)
			if e != nil {
				return nil, e
			}
			ip, e := resolvePublicIP(ctx, host)
			if e != nil {
				return nil, e
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   outboundTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("重定向到非 http(s)")
			}
			// 目标地址的私网校验由 DialContext 在真正连接时完成
			return nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), outboundTimeout)
	defer cancel()
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, m, rawURL, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: 链接不合法", ErrInvalid)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		if !safeOutboundHeader(k) {
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: 请求失败", ErrInvalid)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("%w: 读取响应失败", ErrInvalid)
	}
	if int64(len(data)) > maxBytes {
		return resp.StatusCode, nil, fmt.Errorf("%w: 响应超过大小上限", ErrInvalid)
	}
	return resp.StatusCode, data, nil
}

// safeOutboundHeader 过滤禁止透传的请求头：身份凭据与连接级头一律丢弃，
// 防止用户 JWT / Cookie 被转发到第三方端点。
func safeOutboundHeader(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "host", "authorization", "cookie", "proxy-authorization",
		"connection", "content-length", "transfer-encoding":
		return false
	}
	return k != "" && !strings.ContainsAny(k, "\r\n")
}
