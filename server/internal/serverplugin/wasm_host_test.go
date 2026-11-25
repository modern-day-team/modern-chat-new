package serverplugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func testWP(perms ...string) *wasmPlugin {
	m := make(map[string]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return &wasmPlugin{permissions: m}
}

func userCtx(id uint) context.Context {
	return context.WithValue(context.Background(), wasmUserKey{}, &WASMUser{ID: id, Username: "tester"})
}

// TestHostOpsRegistered 静态自检：新增 op 必须同时登记到 hostOps。
func TestHostOpsRegistered(t *testing.T) {
	want := []string{
		"kv.get", "kv.set", "kv.delete", "kv.keys",
		"http.request", "ai.chat",
		"auth.qr.start", "auth.qr.confirm", "auth.qr.invalidate",
		"message.send", "conv.list", "conv.members", "user.get", "user.search", "notification.push",
	}
	seen := make(map[string]bool, len(hostOps))
	for _, op := range hostOps {
		if seen[op] {
			t.Fatalf("hostOps 存在重复 op %q", op)
		}
		seen[op] = true
	}
	for _, op := range want {
		if !seen[op] {
			t.Errorf("hostOps 缺少 op %q", op)
		}
	}
	if len(hostOps) != len(want) {
		t.Fatalf("hostOps 数量 %d，期望 %d", len(hostOps), len(want))
	}
}

// TestHostCallNewOpsPermissionGate 每个新 op 都受对应 manifest 权限约束。
func TestHostCallNewOpsPermissionGate(t *testing.T) {
	cases := []struct{ op, perm string }{
		{"message.send", "server:message"},
		{"notification.push", "server:message"},
		{"conv.list", "server:conv"},
		{"conv.members", "server:conv"},
		{"user.get", "server:user"},
		{"user.search", "server:user"},
	}
	r := NewRuntime("secret", nil)
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			_, err := r.dispatchHostCall(userCtx(7), testWP(), tc.op, []byte(`{}`))
			if err == nil || !strings.Contains(err.Error(), "缺少 "+tc.perm) {
				t.Fatalf("期望缺少 %s 权限，实际 %v", tc.perm, err)
			}
			// 授权后即便参数不完整，也不能落到「未知宿主调用」（证明 op 已注册）。
			_, err = r.dispatchHostCall(userCtx(7), testWP(tc.perm), tc.op, []byte(`{}`))
			if err == nil || strings.Contains(err.Error(), "未知宿主调用") {
				t.Fatalf("op 应已注册，实际 %v", err)
			}
		})
	}
}

// TestHostCallRequiresLogin 公开路由无用户上下文，全部新 op 必须拒绝。
func TestHostCallRequiresLogin(t *testing.T) {
	r := NewRuntime("secret", nil)
	wp := testWP("server:message", "server:conv", "server:user")
	for _, op := range []string{
		"message.send", "conv.list", "conv.members",
		"user.get", "user.search", "notification.push",
	} {
		_, err := r.dispatchHostCall(context.Background(), wp, op, []byte(`{}`))
		if err == nil || !strings.Contains(err.Error(), "需要登录用户") {
			t.Fatalf("%s 在公开路由应被拒绝，实际 %v", op, err)
		}
	}
}

// TestHostCallNotConfigured 未注入 HostServices 时返回明确错误而非 panic。
func TestHostCallNotConfigured(t *testing.T) {
	r := NewRuntime("secret", nil)
	wp := testWP("server:message", "server:conv", "server:user")
	ctx := userCtx(7)
	cases := []struct{ op, raw, want string }{
		{"message.send", `{"convId":1,"content":"hi"}`, "发送消息能力未配置"},
		{"conv.list", `{}`, "会话列表能力未配置"},
		{"conv.members", `{"convId":1}`, "会话成员能力未配置"},
		{"user.get", `{"userId":1}`, "用户查询能力未配置"},
		{"user.search", `{"kw":"a"}`, "用户搜索能力未配置"},
		{"notification.push", `{"body":"hi"}`, "推送能力未配置"},
	}
	for _, tc := range cases {
		_, err := r.dispatchHostCall(ctx, wp, tc.op, []byte(tc.raw))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s 期望 %q，实际 %v", tc.op, tc.want, err)
		}
	}
}

// TestHostCallInputValidation 入参边界：payload 必须是对象且 ≤32KB，kw/limit 受限。
func TestHostCallInputValidation(t *testing.T) {
	r := NewRuntime("secret", nil)
	wp := testWP("server:message", "server:user")
	ctx := userCtx(7)
	cases := []struct{ op, raw, want string }{
		{"message.send", `{"convId":1,"content":"hi","payload":[1,2]}`, "payload 必须是 JSON object"},
		{"message.send", `{"convId":1,"content":"hi","payload":null}`, "payload 必须是 JSON object"},
		{"message.send", `{"convId":1,"content":"hi","payload":{"k":"` + strings.Repeat("a", 32*1024) + `"}}`, "超过 32KB"},
		{"message.send", `{"convId":0,"content":"hi"}`, "convId 不合法"},
		{"message.send", `{"convId":1,"content":""}`, "消息内容需为"},
		{"message.send", `{"convId":1,"content":"hi","msgType":"BAD"}`, "msgType 不合法"},
		{"user.search", `{"kw":"` + strings.Repeat("字", 51) + `"}`, "kw 需为"},
		{"user.search", `{"kw":"a","limit":51}`, "limit 需在 1-50"},
		{"no.such.op", `{}`, "未知宿主调用"},
	}
	for _, tc := range cases {
		_, err := r.dispatchHostCall(ctx, wp, tc.op, []byte(tc.raw))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s 期望含 %q，实际 %v", tc.op, tc.want, err)
		}
	}
}

// TestHostCallWithInjectedServices 注入假服务验证六个新 op 的返回形状与用户边界。
func TestHostCallWithInjectedServices(t *testing.T) {
	var gotFrom, gotConv, gotPusherUID uint
	var gotMsgType, gotPayload, gotPushBody string
	r := NewRuntime("secret", nil)
	r.SetHost(HostServices{
		MessageSender: func(from, convID uint, content, msgType string, payload json.RawMessage) error {
			gotFrom, gotConv, gotMsgType, gotPayload = from, convID, msgType, string(payload)
			return nil
		},
		ConvLister: func(userID uint) ([]ConvBrief, error) {
			return []ConvBrief{{ID: 3, Type: 2, MemberCount: 5, Unread: 2}}, nil
		},
		ConvMembers: func(userID, convID uint) ([]MemberBrief, error) {
			return []MemberBrief{{UserID: 7, Nickname: "七", Role: "owner"}}, nil
		},
		UserLookup: func(userID uint) (*UserBrief, error) {
			return &UserBrief{ID: userID, Username: "alice", Nickname: "爱丽丝"}, nil
		},
		UserSearch: func(kw string, limit int) ([]UserBrief, error) {
			return []UserBrief{{ID: 5, Username: "alice"}}, nil
		},
		NotificationPusher: func(userID uint, title, body string) error {
			gotPusherUID, gotPushBody = userID, body
			return nil
		},
	})
	wp := testWP("server:message", "server:conv", "server:user")
	ctx := userCtx(9)

	out, err := r.dispatchHostCall(ctx, wp, "conv.list", []byte(`{}`))
	if err != nil || !strings.Contains(string(out), `"conversations"`) || !strings.Contains(string(out), `"unread":2`) {
		t.Fatalf("conv.list 返回异常: %s, %v", out, err)
	}
	out, err = r.dispatchHostCall(ctx, wp, "conv.members", []byte(`{"convId":3}`))
	if err != nil || !strings.Contains(string(out), `"nickname":"七"`) {
		t.Fatalf("conv.members 返回异常: %s, %v", out, err)
	}
	out, err = r.dispatchHostCall(ctx, wp, "user.get", []byte(`{"userId":5}`))
	if err != nil || !strings.Contains(string(out), `"nickname":"爱丽丝"`) {
		t.Fatalf("user.get 返回异常: %s, %v", out, err)
	}
	out, err = r.dispatchHostCall(ctx, wp, "user.search", []byte(`{"kw":"ali","limit":3}`))
	if err != nil || !strings.Contains(string(out), `"users"`) {
		t.Fatalf("user.search 返回异常: %s, %v", out, err)
	}
	out, err = r.dispatchHostCall(ctx, wp, "message.send", []byte(`{"convId":12,"content":"hi","payload":{"x":1}}`))
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("message.send 返回异常: %s, %v", out, err)
	}
	if gotFrom != 9 || gotConv != 12 || gotMsgType != "text" || gotPayload != `{"x":1}` {
		t.Fatalf("message.send 入参错误: from=%d conv=%d type=%s payload=%s", gotFrom, gotConv, gotMsgType, gotPayload)
	}
	out, err = r.dispatchHostCall(ctx, wp, "notification.push", []byte(`{"body":"hello"}`))
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("notification.push 返回异常: %s, %v", out, err)
	}
	if gotPusherUID != 9 || gotPushBody != "hello" {
		t.Fatalf("notification.push 越权推送: uid=%d body=%s", gotPusherUID, gotPushBody)
	}
}
