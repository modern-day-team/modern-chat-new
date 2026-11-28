package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/modern-day-team/modern-chat-new/server/internal/cluster"
	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/serverplugin"
	"github.com/modern-day-team/modern-chat-new/server/internal/service"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

const ShardCount = 64

// kickGrace 踢线兜底延迟：writePump 正常时立即刷帧并关连接；
// 若 writePump 已退出（半死连接），靠该定时器保证连接一定被关闭。
const kickGrace = 2 * time.Second

// 连接准入上限：此前同一账号可无限建连，每条连接带 3 个 goroutine 与
// 256 帧的发送积压（慢消费者判定前最多可排 15MB），资源消耗没有任何上界。
// 单账号 8 条覆盖手机/桌面/平板加若干标签页；同 sid 的多标签页共用登录态，
// 因此不做「同 sid 替换旧连接」（那会踢掉正常的多标签页），改用账号上限收敛。
const (
	maxConnsPerUser = 8
	// maxConnsPerIP 单来源 IP 并发连接上限。回环来源被 quotaIP 视为同机反代
	// 而不计配额（否则所有经 nginx 的用户会合并到同一个 IP）。
	maxConnsPerIP = 50
	// 握手频率：每来源 IP 每秒 2 次、突发 20。每次握手都要查用户、会话表并
	// 触发一次 presence 广播，需要比连接数更细的入口限制。
	handshakeRatePerSec = 2
	handshakeBurst      = 20
	// ipLimiterTTL 空闲来源的握手限流桶保留时长，避免长跑实例无限累积 map。
	ipLimiterTTL = 10 * time.Minute
)

// connSweepInterval 只读连接的定时复核周期。readPump 只有在收到上行帧时才会
// 走 Dispatch → checkConnState，纯收消息的连接（脚本监听广播、隐藏标签页被
// 浏览器节流心跳）可能长期不触发；独立定时扫描保证会话撤销/封禁最迟一个
// 周期内落到既有连接上。
const connSweepInterval = 45 * time.Second

type shard struct {
	mu    sync.RWMutex
	conns map[uint]map[*Client]struct{} // userID -> 该用户全部连接
}

// Deps Hub 依赖：stores + 消息限流器。
type Deps struct {
	Convs      *store.ConversationStore
	Messages   *store.MessageStore
	Friends    *store.FriendStore
	Users      *store.UserStore       // 连接级封禁 / token_version 复核（可为 nil）
	Moderation *store.ModerationStore // 单聊拉黑拦截（可为 nil，测试环境跳过）
	MsgLimiter middleware.Limiter     // 每用户 20 条/分（内存或 Redis）
	// Storage 用于校验图片/语音消息的 URL 归属（可为 nil，测试环境走本地前缀兜底）
	Storage service.Storage
	// Settings 提供可运营的聊天规则（消息长度、撤回时限）。可为 nil，回落默认值。
	Settings *store.SettingStore
	// Push / PushSubs Web Push：离线用户的新消息提醒（Push 为 nil 或未配置时跳过）
	Push     *service.PushService
	PushSubs *store.PushStore
	// Events 服务端插件事件总线（message:new 等；可为 nil）
	Events *serverplugin.Runtime
	// ClusterBus 跨实例事件总线（可为 nil：单实例部署不广播踢线）
	Broadcaster EventBroadcaster
	// ClusterEvents 接收其它实例的踢线事件（可为 nil）
	ClusterEvents EventSubscriber
}

// EventBroadcaster 由 ws 消费方定义的窄接口：只负责广播踢线事件。
type EventBroadcaster interface {
	Publish(ev cluster.Event) error
}

// EventSubscriber 由 ws 消费方定义的窄接口：只负责订阅并回调。
type EventSubscriber interface {
	Subscribe(ctx context.Context, onMessage func(cluster.Event)) error
}

// Hub 在线连接注册表（按用户分片锁）。
type Hub struct {
	shards [ShardCount]*shard
	// presenceMu 上下线广播的分片互斥（F1）：同一用户的 online/offline 广播
	// 串行化，避免重连竞态导致好友端在线状态颠倒。
	presenceMu [ShardCount]sync.Mutex
	deps       *Deps
	// membersCache 会话成员 id 列表短 TTL 缓存（见 membercache.go），
	// REST 侧成员变更经 Notifier.InvalidateMembers 失效。
	membersCache sync.Map // convID -> *membersEntry
	// relatedCache 用户「相关受众」短 TTL 缓存（见 presence.go）：上下线广播
	// 每次要取同会话全部成员 ∪ 好友，最热的高频查库路径之一。
	relatedCache sync.Map // userID -> *relatedEntry
	// calls 一对一通话的 WS 信令状态（只内存，不落库；二期再考虑跨实例共享）
	calls *callManager
	// sessions 登录会话表。Hub 在 main 中先于 router 构造，SessionStore 由
	// router 创建后经 Serve 传入，因此只能在握手时回填（见 setSessions）；
	// 连接级复核与定时巡检据此判断 sid 是否已被撤销。
	sessions atomic.Pointer[store.SessionStore]
	// quotaMu 保护按来源 IP 的并发连接计数与握手限流桶。
	quotaMu    sync.Mutex
	ipConns    map[string]int
	ipLimiters map[string]*ipLimiterEntry
	done       chan struct{}
	// clusterCancel 取消跨实例事件订阅（Close 时调用，与 done 分开：done 是 Hub 生命周期）。
	clusterCancel context.CancelFunc
	closed        bool
	mu            sync.RWMutex
}

// Client 一条 WS 连接。
type Client struct {
	UserID uint
	// tv JWT 中的 token_version（握手时已校验）；handleChat 用它做连接级复核。
	tv int64
	// sid JWT 中的会话 ID；KickSession 按它单独断开某台设备，不影响其他连接。
	sid string
	// ip 握手来源地址（quotaIP 归一化后的配额键）；Unregister 时释放 IP 名额。
	ip   string
	Conn *websocket.Conn
	send chan []byte
	hub  *Hub

	// kickCh 关闭请求：由 KickUser / 慢消费者判定关闭（只关一次），
	// writePump 收到后发 WS 关闭帧退出。kickFlush=true 时先冲刷队列
	//（KickUser 需要把 kicked 帧送达），false 时立即退出（慢消费者）。
	// 注意：c.send 的关闭权仍只属于 Unregister，本结构绝不 close(c.send)。
	kickCh    chan struct{}
	kickFlush atomic.Bool
	kickOnce  sync.Once
	// pendingClose 慢消费者待关闭标记（atomic 保证同一连接只收集一次，
	// 避免 ToUsers 在锁内阻塞，也避免重复触发关闭）。
	pendingClose atomic.Bool
	// sendOnce 保证 c.send 只被 close 一次（Unregister 幂等保护）。
	sendOnce sync.Once
	// ctrl 非聊天上行帧（typing/read/recall/edit/reaction/delivered）的每连接
	// 令牌桶（F2）：serve.go 装配；测试裸构造 Client 时由 allowCtrlFrame 惰性创建。
	ctrl *rate.Limiter
	// connStateAt 上次连接级复核（checkConnState）成功的 UnixNano：短 TTL 内
	// 复用结果，避免高频帧每帧一次 Users.ByID 查库（F2）。封禁 / 重置密码均由
	// KickUser 立即断连兜底，此处只是把防御性复核降频。
	connStateAt atomic.Int64
	// lastInbound 最近一次收到上行帧的 UnixNano（握手时初始化）。writePump
	// 据此判断「只收不发」的连接，超过 maxSilentLifetime 主动断开强制重连鉴权。
	lastInbound atomic.Int64
}

func newShard() *shard { return &shard{conns: make(map[uint]map[*Client]struct{})} }

// NewHub 创建 Hub（deps 可为 nil：仅注册表功能，供测试）。
func NewHub(deps *Deps) *Hub {
	h := &Hub{
		deps: deps, done: make(chan struct{}), calls: newCallManager(),
		ipConns: make(map[string]int), ipLimiters: make(map[string]*ipLimiterEntry),
	}
	for i := range h.shards {
		h.shards[i] = newShard()
	}
	return h
}

// setSessions 回填会话表（Serve 每次握手调用，后写覆盖；nil 忽略）。
// Hub 与 SessionStore 的构造顺序决定了这里只能后注入，而不是走 Deps。
func (h *Hub) setSessions(s *store.SessionStore) {
	if s != nil {
		h.sessions.Store(s)
	}
}

// Run 阻塞至 Close，并定期清扫过期的成员列表缓存。
// TTL 30s 只表示"过期后重查"，此前过期条目永不删除：十年运行的站点上
// 每个曾用过的 convID 都常驻一份成员 ID 切片（可占数十 MB）。每 5 分钟
// 扫一遍即可，清扫本身 O(缓存条目数)，频率足够低。
func (h *Hub) Run() {
	cleanup := time.NewTicker(5 * time.Minute)
	defer cleanup.Stop()
	// connSweep 独立于上行帧的定时复核：踢线事件只广播一次，pub/sub 未送达
	// （多实例未配 Redis / 总线故障）或与握手竞态漏踢时，靠它最迟一个周期
	// 发现会话已撤销并关闭连接。
	connSweep := time.NewTicker(connSweepInterval)
	defer connSweep.Stop()
	// 跨实例踢线订阅：单实例（未注入）时不启动。首次建连失败只告警、不取消 ctx，
	// 由总线内部指数退避自动重连；本实例始终保留 5s 连接级复核兜底。
	if h.deps != nil && h.deps.ClusterEvents != nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.mu.Lock()
		h.clusterCancel = cancel
		h.mu.Unlock()
		if err := h.deps.ClusterEvents.Subscribe(ctx, func(ev cluster.Event) {
			switch ev.Kind {
			case cluster.KindKickUser:
				h.kickUserLocal(ev.UID, ev.Reason)
			case cluster.KindKickSession:
				h.kickSessionLocal(ev.SID, ev.Reason)
			}
		}); err != nil {
			// 不 cancel：Subscribe 内部仍在重试，ctx 只在 Hub.Close 时取消。
			slog.Warn("cluster kick subscription initial connect failed, will retry", "err", err.Error())
		}
	}
	for {
		select {
		case <-h.done:
			return
		case <-cleanup.C:
			h.evictExpiredMembers()
			h.evictIdleIPs()
		case <-connSweep.C:
			h.sweepConns()
		}
	}
}

// evictExpiredMembers 删除超过 TTL 未访问的缓存条目（供单测直接驱动）。
func (h *Hub) evictExpiredMembers() {
	now := time.Now()
	h.membersCache.Range(func(key, value any) bool {
		if e, ok := value.(*membersEntry); ok && now.Sub(e.at) >= membersCacheTTL {
			h.membersCache.Delete(key)
		}
		return true
	})
	h.relatedCache.Range(func(key, value any) bool {
		if e, ok := value.(*relatedEntry); ok && now.Sub(e.at) >= relatedCacheTTL {
			h.relatedCache.Delete(key)
		}
		return true
	})
}

// evictIdleIPs 清理长时间未握手的来源 IP 限流桶（并发连接计数为 0 时整条删除，
// 非 0 的保留计数以免配额被绕过）。
func (h *Hub) evictIdleIPs() {
	cutoff := time.Now().Add(-ipLimiterTTL)
	h.quotaMu.Lock()
	defer h.quotaMu.Unlock()
	for ip, e := range h.ipLimiters {
		if e.at.Before(cutoff) {
			delete(h.ipLimiters, ip)
		}
	}
	for ip, n := range h.ipConns {
		if n <= 0 {
			delete(h.ipConns, ip)
		}
	}
}

// sweepConns 定时复核全部既有连接（见 connSweepInterval）。
// 只收集指针、在分片锁外判定：loadConnState 会查库，不能持锁；失效连接只用
// requestKick 关闭，不投递 error 帧 —— Unregister 会并发关闭 c.send，
// 锁外 trySend 有 send on closed channel 的风险，而 requestKick 只关信号 channel。
func (h *Hub) sweepConns() {
	var conns []*Client
	for i := range h.shards {
		s := h.shards[i]
		s.mu.RLock()
		for _, set := range s.conns {
			for c := range set {
				conns = append(conns, c)
			}
		}
		s.mu.RUnlock()
	}
	for _, c := range conns {
		if time.Since(time.Unix(0, c.connStateAt.Load())) < connStateTTL {
			continue // 刚由上行帧复核过
		}
		reason, err := h.loadConnState(c)
		if err != nil {
			slog.Warn("ws conn sweep lookup failed, skip", "uid", c.UserID, "err", err.Error())
			continue
		}
		if reason == "" {
			c.connStateAt.Store(time.Now().UnixNano())
			continue
		}
		slog.Warn("ws conn swept: state changed", "uid", c.UserID, "sid", c.sid, "reason", reason)
		c.requestKick(true)
	}
}

// IsOnline 该用户当前是否有任意在线连接。
func (h *Hub) IsOnline(uid uint) bool {
	s := h.shards[uid%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns[uid]) > 0
}

// ConnCount 用户当前连接数（握手准入判断用）。
func (h *Hub) ConnCount(userID uint) int {
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns[userID])
}

// ipLimiterEntry 每来源 IP 的握手令牌桶 + 最近使用时刻（供空闲清理）。
type ipLimiterEntry struct {
	lim *rate.Limiter
	at  time.Time
}

// allowHandshake 握手频率限制。每次握手都会查用户、Touch 会话表并广播 presence，
// 连接数上限拦不住「连上即断」的循环。ip 为空（未知来源 / 回环反代）时放行。
func (h *Hub) allowHandshake(ip string) bool {
	if ip == "" {
		return true
	}
	h.quotaMu.Lock()
	defer h.quotaMu.Unlock()
	e := h.ipLimiters[ip]
	if e == nil {
		e = &ipLimiterEntry{lim: rate.NewLimiter(rate.Limit(handshakeRatePerSec), handshakeBurst)}
		h.ipLimiters[ip] = e
	}
	e.at = time.Now()
	return e.lim.Allow()
}

// acquireIPSlot 占用一个来源 IP 的并发连接名额（握手前调用，失败返回 false）。
func (h *Hub) acquireIPSlot(ip string) bool {
	if ip == "" {
		return true
	}
	h.quotaMu.Lock()
	defer h.quotaMu.Unlock()
	if h.ipConns[ip] >= maxConnsPerIP {
		return false
	}
	h.ipConns[ip]++
	return true
}

// releaseIPSlot 释放来源 IP 名额（握手失败或连接下线时调用，与 acquire 严格配对）。
func (h *Hub) releaseIPSlot(ip string) {
	if ip == "" {
		return
	}
	h.quotaMu.Lock()
	defer h.quotaMu.Unlock()
	if n := h.ipConns[ip]; n <= 1 {
		delete(h.ipConns, ip)
	} else {
		h.ipConns[ip] = n - 1
	}
}

// Register 连接上线。
func (h *Hub) Register(c *Client) {
	s := h.shards[c.UserID%ShardCount]
	s.mu.Lock()
	if s.conns[c.UserID] == nil {
		s.conns[c.UserID] = make(map[*Client]struct{})
	}
	s.conns[c.UserID][c] = struct{}{}
	s.mu.Unlock()
	slog.Debug("ws register", "uid", c.UserID)
}

// Unregister 连接下线（引用计数：最后一条连接才移除用户）。
//
// 修复缺陷：c.send 的关闭权唯一属于本函数，且必须幂等 —— 被 KickUser /
// 慢消费者踢掉的连接也会走到这里（readPump 退出触发），若关闭两次会 panic。
// 因此统一用 sendOnce 保护；另在 Hub.Close 清空注册表后仍要关闭 send，
// 否则 writePump 无信号退出会泄漏 goroutine。
func (h *Hub) Unregister(c *Client) {
	s := h.shards[c.UserID%ShardCount]
	s.mu.Lock()
	if set, ok := s.conns[c.UserID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(s.conns, c.UserID)
		}
	}
	// 持写锁期间关闭：ToUsers 只在读锁内投递，二者互斥；且连接已从
	// 注册表摘除，不会再有新的投递落到这个已关闭的 channel 上。
	// IP 名额释放也放进 sendOnce：Unregister 幂等，重复调用不会把计数减两次。
	c.sendOnce.Do(func() {
		close(c.send)
		h.releaseIPSlot(c.ip)
	})
	s.mu.Unlock()
	// 断线（含被踢）结束该设备参与的通话并通知对端。必须在分片锁外调用：
	// endCallForDevice 内部会取其它分片锁做广播，锁内调用会死锁。
	h.endCallForDevice(c.UserID, c.sid, "disconnect")
}

// KickUser 强制断开某用户的全部既有连接（封禁 / 重置密码后 tv 已失效）。
//
// 修复缺陷：此前 admin 仅推 kicked 帧而不关连接，被封禁用户忽略该帧即可
// 继续发消息、写库与 fan-out；握手段校验只管新连接。本方法在分片锁内
// 「先入帧、后关连接」，且严格不 close(c.send)（所有权只属 Unregister）：
//   - trySend 非阻塞入帧，锁内绝不等待网络写 / channel；
//   - 锁外通过 kickCh 通知 writePump 冲刷队列、发 WS 关闭帧后自行退出；
//   - 另有 kickGrace 兜底定时器强制 Close，防止 writePump 已死时连接残留。
func (h *Hub) KickUser(userID uint, reason string) {
	if userID == 0 {
		return
	}
	h.kickUserLocal(userID, reason)
	// 广播给其它实例：远端只踢自己的同 uid 连接，不再次广播（靠 nodeID 过滤回声）。
	// Redis 不可用时 Publish 返回错误，本实例已处理，远端由 5s 连接级复核兜底。
	h.broadcast(cluster.Event{Kind: cluster.KindKickUser, UID: userID, Reason: reason})
}

// kickUserLocal 只踢本实例连接（供 KickUser 与跨实例事件回调复用）。
func (h *Hub) kickUserLocal(userID uint, reason string) {
	data, err := json.Marshal(ginH{"type": "kicked", "reason": reason})
	if err != nil {
		slog.Error("ws marshal kicked frame", "err", err.Error())
		return
	}
	s := h.shards[userID%ShardCount]
	s.mu.Lock()
	targets := make([]*Client, 0, len(s.conns[userID]))
	for c := range s.conns[userID] {
		c.trySend(data) // 非阻塞：慢消费者也不阻塞踢线流程
		targets = append(targets, c)
	}
	s.mu.Unlock()

	// 锁外关连接：避免在持有分片锁时做任何可能阻塞的操作。
	for _, c := range targets {
		c.requestKick(true) // 先冲刷队列，保证 kicked 帧送达
	}
	if len(targets) > 0 {
		slog.Info("ws kicked user conns", "uid", userID, "conns", len(targets))
	}
}

// KickSession 强制断开指定会话的全部连接（单设备下线）。
// 与 KickUser 的区别：只匹配 sid 相同的连接，该用户其他设备不受影响。
// sid 为空的老 token 不会命中（老登录态只能靠 tv 全局失效）。
func (h *Hub) KickSession(sid, reason string) {
	if sid == "" {
		return
	}
	h.kickSessionLocal(sid, reason)
	h.broadcast(cluster.Event{Kind: cluster.KindKickSession, SID: sid, Reason: reason})
}

// kickSessionLocal 只踢本实例中 sid 匹配的连接（供 KickSession 与跨实例回调复用）。
func (h *Hub) kickSessionLocal(sid, reason string) {
	data, err := json.Marshal(ginH{"type": "kicked", "reason": reason})
	if err != nil {
		slog.Error("ws marshal kicked frame", "err", err.Error())
		return
	}
	var targets []*Client
	for i := range h.shards {
		s := h.shards[i]
		s.mu.Lock()
		for _, set := range s.conns {
			for c := range set {
				if c.sid == sid {
					c.trySend(data)
					targets = append(targets, c)
				}
			}
		}
		s.mu.Unlock()
	}
	for _, c := range targets {
		c.requestKick(true)
	}
	if len(targets) > 0 {
		slog.Info("ws kicked session conns", "sid", sid, "conns", len(targets))
	}
}

// broadcast 广播踢线事件。未注入总线（单实例 / 测试）时为 no-op。
// 广播失败只告警：不影响本实例已执行的踢线，远端靠连接级复核兜底。
func (h *Hub) broadcast(ev cluster.Event) {
	if h.deps == nil || h.deps.Broadcaster == nil {
		return
	}
	if err := h.deps.Broadcaster.Publish(ev); err != nil {
		slog.Warn("cluster kick broadcast failed", "kind", ev.Kind, "err", err.Error())
	}
}

// requestKick 请求断开连接：flush=true 时通知 writePump 先冲刷队列
// （KickUser 需送达 kicked 帧），flush=false 时立即关闭（慢消费者）。
// 幂等：同一条连接被多次判定关闭也只触发一次。
func (c *Client) requestKick(flush bool) {
	c.kickOnce.Do(func() {
		if c.kickCh == nil {
			// 测试 / 非常规构造的连接：没有 writePump，直接关底层连接。
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
			return
		}
		// 必须先写 kickFlush 再 close kickCh：writePump 收到信号时读取该标记，
		// close(channel) 与 atomic 写之间由 channel 操作建立 happens-before。
		c.kickFlush.Store(flush)
		close(c.kickCh)
		// 兜底：writePump 若已退出或卡在网络写，kickGrace 后强制断开。
		time.AfterFunc(kickGrace, func() {
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
		})
	})
}

// Online 用户是否有在线连接。
func (h *Hub) Online(userID uint) bool {
	s := h.shards[userID%ShardCount]
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.conns[userID]) > 0
}

// ToUsers 向一组在线用户的全部连接推帧（序列化一次）。
//
// 修复缺陷：此前 send channel 满时走 default 分支静默丢帧 —— 客户端既收不到
// 该帧、也不会被断开，表现为「消息丢了但连接还活着」。现在改为：标记慢消费者
// pendingClose，把该连接收集到待关闭列表，**锁外**统一触发关闭。锁内只做
// 非阻塞投递与收集，绝不在分片锁内 close / 网络写 / 等待。
func (h *Hub) ToUsers(userIDs []uint, frame any) {
	data, err := json.Marshal(frame)
	if err != nil {
		slog.Error("ws marshal frame", "err", err.Error())
		return
	}
	// slow 收集跨分片发现的慢消费者：锁外关闭，避免在 RLock 内 close。
	var slow []*Client
	for _, uid := range userIDs {
		s := h.shards[uid%ShardCount]
		s.mu.RLock()
		for c := range s.conns[uid] {
			select {
			case c.send <- data:
			default:
				// 慢消费者：标记待关闭（CAS 保证只收集一次），锁内不做阻塞操作
				if c.pendingClose.CompareAndSwap(false, true) {
					slow = append(slow, c)
					slog.Warn("ws slow consumer, closing conn", "uid", c.UserID)
				}
			}
		}
		s.mu.RUnlock()
	}
	// 锁外关闭：RLock 已释放，requestKick 只 close 信号 channel + 设兜底定时器。
	for _, c := range slow {
		c.requestKick(false)
	}
}

// Close 关闭全部连接并退出 Run。
func (h *Hub) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	close(h.done)
	if h.clusterCancel != nil {
		h.clusterCancel()
	}
	h.mu.Unlock()
	// 锁内只收集，锁外关闭：避免持分片锁做网络操作。
	var all []*Client
	for i := range h.shards {
		s := h.shards[i]
		s.mu.Lock()
		for uid, set := range s.conns {
			for c := range set {
				all = append(all, c)
			}
			delete(s.conns, uid)
		}
		s.mu.Unlock()
	}
	for _, c := range all {
		// 只关底层连接：readPump 的 ReadMessage 随即出错退出，由它的 defer
		// 调用 Unregister 关闭 send（关闭权唯一）并唤醒 writePump。
		// 这里绝不能自行 close(c.send)：Dispatch 的 sendError 走 trySend 且
		// 不持分片锁，并发 close 会触发 send on closed channel panic。
		_ = c.Conn.Close()
	}
}

// ---- 帧发送小工具 ----

func (c *Client) trySend(data []byte) {
	select {
	case c.send <- data:
	default:
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ginH 帧字面量类型（避免本包依赖 gin）。
type ginH = map[string]any

// OnlineCount 当前在线用户数（同一用户多端只计一次）。
func (h *Hub) OnlineCount() int {
	n := 0
	for i := 0; i < ShardCount; i++ {
		s := h.shards[i]
		s.mu.RLock()
		n += len(s.conns)
		s.mu.RUnlock()
	}
	return n
}
