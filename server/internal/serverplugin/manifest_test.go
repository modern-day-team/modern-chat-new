package serverplugin

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const validManifestJSON = `{
  "name": "server-hello",
  "version": "1.0.0",
  "description": "演示插件",
  "pages": [
    {"id":"home","title":"首页","icon":"home","scope":"user","body":[{"type":"text","value":"hi"}]},
    {"id":"admin-panel","title":"管理","scope":"admin","body":[]}
  ],
  "routes": [
    {"method":"GET","path":"/hello","response":{"status":200,"body":{"message":"hi"}}},
    {"method":"POST","path":"/hook","proxy":"https://example.com/hook"}
  ],
  "events": [{"event":"message:new","endpoint":"https://example.com/events"}],
  "jobs": [{"name":"ping","endpoint":"https://example.com/ping","everySeconds":60,"body":{"x":1}}],
  "allowedHosts": ["example.com"]
}`

func TestParseManifestValid(t *testing.T) {
	m, err := ParseManifest([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("合法 manifest 不应报错: %v", err)
	}
	if m.Name != "server-hello" || m.Version != "1.0.0" {
		t.Fatalf("字段解析错误: name=%q version=%q", m.Name, m.Version)
	}
	if len(m.Pages) != 2 || len(m.Routes) != 2 || len(m.Events) != 1 || len(m.Jobs) != 1 {
		t.Fatalf("声明段数量错误: %d/%d/%d/%d", len(m.Pages), len(m.Routes), len(m.Events), len(m.Jobs))
	}
	if m.Pages[0].Scope != "user" || m.Pages[1].Scope != "admin" {
		t.Fatal("scope 解析错误")
	}
	if !json.Valid(m.Pages[0].Body) {
		t.Fatal("page body 应保持合法 JSON")
	}
	if m.Routes[0].Response == nil || m.Routes[0].Response.Status != 200 {
		t.Fatal("response 规范解析错误")
	}
	if m.Routes[1].Proxy != "https://example.com/hook" {
		t.Fatal("proxy 解析错误")
	}
	if m.Jobs[0].EverySeconds != 60 {
		t.Fatal("everySeconds 解析错误")
	}
}

func TestParseManifestInvalid(t *testing.T) {
	tooManyPages := strings.Replace(validManifestJSON,
		`"pages": [`,
		`"pages": [`+strings.Repeat(`{"id":"x","title":"x","scope":"user","body":[]},`, maxItems+1), 1)

	bigBody := fmt.Sprintf(`{"name":"x","version":"1.0.0","pages":[{"id":"p","title":"t","scope":"user","body":[%q]}],"allowedHosts":[]}`,
		strings.Repeat("a", maxPageBody+1))

	cases := []struct {
		name string
		raw  string
	}{
		{"不是 JSON", `{oops`},
		{"缺少 name", strings.Replace(validManifestJSON, `"name": "server-hello",`, "", 1)},
		{"name 过长", strings.Replace(validManifestJSON, `"name": "server-hello"`, `"name": "`+strings.Repeat("a", maxNameChars+1)+`"`, 1)},
		{"version 非法", strings.Replace(validManifestJSON, `"version": "1.0.0"`, `"version": "v1"`, 1)},
		{"pages 超量", tooManyPages},
		{"scope 非法", strings.Replace(validManifestJSON, `"scope":"user"`, `"scope":"root"`, 1)},
		{"body 非数组", strings.Replace(validManifestJSON, `"body":[{"type":"text","value":"hi"}]`, `"body":{"type":"text"}`, 1)},
		{"body 超 64KB", bigBody},
		{"path 未以斜杠开头", strings.Replace(validManifestJSON, `"path":"/hello"`, `"path":"hello"`, 1)},
		{"path 含遍历", strings.Replace(validManifestJSON, `"path":"/hello"`, `"path":"/a/../b"`, 1)},
		{"method 非法", strings.Replace(validManifestJSON, `"method":"GET"`, `"method":"TRACE"`, 1)},
		{"response 与 proxy 同时配置", strings.Replace(validManifestJSON, `"path":"/hello"`, `"path":"/hello","proxy":"https://example.com/x"`, 1)},
		{"proxy 用 http", strings.Replace(validManifestJSON, `https://example.com/hook`, `http://example.com/hook`, 1)},
		{"proxy 域名不在白名单", strings.Replace(validManifestJSON, `https://example.com/hook`, `https://evil.test/hook`, 1)},
		{"事件类型非法", strings.Replace(validManifestJSON, `"event":"message:new"`, `"event":"user:login"`, 1)},
		{"endpoint 域名不在白名单", strings.Replace(validManifestJSON, `https://example.com/events`, `https://attacker.test/events`, 1)},
		{"任务间隔非法", strings.Replace(validManifestJSON, `"everySeconds":60`, `"everySeconds":0`, 1)},
		{"allowedHost 非法", strings.Replace(validManifestJSON, `"example.com"`, `"bad_host"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.raw)); err == nil {
				t.Fatal("非法 manifest 应报错")
			}
		})
	}
}

func TestParseManifestWASM(t *testing.T) {
	raw := `{
	  "name":"qr-kit","version":"1.0.0","runtime":"wasm","permissions":["auth:qr"],
	  "pages":[],"events":[],"jobs":[],"allowedHosts":[],
	  "routes":[
	    {"method":"POST","path":"/qr/start","handler":true,"public":true},
	    {"method":"POST","path":"/qr/confirm","handler":true}
	  ]
	}`
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("WASM manifest 应合法: %v", err)
	}
	if m.Runtime != "wasm" || len(m.Routes) != 2 || !m.Routes[0].Handler || !m.Routes[0].Public {
		t.Fatalf("WASM 字段解析错误: %+v", m.Routes)
	}
	bad := strings.Replace(raw, `"runtime":"wasm"`, `"runtime":"declarative"`, 1)
	if _, err := ParseManifest([]byte(bad)); err == nil {
		t.Fatal("declarative 运行时不能使用 handler 路由")
	}
}

func TestParseManifestWildcardHost(t *testing.T) {
	raw := strings.Replace(validManifestJSON,
		`"allowedHosts": ["example.com"]`,
		`"allowedHosts": ["*.example.com"]`, 1)
	raw = strings.Replace(raw, `https://example.com/hook`, `https://api.example.com/hook`, 1)
	raw = strings.Replace(raw, `https://example.com/events`, `https://api.example.com/events`, 1)
	raw = strings.Replace(raw, `https://example.com/ping`, `https://api.example.com/ping`, 1)
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("通配子域应可通过: %v", err)
	}
	if len(m.AllowedHosts) != 1 || m.AllowedHosts[0] != "*.example.com" {
		t.Fatal("allowedHosts 解析错误")
	}
	// 根域本身也命中 *.example.com 的语义（与 plugin 包域名规则一致）。
	if err := checkEndpoint("https://example.com/x", m.AllowedHosts); err != nil {
		t.Fatalf("根域应命中通配: %v", err)
	}
	if err := checkEndpoint("https://api.evil.test/x", m.AllowedHosts); err == nil {
		t.Fatal("不在白名单的域名应被拒绝")
	}
}

func TestCheckEndpointRejectsUserInfo(t *testing.T) {
	if err := checkEndpoint("https://user:pass@example.com/x", []string{"example.com"}); err == nil {
		t.Fatal("带用户名密码的 endpoint 必须拒绝")
	}
}
