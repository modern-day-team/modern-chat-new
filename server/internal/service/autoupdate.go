package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// 自动更新调度器，独立于图形化更新的业务层。设计取舍（与「应用内升级」的安全边界一致）：
//
//   - 只在发行版模式 + systemd 部署生效：tag 是 Release 的概念，源码模式按
//     origin/main commit 走，且自动 git pull + 本地编译风险显著更高——源码部署
//     显式不做自动更新，由维护者自己跑 scripts/update.sh。
//   - 默认开启但依赖 WEB_UPDATE 总开关：WEB_UPDATE=false 时 updater 不装配，
//     自动调度也不会启动（避免绕过管理员关停图形化更新的决定）。
//   - 只触发已有的 Updater.Start()：下载 / sha256 校验 / 解压路径校验 / 原子替换
//     全部复用图形化更新那一套，不新写一条"自动专用"的更新链路。
//   - 新版本判定复用 Check()：发行版查 releases/latest 的 tag_name 与本地
//     version.Version 比对；启动后先等一个间隔再首次检查，给服务留启动时间。
//   - 失败不阻塞下一次检查；busy（手动更新进行中）时跳过本轮，不排队、不抢占。
//   - 审计由回调注入（main 层用 store.AuditStore 写），service 不依赖 store,
//     避免 service → store 的依赖方向反转。

// AutoUpdateState 自动更新运行态快照（并入后台 Status 接口展示）。
type AutoUpdateState struct {
	Enabled      bool   `json:"enabled"`
	Available    bool   `json:"available"`
	IntervalMin  int    `json:"intervalMin"`
	LastCheck    string `json:"lastCheck,omitempty"` // RFC3339，最近一次检查时间
	LastTag      string `json:"lastTag,omitempty"`   // 最近一次看到的远端 tag
	UpToDate     bool   `json:"upToDate"`
	Result       string `json:"result,omitempty"`       // idle / up_to_date / triggered / trigger_failed / check_failed / skipped
	LastError    string `json:"lastError,omitempty"`    // 最近一次检查/触发的错误
	NextCheck    string `json:"nextCheck,omitempty"`    // RFC3339，下次检查计划时间
	TriggeredTag string `json:"triggeredTag,omitempty"` // 最近一次成功触发升级的 tag
	SkipReason   string `json:"skipReason,omitempty"`   // 未启用/不可用的原因（面向运维）
}

// AutoUpdateTrigger 自动更新触发回调（main 层写审计）。
type AutoUpdateTrigger func(tag, local, remote string)

// AutoScheduler 定时检查新版本并触发升级。
// check/start 为可注入函数（默认指向 updater 的同名方法），便于单测驱动核心判定。
type AutoScheduler struct {
	updater   *Updater
	interval  time.Duration
	onTrigger AutoUpdateTrigger
	check     func() (CheckResult, error)
	start     func() bool

	mu    sync.Mutex
	state AutoUpdateState
	stop  context.CancelFunc
}

// NewAutoScheduler 构造调度器。interval 已在 config 层做过下限裁剪。
func NewAutoScheduler(u *Updater, interval time.Duration, onTrigger AutoUpdateTrigger) *AutoScheduler {
	if interval < time.Minute {
		interval = 30 * time.Minute
	}
	s := &AutoScheduler{
		updater:   u,
		interval:  interval,
		onTrigger: onTrigger,
		state: AutoUpdateState{
			IntervalMin: int(interval / time.Minute),
			Result:      "idle",
		},
	}
	if u != nil {
		s.check = u.Check
		s.start = u.Start
	}
	return s
}

// State 返回状态快照。
func (s *AutoScheduler) State() AutoUpdateState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.state
	return out
}

func (s *AutoScheduler) setState(mut func(*AutoUpdateState)) {
	s.mu.Lock()
	mut(&s.state)
	s.mu.Unlock()
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Start 启动定时调度。返回是否真正启动（发行版 + systemd 部署、且 updater 可用）。
// 源码模式 / 不支持的部署方式返回 false 并写入 skipReason，供后台展示。
func (s *AutoScheduler) Start() bool {
	if s.updater == nil {
		s.setState(func(st *AutoUpdateState) { st.SkipReason = "更新功能未装配（WEB_UPDATE=false）" })
		return false
	}
	if !s.updater.Available() {
		s.setState(func(st *AutoUpdateState) {
			st.SkipReason = "当前部署方式不支持自动更新（需要 install.sh 的 systemd 发行版部署）"
		})
		return false
	}
	if s.updater.Mode() != "release" {
		s.setState(func(st *AutoUpdateState) {
			st.SkipReason = "源码部署不自动更新；请维护者手动执行 scripts/update.sh"
		})
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.stop = cancel
	s.state.Enabled = true
	s.state.Available = true
	s.state.NextCheck = rfc3339(time.Now().Add(s.interval))
	s.mu.Unlock()

	slog.Info("auto update enabled",
		"intervalMin", int(s.interval/time.Minute),
		"mode", s.updater.Mode(), "repo", s.updater.Repo(), "port", s.updater.Port())
	go s.loop(ctx)
	return true
}

// Stop 停止调度（进程内使用；优雅停机时由 main 调用）。
func (s *AutoScheduler) Stop() {
	s.mu.Lock()
	cancel := s.stop
	s.stop = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *AutoScheduler) loop(ctx context.Context) {
	// 先等一个间隔再首次检查：避免刚启动就重启打断启动流程 / 健康检查。
	timer := time.NewTimer(s.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.tick()
			timer.Reset(s.interval)
		}
	}
}

// tick 执行一次检查 + 可能的触发。导出为方法便于单元测试直接驱动。
func (s *AutoScheduler) tick() {
	next := rfc3339(time.Now().Add(s.interval))
	s.setState(func(st *AutoUpdateState) { st.NextCheck = next })

	if s.check == nil {
		s.setState(func(st *AutoUpdateState) {
			st.Result = "check_failed"
			st.LastError = "更新器未装配"
		})
		return
	}
	res, err := s.check()
	if err != nil {
		msg := err.Error()
		s.setState(func(st *AutoUpdateState) {
			st.LastCheck = rfc3339(time.Now())
			st.Result = "check_failed"
			st.LastError = msg
		})
		slog.Warn("auto update check failed", "err", msg, "nextCheck", next)
		return
	}

	s.setState(func(st *AutoUpdateState) {
		st.LastCheck = rfc3339(time.Now())
		st.LastTag = res.RemoteTag
		st.LastError = ""
	})

	if res.UpToDate {
		s.setState(func(st *AutoUpdateState) {
			st.Result = "up_to_date"
			st.UpToDate = true
		})
		return
	}

	// 版本比较口径：Check 已经按 tag 与本地版本比对。这里再加一道防御——
	// 远端 tag 必须看起来像个版本号（vX.Y.Z 或 X.Y.Z），避免把畸形 tag 当作升级信号。
	if !looksLikeVersion(res.RemoteTag) {
		s.setState(func(st *AutoUpdateState) {
			st.Result = "skipped"
			st.LastError = "远端 tag 不是版本号：" + res.RemoteTag
		})
		slog.Warn("auto update skipped malformed tag", "tag", res.RemoteTag)
		return
	}

	if s.start == nil || !s.start() {
		// busy 或部署突然不可用：本轮跳过，下一轮再试（不排队，避免陈旧任务堆积）
		s.setState(func(st *AutoUpdateState) {
			st.Result = "skipped"
			st.LastError = "更新任务繁忙或部署已不可用，本轮跳过"
		})
		return
	}

	s.setState(func(st *AutoUpdateState) {
		st.Result = "triggered"
		st.TriggeredTag = res.RemoteTag
		st.UpToDate = false
	})
	slog.Info("auto update triggered", "local", res.Local, "remote", res.RemoteTag)
	if s.onTrigger != nil {
		s.onTrigger(res.RemoteTag, res.Local, res.Remote)
	}
}

// looksLikeVersion tag 形态：v0.11.0 / 0.11.0 / v1.2.3-beta.1。
// 只要求「主版本.次版本」为纯数字开头，后半段允许 semver 预发布/构建元数据里的
// 字母数字与连字符；目的在于挡住 "latest" / "abc.def" 这类畸形 tag，而非完整 semver 排序。
func looksLikeVersion(tag string) bool {
	core := strings.TrimSpace(tag)
	core = strings.TrimPrefix(core, "v")
	// 预发布 / 构建元数据与版本主体分离，只对主体做数字校验
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}
