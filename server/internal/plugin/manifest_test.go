package plugin

import (
	"errors"
	"strings"
	"testing"
)

const validManifest = `/* {
  "name": "weather",
  "version": "1.2.3",
  "description": "天气面板",
  "permissions": ["message:read", "ui:panel", "net"],
  "allowedHosts": ["api.example.com", "*.openweathermap.org"]
} */
function __mcActivate(api) { /* ... */ }`

func TestParseValid(t *testing.T) {
	m, err := Parse(validManifest)
	if err != nil {
		t.Fatalf("expect ok, got %v", err)
	}
	if m.Name != "weather" || m.Version != "1.2.3" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if len(m.AllowedHosts) != 2 {
		t.Fatalf("allowedHosts = %v", m.AllowedHosts)
	}
}

func TestParseSDKV3Permissions(t *testing.T) {
	for _, p := range []string{"ui:page", "data", "ai"} {
		if !ValidPermissions[p] {
			t.Fatalf("SDK v3 权限 %q 未登记", p)
		}
		code := `/* {"name":"v3","version":"1.0.0","permissions":["` + p + `"]} */`
		if _, err := Parse(code); err != nil {
			t.Fatalf("权限 %q 应可安装: %v", p, err)
		}
	}
}

// SDK v5 权限必须与前端权限表、官方 power-kit 示例同步，否则示例插件安装即 400。
func TestParseSDKV5Permissions(t *testing.T) {
	for _, p := range []string{"conv:write", "file:upload"} {
		if !ValidPermissions[p] {
			t.Fatalf("SDK v5 权限 %q 未登记", p)
		}
		code := `/* {"name":"v5","version":"1.0.0","permissions":["` + p + `"]} */`
		if _, err := Parse(code); err != nil {
			t.Fatalf("权限 %q 应可安装: %v", p, err)
		}
	}
	// power-kit 实际 manifest：多权限组合必须整体验证通过。
	powerKit := `/* {"name":"power-kit","version":"1.0.0","permissions":["ui:panel","ui:page","conv:read","conv:write","message:send","file:upload"]} */`
	if _, err := Parse(powerKit); err != nil {
		t.Fatalf("power-kit 示例 manifest 应可安装: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name   string
		code   string
		wantTo bool
	}{
		{"no comment", "function f(){}", true},
		{"unclosed", "/* {\"name\":\"a\"}", true},
		{"bad json", "/* not json */", true},
		{"empty name", `/* {"name":"","version":"1.0.0","permissions":["net"]} */`, true},
		{"bad version", `/* {"name":"a","version":"1.0","permissions":["net"]} */`, true},
		{"unknown permission", `/* {"name":"a","version":"1.0.0","permissions":["root"]} */`, true},
		{"empty permissions", `/* {"name":"a","version":"1.0.0","permissions":[]} */`, true},
		{"bad host", `/* {"name":"a","version":"1.0.0","permissions":["net"],"allowedHosts":["-bad-"]} */`, true},
		{"too many hosts", `/* {"name":"a","version":"1.0.0","permissions":["net"],"allowedHosts":["` +
			strings.Repeat("h.com,", 25) + `h.com"]} */`, true},
		{"wildcard ok", `/* {"name":"a","version":"1.0.0","permissions":["net"],"allowedHosts":["*.example.com"]} */`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.code)
			if tc.wantTo && !errors.Is(err, ErrInvalid) {
				t.Fatalf("expect ErrInvalid, got %v", err)
			}
			if !tc.wantTo && err != nil {
				t.Fatalf("expect ok, got %v", err)
			}
		})
	}
}
