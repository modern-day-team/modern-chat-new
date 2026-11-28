package plugin

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 版本需求：engines.sdk 与 dependencies 里的 "@^1.2.0" 共用这一套语义。
//
// 刻意只实现插件生态用得到的子集（不支持预发布、构建元数据、|| 多段），
// 理由和权限白名单一致：声明越简单，作者越不容易写错，宿主和开发工具也不会
// 各自解释出不同结果。需要更复杂的范围时，应该加新字段而不是让解析器变复杂。

// Version 固定三段；缺省位补 0（SDK 级别写作 "5" 等价 5.0.0）。
type Version [3]int

var looseVersionRe = regexp.MustCompile(`^\d{1,6}(\.\d{1,6}){0,2}$`)

// ParseVersion 解析 "5" / "1.2" / "1.2.3"，容忍前缀 v。
func ParseVersion(s string) (Version, error) {
	var v Version
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if !looseVersionRe.MatchString(s) {
		return v, fmt.Errorf("%w: 版本 %q 需为 x.y.z 形式", ErrInvalid, s)
	}
	for i, part := range strings.SplitN(s, ".", 3) {
		n, err := strconv.Atoi(part)
		if err != nil {
			return v, fmt.Errorf("%w: 版本 %q 含非法数字", ErrInvalid, s)
		}
		v[i] = n
	}
	return v, nil
}

// String 三段式输出（补零后）。
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// Compare 返回 -1 / 0 / 1。
func (v Version) Compare(o Version) int {
	for i := 0; i < 3; i++ {
		switch {
		case v[i] < o[i]:
			return -1
		case v[i] > o[i]:
			return 1
		}
	}
	return 0
}

// Requirement 单条版本约束，例如 ">=1.2.0" / "^1.2.3" / "1.2.3" / "*"。
type Requirement struct {
	op  string
	ver Version
}

var requirementOps = []string{">=", "<=", "^", "~", ">", "<", "="}

// ParseRequirement 解析约束；裸版本等价于 "="，空串与 "*" 表示任意版本。
func ParseRequirement(s string) (Requirement, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || strings.EqualFold(s, "x") {
		return Requirement{op: "*"}, nil
	}
	op := ""
	for _, p := range requirementOps {
		if strings.HasPrefix(s, p) {
			op = p
			s = strings.TrimSpace(strings.TrimPrefix(s, p))
			break
		}
	}
	v, err := ParseVersion(s)
	if err != nil {
		return Requirement{}, err
	}
	if op == "" {
		op = "="
	}
	return Requirement{op: op, ver: v}, nil
}

// Satisfies 判断版本是否满足约束。
//
// ^ 与 ~ 的边界沿用 npm 语义：^1.2.3 → <2.0.0，^0.2.3 → <0.3.0（0.x 视为不稳定，
// 锁到次版本），~1.2.3 → <1.3.0。插件作者对这两个符号的直觉基本来自 npm，
// 换成自定义边界反而容易出事故。
func (r Requirement) Satisfies(v Version) bool {
	switch r.op {
	case "*":
		return true
	case "=":
		return v.Compare(r.ver) == 0
	case ">=":
		return v.Compare(r.ver) >= 0
	case "<=":
		return v.Compare(r.ver) <= 0
	case ">":
		return v.Compare(r.ver) > 0
	case "<":
		return v.Compare(r.ver) < 0
	case "~":
		upper := Version{r.ver[0], r.ver[1] + 1, 0}
		return v.Compare(r.ver) >= 0 && v.Compare(upper) < 0
	case "^":
		var upper Version
		switch {
		case r.ver[0] > 0:
			upper = Version{r.ver[0] + 1, 0, 0}
		case r.ver[1] > 0:
			upper = Version{0, r.ver[1] + 1, 0}
		default:
			upper = Version{0, 0, r.ver[2] + 1}
		}
		return v.Compare(r.ver) >= 0 && v.Compare(upper) < 0
	}
	return false
}

// String 返回规范化的约束写法（错误信息与 CLI 输出共用）。
func (r Requirement) String() string {
	if r.op == "*" {
		return "*"
	}
	return r.op + r.ver.String()
}

// Dependency 一条依赖声明："name" 或 "name@^1.2.0"。
type Dependency struct {
	Name        string
	Requirement Requirement
	Raw         string
}

// 依赖名只允许这些字符：@ 是分隔符，名字里出现会让 "name@range" 产生歧义；
// 同时它也是跨插件引用的稳定标识，不应该允许空格 / 中文。
var depNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,30}$`)

// ParseDependency 解析一条依赖声明；缺省约束为 "*"。
func ParseDependency(s string) (Dependency, error) {
	raw := strings.TrimSpace(s)
	name, spec, _ := strings.Cut(raw, "@")
	name = strings.TrimSpace(name)
	if !depNameRe.MatchString(name) {
		return Dependency{}, fmt.Errorf("%w: 依赖名 %q 只能用字母数字与 _ . -（1-30 字符）", ErrInvalid, name)
	}
	req, err := ParseRequirement(spec)
	if err != nil {
		return Dependency{}, fmt.Errorf("%w: 依赖 %q 的版本约束非法", ErrInvalid, raw)
	}
	return Dependency{Name: name, Requirement: req, Raw: raw}, nil
}

// ParseDependencies 批量解析，同时拒绝重复与自依赖。
func ParseDependencies(raw []string, self string) ([]Dependency, error) {
	if len(raw) > MaxDependencies {
		return nil, fmt.Errorf("%w: dependencies 最多 %d 项", ErrInvalid, MaxDependencies)
	}
	seen := make(map[string]bool, len(raw))
	out := make([]Dependency, 0, len(raw))
	for _, item := range raw {
		d, err := ParseDependency(item)
		if err != nil {
			return nil, err
		}
		if d.Name == self {
			return nil, fmt.Errorf("%w: 不能依赖自己（%s）", ErrInvalid, self)
		}
		if seen[d.Name] {
			return nil, fmt.Errorf("%w: 依赖 %q 重复声明", ErrInvalid, d.Name)
		}
		seen[d.Name] = true
		out = append(out, d)
	}
	return out, nil
}

// MaxDependencies 单个插件最多声明的依赖数（与 allowedHosts 的 20 项同量级）。
const MaxDependencies = 10

// DepState 调用方查到的「同名依赖当前状态」。
type DepState struct {
	Found   bool
	Enabled bool
	Version Version
}

// UnmetDependencies 返回未满足的依赖描述；空切片表示全部满足。
//
// 三种不满足分清原因很有必要：未安装要提示去装，版本不符要提示升级，
// 已安装但停用则是一键启用就能解决——合成一句「依赖不满足」会让作者无从下手。
func UnmetDependencies(deps []Dependency, lookup func(name string) DepState) []string {
	var unmet []string
	for _, d := range deps {
		st := lookup(d.Name)
		switch {
		case !st.Found:
			unmet = append(unmet, fmt.Sprintf("%s（未安装）", d.Raw))
		case !d.Requirement.Satisfies(st.Version):
			unmet = append(unmet, fmt.Sprintf("%s（已安装 %s）", d.Raw, st.Version))
		case !st.Enabled:
			unmet = append(unmet, fmt.Sprintf("%s（已安装但未启用）", d.Raw))
		}
	}
	return unmet
}
