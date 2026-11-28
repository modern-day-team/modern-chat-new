package plugin

import (
	"errors"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		bad  bool
	}{
		{in: "5", want: Version{5, 0, 0}},
		{in: "1.2", want: Version{1, 2, 0}},
		{in: "1.2.3", want: Version{1, 2, 3}},
		{in: "v1.2.3", want: Version{1, 2, 3}},
		{in: " 1.2.3 ", want: Version{1, 2, 3}},
		{in: "1.2.3.4", bad: true},
		{in: "1.x", bad: true},
		{in: "", bad: true},
		{in: "1.2.3-beta", bad: true},
	}
	for _, tc := range cases {
		got, err := ParseVersion(tc.in)
		if tc.bad {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%q 应非法，got %v", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q 应合法: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q => %v，期望 %v", tc.in, got, tc.want)
		}
	}
}

func TestRequirementSatisfies(t *testing.T) {
	cases := []struct {
		req  string
		ver  string
		want bool
	}{
		// 通配
		{"*", "9.9.9", true},
		{"", "0.0.1", true},
		// 比较
		{">=5", "5", true},
		{">=5", "4.9.9", false},
		{">5", "5.0.0", false},
		{"<2.0.0", "1.9.9", true},
		{"<=1.2.3", "1.2.3", true},
		{"1.2.3", "1.2.3", true},
		{"=1.2.3", "1.2.4", false},
		// ~：锁次版本
		{"~1.2.3", "1.2.9", true},
		{"~1.2.3", "1.3.0", false},
		// ^：锁主版本
		{"^1.2.3", "1.9.9", true},
		{"^1.2.3", "2.0.0", false},
		{"^1.2.3", "1.2.2", false},
		// 0.x 视为不稳定，^ 锁到次版本
		{"^0.2.3", "0.2.9", true},
		{"^0.2.3", "0.3.0", false},
		{"^0.0.3", "0.0.3", true},
		{"^0.0.3", "0.0.4", false},
	}
	for _, tc := range cases {
		req, err := ParseRequirement(tc.req)
		if err != nil {
			t.Fatalf("解析 %q 失败: %v", tc.req, err)
		}
		v, err := ParseVersion(tc.ver)
		if err != nil {
			t.Fatalf("解析版本 %q 失败: %v", tc.ver, err)
		}
		if got := req.Satisfies(v); got != tc.want {
			t.Fatalf("%q 对 %q = %v，期望 %v", tc.req, tc.ver, got, tc.want)
		}
	}
}

func TestParseDependency(t *testing.T) {
	d, err := ParseDependency("word-count@^1.2.0")
	if err != nil {
		t.Fatalf("应合法: %v", err)
	}
	if d.Name != "word-count" || d.Requirement.String() != "^1.2.0" {
		t.Fatalf("解析结果异常: %+v", d)
	}
	// 省略约束等价于任意版本；缺省位补零（1.0 视为 1.0.0）
	d, err = ParseDependency("word-count")
	if err != nil || d.Requirement.String() != "*" {
		t.Fatalf("无约束解析异常: %+v %v", d, err)
	}
	d, err = ParseDependency("word-count@1.0")
	if err != nil || d.Requirement.String() != "=1.0.0" {
		t.Fatalf("两段版本应补零: %+v %v", d, err)
	}
	for _, bad := range []string{"", "@1.0.0", "有中文@1.0.0", "a@1.2.3.4"} {
		if _, err := ParseDependency(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%q 应非法，got %v", bad, err)
		}
	}
}

func TestParseDependenciesRejectsSelfAndDupes(t *testing.T) {
	if _, err := ParseDependencies([]string{"me@^1.0.0"}, "me"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("自依赖应被拒: %v", err)
	}
	if _, err := ParseDependencies([]string{"a@1.0.0", "a@2.0.0"}, "me"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("重复依赖应被拒: %v", err)
	}
	got, err := ParseDependencies([]string{"a@1.0.0", "b"}, "me")
	if err != nil || len(got) != 2 {
		t.Fatalf("合法依赖解析异常: %+v %v", got, err)
	}
}

func TestUnmetDependencies(t *testing.T) {
	deps, err := ParseDependencies([]string{"a@^1.0.0", "b@^2.0.0", "c"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]DepState{
		"a": {Found: true, Enabled: true, Version: Version{1, 5, 0}},
		"b": {Found: true, Enabled: true, Version: Version{2, 9, 0}},
		// c 故意缺失
	}
	unmet := UnmetDependencies(deps, func(n string) DepState { return states[n] })
	if len(unmet) != 1 || unmet[0] != "c（未安装）" {
		t.Fatalf("未满足项不符: %v", unmet)
	}
	// 已安装但停用 / 版本不符 要给出可操作的提示
	states["b"] = DepState{Found: true, Enabled: false, Version: Version{2, 9, 0}}
	if unmet := UnmetDependencies(deps, func(n string) DepState { return states[n] }); len(unmet) != 2 {
		t.Fatalf("应有两项未满足: %v", unmet)
	}
}

func TestManifestEngines(t *testing.T) {
	ok := `/* {"name":"a","version":"1.0.0","permissions":["ui:panel"],"engines":{"sdk":">=5"}} */`
	m, err := Parse(ok)
	if err != nil {
		t.Fatalf("engines 声明应合法: %v", err)
	}
	if err := m.CheckEngines(SDKLevel); err != nil {
		t.Fatalf("当前 SDK 应满足: %v", err)
	}
	if err := m.CheckEngines(4); err == nil {
		t.Fatal("SDK 4 不应满足 >=5")
	}

	for _, bad := range []string{
		`/* {"name":"a","version":"1.0.0","permissions":["ui:panel"],"engines":{"sdks":">=5"}} */`, // 拼错的键
		`/* {"name":"a","version":"1.0.0","permissions":["ui:panel"],"engines":{"sdk":""}} */`,     // 声明了但留空
		`/* {"name":"a","version":"1.0.0","permissions":["ui:panel"],"engines":{"sdk":"abc"}} */`,
	} {
		if _, err := Parse(bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("应被拒: %s => %v", bad, err)
		}
	}
	// 空对象等于没约束，合法；这样从模板里删掉内容不会突然装不上
	if _, err := Parse(`/* {"name":"a","version":"1.0.0","permissions":["ui:panel"],"engines":{}} */`); err != nil {
		t.Fatalf("空 engines 应视为无约束: %v", err)
	}
}

func TestManifestDependencies(t *testing.T) {
	code := `/* {"name":"deps-demo","version":"1.0.0","permissions":["ui:panel"],"dependencies":["word-count@^1.0.0"]} */`
	m, err := Parse(code)
	if err != nil {
		t.Fatalf("依赖声明应合法: %v", err)
	}
	unmet := m.CheckDependencies(func(n string) DepState {
		if n == "word-count" {
			return DepState{Found: true, Enabled: false, Version: Version{1, 0, 0}}
		}
		return DepState{}
	})
	if len(unmet) != 1 {
		t.Fatalf("应报告停用的依赖: %v", unmet)
	}

	if _, err := Parse(`/* {"name":"a","version":"1.0.0","permissions":["net"],"dependencies":["a@1.0.0"]} */`); !errors.Is(err, ErrInvalid) {
		t.Fatalf("自依赖应被拒: %v", err)
	}
}
