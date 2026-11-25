package serverplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/modern-day-team/modern-chat-new/server/internal/plugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/service/qrsession"
)

const (
	wasmTimeout     = 8 * time.Second
	wasmMaxInput    = 1 << 20
	wasmMaxOutput   = 2 << 20
	wasmMaxHostResp = 2 << 20
)

// WASMUser 请求上下文里的当前登录用户；公开路由为 nil。
type WASMUser struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

type wasmUserKey struct{}

func currentWASMUser(ctx context.Context) *WASMUser {
	u, _ := ctx.Value(wasmUserKey{}).(*WASMUser)
	return u
}

// WASMRequest 是传给 WASM 插件的统一请求信封。
type WASMRequest struct {
	Kind    string            `json:"kind"` // http / event / job
	Plugin  string            `json:"plugin"`
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path,omitempty"`
	Query   string            `json:"query,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
	User    *WASMUser         `json:"user,omitempty"`
	Event   string            `json:"event,omitempty"`
	Payload json.RawMessage   `json:"payload,omitempty"`
}

// WASMResponse 是 WASM 插件返回的 HTTP 响应。
type WASMResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// ValidateWASM 在上传时编译一次，拒绝损坏或非 WASM 的文件。
func ValidateWASM(ctx context.Context, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return err
	}
	if _, err := rt.CompileModule(ctx, data); err != nil {
		return fmt.Errorf("WASM 编译失败: %w", err)
	}
	return nil
}

// HandleWASM 调用插件 WASM 的 `_start`，stdin 传请求信封，stdout 收响应 JSON。
func (r *Runtime) HandleWASM(ctx context.Context, pluginName string, req WASMRequest) (WASMResponse, error) {
	snap, _ := r.snap.Load().(*snapshot)
	if snap == nil || snap.wasm[pluginName] == nil {
		return WASMResponse{}, fmt.Errorf("wasm plugin %s not enabled", pluginName)
	}
	wp := snap.wasm[pluginName]
	req.Plugin = pluginName
	if req.Kind == "" {
		req.Kind = "http"
	}
	// 宿主调用可读取当前用户；公开路由 req.User 为 nil，auth.qr.confirm 会拒绝。
	ctx = context.WithValue(ctx, wasmUserKey{}, req.User)
	raw, err := json.Marshal(req)
	if err != nil {
		return WASMResponse{}, err
	}
	out, err := r.invokeWASM(ctx, wp, raw)
	if err != nil {
		return WASMResponse{}, err
	}
	var res WASMResponse
	if err := json.Unmarshal(out, &res); err != nil {
		return WASMResponse{}, fmt.Errorf("wasm 返回不是合法 JSON: %w", err)
	}
	if res.Status == 0 {
		res.Status = http.StatusOK
	}
	if res.Status < 100 || res.Status > 599 {
		return WASMResponse{}, fmt.Errorf("wasm 返回状态码非法: %d", res.Status)
	}
	return res, nil
}

func (r *Runtime) invokeWASM(ctx context.Context, wp *wasmPlugin, input []byte) ([]byte, error) {
	if len(input) > wasmMaxInput {
		return nil, fmt.Errorf("wasm 输入超过 1MB")
	}
	ctx, cancel := context.WithTimeout(ctx, wasmTimeout)
	defer cancel()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCloseOnContextDone(true))
	defer rt.Close(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, err
	}

	b := rt.NewHostModuleBuilder("env")
	b.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(_ context.Context, mod api.Module, stack []uint64) {
		stack[0] = r.wasmHostCall(ctx, wp, mod, stack)
	}), []api.ValueType{
		api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32,
		api.ValueTypeI32, api.ValueTypeI32, api.ValueTypeI32,
	}, []api.ValueType{api.ValueTypeI32}).Export("mc_call")
	if _, err := b.Instantiate(ctx); err != nil {
		return nil, err
	}
	compiled, err := rt.CompileModule(ctx, wp.wasm)
	if err != nil {
		return nil, fmt.Errorf("编译 wasm 失败: %w", err)
	}
	var stdout limitedBuffer
	stdout.limit = wasmMaxOutput
	var stderr limitedBuffer
	stderr.limit = 64 << 10
	_, err = rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().
		WithName("").
		WithStdin(bytes.NewReader(input)).
		WithStdout(&stdout).
		WithStderr(&stderr))
	if err != nil {
		return nil, fmt.Errorf("wasm 异常退出: %w (%s)", err, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil, fmt.Errorf("wasm 没有返回数据")
	}
	return stdout.Bytes(), nil
}

func (r *Runtime) wasmHostCall(ctx context.Context, wp *wasmPlugin, mod api.Module, stack []uint64) uint64 {
	mem := mod.Memory()
	opPtr, opLen := uint32(stack[0]), uint32(stack[1])
	reqPtr, reqLen := uint32(stack[2]), uint32(stack[3])
	outPtr, outCap := uint32(stack[4]), uint32(stack[5])
	opBytes, ok := mem.Read(opPtr, opLen)
	if !ok {
		return ^uint64(0)
	}
	reqBytes, ok := mem.Read(reqPtr, reqLen)
	if !ok {
		return ^uint64(0)
	}
	out, err := r.dispatchHostCall(ctx, wp, string(opBytes), reqBytes)
	if err != nil {
		slog.Warn("server plugin host call failed", "plugin", wp.name, "op", string(opBytes), "err", err.Error())
		out, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	if uint32(len(out)) > outCap || !mem.Write(outPtr, out) {
		return 1 << 63
	}
	return uint64(len(out))
}

func validKVKey(key string) bool {
	n := len([]rune(key))
	return n >= 1 && n <= 120
}

func (r *Runtime) dispatchHostCall(ctx context.Context, wp *wasmPlugin, op string, raw []byte) ([]byte, error) {
	switch op {
	case "kv.get":
		if wp.kv == nil {
			return nil, fmt.Errorf("kv 未启用")
		}
		var in struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if !validKVKey(in.Key) {
			return nil, fmt.Errorf("kv key 需为 1-120 个字符")
		}
		v, err := wp.kv.Get(wp.id, in.Key)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"value": v})
	case "kv.set":
		if wp.kv == nil {
			return nil, fmt.Errorf("kv 未启用")
		}
		var in struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if !validKVKey(in.Key) || len(in.Value) > 64*1024 {
			return nil, fmt.Errorf("kv key 需为 1-120 个字符且 value ≤64KB")
		}
		if err := wp.kv.Set(wp.id, in.Key, in.Value); err != nil {
			return nil, err
		}
		return []byte(`{"ok":true}`), nil
	case "kv.delete":
		if wp.kv == nil {
			return nil, fmt.Errorf("kv 未启用")
		}
		var in struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		if !validKVKey(in.Key) {
			return nil, fmt.Errorf("kv key 需为 1-120 个字符")
		}
		return []byte(`{"ok":true}`), wp.kv.Delete(wp.id, in.Key)
	case "kv.keys":
		if wp.kv == nil {
			return nil, fmt.Errorf("kv 未启用")
		}
		keys, err := wp.kv.Keys(wp.id)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"keys": keys})
	case "http.request":
		return r.hostHTTP(ctx, wp, raw)
	case "ai.chat":
		if !wp.permissions["ai:chat"] {
			return nil, fmt.Errorf("缺少 ai:chat 权限")
		}
		return r.hostAIChat(ctx, wp, raw)
	case "auth.qr.start":
		if !wp.permissions["auth:qr"] {
			return nil, fmt.Errorf("缺少 auth:qr 权限")
		}
		token, expires, err := qrsession.Default().Issue()
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"token": token, "expiresAt": expires.UTC().Format(time.RFC3339)})
	case "auth.qr.confirm":
		if !wp.permissions["auth:qr"] {
			return nil, fmt.Errorf("缺少 auth:qr 权限")
		}
		u := currentWASMUser(ctx)
		if u == nil || u.ID == 0 {
			return nil, fmt.Errorf("确认扫码票据需要登录")
		}
		var in struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		ok := qrsession.Default().Confirm(in.Token, u.ID)
		return json.Marshal(map[string]bool{"ok": ok})
	case "auth.qr.invalidate":
		if !wp.permissions["auth:qr"] {
			return nil, fmt.Errorf("缺少 auth:qr 权限")
		}
		var in struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		qrsession.Default().Invalidate(in.Token)
		return []byte(`{"ok":true}`), nil
	default:
		return nil, fmt.Errorf("未知宿主调用 %q", op)
	}
}

func (r *Runtime) hostHTTP(ctx context.Context, wp *wasmPlugin, raw []byte) ([]byte, error) {
	var in struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    json.RawMessage   `json:"body"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if err := checkEndpoint(in.URL, wp.manifest.AllowedHosts); err != nil {
		return nil, err
	}
	status, body, err := plugin.SafeRequest(in.Method, in.URL, in.Body, in.Headers, wasmMaxHostResp)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"status": status, "body": json.RawMessage(body)})
}

func (r *Runtime) hostAIChat(ctx context.Context, wp *wasmPlugin, raw []byte) ([]byte, error) {
	if !r.ai.Enabled() {
		return nil, fmt.Errorf("AI 代理未配置")
	}
	var in struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if len(in.Messages) == 0 || len(in.Messages) > 40 {
		return nil, fmt.Errorf("messages 数量需为 1-40")
	}
	for _, m := range in.Messages {
		if m.Role != "system" && m.Role != "user" && m.Role != "assistant" {
			return nil, fmt.Errorf("非法消息角色 %q", m.Role)
		}
		if len(m.Content) > 32*1024 {
			return nil, fmt.Errorf("单条消息超过 32KB")
		}
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":       r.ai.Model,
		"messages":    in.Messages,
		"temperature": 0.7,
		"stream":      false,
	})
	timeout := r.ai.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	status, body, err := plugin.SafeRequestWithAuthTimeout(http.MethodPost, r.ai.BaseURL, reqBody,
		map[string]string{"Authorization": "Bearer " + r.ai.APIKey, "Content-Type": "application/json"},
		wasmMaxHostResp, timeout, r.ai.AllowPrivate)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("AI 上游返回 %d", status)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("AI 响应解析失败: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("AI 没有返回内容")
	}
	return json.Marshal(map[string]string{"content": out.Choices[0].Message.Content})
}

// limitedBuffer 限制插件 stdout 体积，避免恶意 WASM 用日志撑爆内存。
type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.buf.Len()+len(p) > b.limit {
		return 0, io.ErrShortWrite
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) Len() int       { return b.buf.Len() }
func (b *limitedBuffer) String() string { return b.buf.String() }
