//go:build wasip1

// qr-kit 是一个最小服务端 WASM 插件示例：
//   - 公开路由 /qr/start：调用宿主 auth.qr.start 生成一次性二维码票据
//   - 登录路由 /qr/confirm：把当前登录用户绑定到票据（用于第三方扫码回调后的确认）
//
// 构建（在 server-examples/qr-kit 下）：
//
//	GOOS=wasip1 GOARCH=wasm go build -o qr-kit.wasm .
//
// 安装：后台粘贴 manifest.json，选择 qr-kit.wasm 上传后启用。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unsafe"
)

//go:wasmimport env mc_call
func mcCall(opPtr, opLen, reqPtr, reqLen, outPtr, outCap uint32) int32

type request struct {
	Kind   string          `json:"kind"`
	Plugin string          `json:"plugin"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
	User   *struct {
		ID uint `json:"id"`
	} `json:"user"`
}

type response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

func hostCall(op string, req any) ([]byte, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 64*1024)
	var opPtr uint32
	if len(op) > 0 {
		opPtr = uint32(uintptr(unsafe.Pointer(unsafe.StringData(op))))
	}
	var reqPtr uint32
	if len(raw) > 0 {
		reqPtr = uint32(uintptr(unsafe.Pointer(&raw[0])))
	}
	n := mcCall(opPtr, uint32(len(op)), reqPtr, uint32(len(raw)),
		uint32(uintptr(unsafe.Pointer(&out[0]))), uint32(len(out)))
	if n < 0 {
		return nil, fmt.Errorf("host %s failed: %d", op, n)
	}
	return out[:n], nil
}

func writeJSON(v any) {
	raw, _ := json.Marshal(v)
	_, _ = os.Stdout.Write(raw)
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeJSON(response{Status: 400, Body: json.RawMessage(`{"error":"bad request"}`)})
		return
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(response{Status: 400, Body: json.RawMessage(`{"error":"bad json"}`)})
		return
	}
	switch req.Path {
	case "/qr/start":
		host, err := hostCall("auth.qr.start", map[string]any{})
		if err != nil {
			writeJSON(response{Status: 500, Body: json.RawMessage(`{"error":"qr start failed"}`)})
			return
		}
		var data struct {
			Token     string `json:"token"`
			ExpiresAt string `json:"expiresAt"`
		}
		_ = json.Unmarshal(host, &data)
		body, _ := json.Marshal(map[string]any{
			"token": data.Token, "expiresAt": data.ExpiresAt,
			"confirmPath": "/qr/" + data.Token,
		})
		writeJSON(response{Status: 200, Body: body})
	case "/qr/confirm":
		if req.User == nil || req.User.ID == 0 {
			writeJSON(response{Status: 401, Body: json.RawMessage(`{"error":"login required"}`)})
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.Unmarshal(req.Body, &in)
		host, err := hostCall("auth.qr.confirm", map[string]any{"token": in.Token})
		if err != nil {
			writeJSON(response{Status: 500, Body: json.RawMessage(`{"error":"confirm failed"}`)})
			return
		}
		writeJSON(response{Status: 200, Body: host})
	default:
		writeJSON(response{Status: 404, Body: json.RawMessage(`{"error":"not found"}`)})
	}
}
