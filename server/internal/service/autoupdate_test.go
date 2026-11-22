package service

import (
	"testing"
	"time"
)

func TestLooksLikeVersion(t *testing.T) {
	good := []string{"v0.11.0", "0.11.0", "v1.2.3", "v2.0.0-beta.1", "  v3.4.5  "}
	bad := []string{"", "v", "latest", "stable", "v1", "abc.def", "v1.2.3/evil"}
	for _, tag := range good {
		if !looksLikeVersion(tag) {
			t.Errorf("expected version-like: %q", tag)
		}
	}
	for _, tag := range bad {
		if looksLikeVersion(tag) {
			t.Errorf("expected NOT version-like: %q", tag)
		}
	}
}

// 构造一个未装配到真实部署的 Updater（rootDir 为空，Available=false），
// 用于验证 Start() 在不支持的部署方式下安全跳过、不启动 goroutine。
func TestAutoSchedulerSkipOnUnavailable(t *testing.T) {
	u := NewUpdater("", "8080", ReleaseSource{})
	s := NewAutoScheduler(u, 10*time.Minute, nil)
	if s.Start() {
		t.Fatal("scheduler should not start on unavailable deployment")
	}
	st := s.State()
	if st.Enabled || st.SkipReason == "" {
		t.Fatalf("expected disabled with skip reason, got %+v", st)
	}
	s.Stop()
}

func TestAutoSchedulerStateDefaults(t *testing.T) {
	s := NewAutoScheduler(nil, 10*time.Minute, nil)
	st := s.State()
	if st.IntervalMin != 10 || st.Result != "idle" {
		t.Fatalf("unexpected defaults: %+v", st)
	}
}

// tick 在检查失败时应记录 check_failed 且不触发回调；用 nil updater 无法直接
// 调 tick（会 panic），这里用一个 Available=false 的 updater：Check 会先返回错误。
func TestAutoSchedulerTickCheckFailure(t *testing.T) {
	u := NewUpdater("", "8080", ReleaseSource{})
	s := NewAutoScheduler(u, 10*time.Minute, func(string, string, string) {
		t.Fatal("trigger should not be called on check failure")
	})
	s.tick()
	st := s.State()
	if st.Result != "check_failed" || st.LastError == "" || st.NextCheck == "" {
		t.Fatalf("expected check_failed state with error and next check, got %+v", st)
	}
}

// 注入 check/start，验证核心判定：发现新 tag → 触发 → 回调收到 tag。
func TestAutoSchedulerTickTriggersOnNewTag(t *testing.T) {
	s := NewAutoScheduler(nil, 10*time.Minute, nil)
	s.check = func() (CheckResult, error) {
		return CheckResult{Local: "v0.11.0", Remote: "v0.12.0", RemoteTag: "v0.12.0", UpToDate: false}, nil
	}
	started := false
	s.start = func() bool { started = true; return true }
	gotTag := ""
	s.onTrigger = func(tag, local, remote string) { gotTag = tag }

	s.tick()
	if !started || gotTag != "v0.12.0" {
		t.Fatalf("expected trigger with tag v0.12.0, started=%v tag=%q", started, gotTag)
	}
	st := s.State()
	if st.Result != "triggered" || st.TriggeredTag != "v0.12.0" || st.UpToDate {
		t.Fatalf("unexpected state: %+v", st)
	}
}

// 已是最新版则不触发。
func TestAutoSchedulerTickUpToDate(t *testing.T) {
	s := NewAutoScheduler(nil, 10*time.Minute, nil)
	s.check = func() (CheckResult, error) {
		return CheckResult{Local: "v0.11.0", Remote: "v0.11.0", RemoteTag: "v0.11.0", UpToDate: true}, nil
	}
	s.start = func() bool { t.Fatal("start should not be called"); return false }
	s.tick()
	if s.State().Result != "up_to_date" {
		t.Fatalf("expected up_to_date, got %s", s.State().Result)
	}
}

// 畸形 tag 与 busy 都应跳过，不触发升级。
func TestAutoSchedulerTickSkips(t *testing.T) {
	cases := []struct {
		name  string
		res   CheckResult
		start bool
		want  string
	}{
		{"malformed", CheckResult{RemoteTag: "latest", UpToDate: false}, true, "skipped"},
		{"busy", CheckResult{RemoteTag: "v0.12.0", UpToDate: false}, false, "skipped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewAutoScheduler(nil, 10*time.Minute, nil)
			s.check = func() (CheckResult, error) { return tc.res, nil }
			s.start = func() bool { return tc.start }
			s.onTrigger = func(string, string, string) { t.Fatal("trigger should not fire") }
			s.tick()
			if s.State().Result != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, s.State().Result)
			}
		})
	}
}

// 验证 setState/State 的并发安全（slog 与轮询可能同时读写）。
func TestAutoSchedulerConcurrentState(t *testing.T) {
	s := NewAutoScheduler(nil, 10*time.Minute, nil)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 500; i++ {
			s.setState(func(st *AutoUpdateState) { st.Result = "up_to_date" })
		}
		close(done)
	}()
	for i := 0; i < 500; i++ {
		_ = s.State()
	}
	<-done
	if s.State().Result != "up_to_date" {
		t.Fatal("state not updated")
	}
}
