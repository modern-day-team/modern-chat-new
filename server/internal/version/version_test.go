package version

import (
	"testing"
)

func TestEnvOr(t *testing.T) {
	if got := envOr("dev", "dev", "TEST_MC_VERSION"); got != "dev" {
		t.Fatalf("未设置环境变量时应保留默认值，got %q", got)
	}
	t.Setenv("TEST_MC_VERSION", "0.12.0")
	if got := envOr("dev", "dev", "TEST_MC_VERSION"); got != "0.12.0" {
		t.Fatalf("环境变量未生效，got %q", got)
	}
	if got := envOr("v1.2.3", "dev", "TEST_MC_VERSION"); got != "v1.2.3" {
		t.Fatalf("ldflags 注入值应优先，got %q", got)
	}
}
