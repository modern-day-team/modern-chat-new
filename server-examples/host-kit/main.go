//go:build wasip1

// host-kit 是服务端 WASM 宿主能力综合示例：
//   - GET  /summary  → conv.list
//   - GET  /members  → conv.members（query: convId）
//   - POST /users    → user.search（body: {kw,limit}）
//   - POST /send     → message.send（body: {convId,content,msgType?,payload?}）
//   - POST /push     → notification.push（body: {title,body}）
//   - POST /ai       → ai.chat（body: {messages}）
//
// 构建（在 server-examples/host-kit 下）：
//
//	GOOS=wasip1 GOARCH=wasm go build -o host-kit.wasm .
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"unsafe"
)

//go:wasmimport env mc_call
func mcCall(opPtr, opLen, reqPtr, reqLen, outPtr, outCap uint32) int32

type request struct {
	Kind   string          `json:"kind"`
	Plugin string          `json:"plugin"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Query  string          `json:"query"`
	Body   json.RawMessage `json:"body"`
	User   *struct {
		ID       uint   `json:"id"`
		Username string `json:"username"`
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
	out := make([]byte, 128*1024)
	var opPtr, reqPtr uint32
	if len(op) > 0 {
		opPtr = uint32(uintptr(unsafe.Pointer(unsafe.StringData(op))))
	}
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

func writeJSON(status int, v any) {
	raw, _ := json.Marshal(v)
	full, _ := json.Marshal(response{Status: status, Body: raw})
	_, _ = os.Stdout.Write(full)
}

func writeErr(status int, msg string) {
	raw, _ := json.Marshal(map[string]any{"error": msg})
	writeJSON(status, json.RawMessage(raw))
}

func parseBody(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("请求体为空")
	}
	return json.Unmarshal(raw, out)
}

func main() {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		writeErr(400, "bad request")
		return
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		writeErr(400, "bad json")
		return
	}
	if req.User == nil || req.User.ID == 0 {
		writeErr(401, "login required")
		return
	}

	switch req.Path {
	case "/summary":
		host, err := hostCall("conv.list", map[string]any{})
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		var data struct {
			Conversations []map[string]any `json:"conversations"`
		}
		_ = json.Unmarshal(host, &data)
		top := data.Conversations
		if len(top) > 10 {
			top = top[:10]
		}
		raw, _ := json.Marshal(map[string]any{
			"count": len(data.Conversations), "preview": top,
		})
		writeJSON(200, json.RawMessage(raw))
	case "/members":
		q, _ := url.ParseQuery(req.Query)
		convID, _ := strconv.ParseUint(q.Get("convId"), 10, 64)
		if convID == 0 {
			writeErr(400, "convId required")
			return
		}
		host, err := hostCall("conv.members", map[string]any{"convId": convID})
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		writeJSON(200, json.RawMessage(host))
	case "/users":
		var in struct {
			Kw    string `json:"kw"`
			Limit int    `json:"limit"`
		}
		if err := parseBody(req.Body, &in); err != nil {
			writeErr(400, "bad body")
			return
		}
		host, err := hostCall("user.search", map[string]any{"kw": in.Kw, "limit": in.Limit})
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		writeJSON(200, json.RawMessage(host))
	case "/send":
		var in struct {
			ConvID  uint            `json:"convId"`
			Content string          `json:"content"`
			MsgType string          `json:"msgType"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := parseBody(req.Body, &in); err != nil {
			writeErr(400, "bad body")
			return
		}
		host, err := hostCall("message.send", in)
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		writeJSON(200, json.RawMessage(host))
	case "/push":
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if err := parseBody(req.Body, &in); err != nil {
			writeErr(400, "bad body")
			return
		}
		host, err := hostCall("notification.push", in)
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		writeJSON(200, json.RawMessage(host))
	case "/ai":
		var in struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := parseBody(req.Body, &in); err != nil {
			writeErr(400, "bad body")
			return
		}
		host, err := hostCall("ai.chat", in)
		if err != nil {
			writeErr(502, err.Error())
			return
		}
		writeJSON(200, json.RawMessage(host))
	default:
		writeErr(404, "not found")
	}
}
