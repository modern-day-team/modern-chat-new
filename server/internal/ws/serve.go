package ws

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"

	"github.com/modern-day-team/modern-chat-new/server/internal/middleware"
	"github.com/modern-day-team/modern-chat-new/server/internal/model"
	"github.com/modern-day-team/modern-chat-new/server/internal/store"
)

const (
	writeWait  = 10 * time.Second // 单次写超时
	pongWait   = 60 * time.Second // 读超时（客户端 25s 心跳，服务端协议 ping 兜底）
	pingPeriod = 50 * time.Second
	sendBuffer = 256
	// maxMsgSize 上行帧上限（字节）。必须大于「后台可配的最大消息长度」的字节数：
	// chat.maxMessageLength 上限为 5000 个字符，UTF-8 下最多约 20 KB（4 字节/字符），
	// 再加上 clientMsgId、引用、mentions 等 JSON 开销。原来固定 4096 字节时，
	// 管理员把长度调到几千后，合法长消息会触发 gorilla 的 read limit 直接断连，
	// 用户连 error 帧都收不到。64 KB 留足余量，不做无上限放大。
	maxMsgSize = 64 * 1024
	// maxSilentLifetime 连接始终没有任何上行帧的最长存活时间。正常客户端
	// 25s 一次心跳必然刷新它；只有「只收不发」的连接会到期被断开并强制重新
	// 握手鉴权，避免撤销/封禁复核被完全绕过。
	maxSilentLifetime = 10 * time.Minute
)

// errMsgTooBig 解压后超过 maxMsgSize 的上行帧（readFrame 返回后直接断开连接）。
var errMsgTooBig = errors.New("ws: 上行帧超过大小上限")

// firstNonEmpty 返回第一个非空串（query 优先于请求头）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// quotaIP 返回用于连接配额的握手来源地址；回环地址视为同机反向代理返回空串。
// Serve 只拿得到 http.Request，拿不到 gin 的 ClientIP（TRUSTED_PROXIES 解析在
// router 层），若把反代的回环地址当来源，所有经 nginx 的用户会被合并成一个
// IP 而互相挤占配额；跨机反代场景应在反代侧做按 IP 限流。
func quotaIP(remoteAddr string) string {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	if host == "" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return host
}

// upgraderFor 构造带 Origin 白名单的升级器：
// 浏览器 WS 升级必带 Origin，非白名单来源拒绝（防跨站 WebSocket 劫持）；
// 无 Origin（curl / 服务端客户端）放行，仍受 token 鉴权保护。
func upgraderFor(allowed []string) websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		// permessage-deflate（设计文档 §9.2）：浏览器默认请求该扩展，
		// 开启后文本帧自动压缩；连接数压顶时可置 false 换取 CPU。
		// 注意：开启后上行帧可能是压缩的，gorilla 的 SetReadLimit 只统计线路字节，
		// 解压后的实际大小由 readFrame 另行限制（见 serve.go 底部）。
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // 原生客户端无 Origin，仍受 token 鉴权保护
			}
			// 与 REST CORS 共用同一 Origin 策略，口径一致（归一化 + 精确白名单）
			if middleware.OriginAllowed(allowed, origin) {
				return true
			}
			slog.Warn("ws origin rejected", "origin", origin, "remote", r.RemoteAddr)
			return false
		},
	}
}

// Serve 握手 + 鉴权 + 启动读写 pump。
func Serve(
	secret string,
	allowedOrigins []string,
	users *store.UserStore,
	hub *Hub,
	settings *store.SettingStore,
	w http.ResponseWriter,
	r *http.Request,
	sessions ...*store.SessionStore,
) {
	var sessionStore *store.SessionStore
	if len(sessions) > 0 {
		sessionStore = sessions[0]
	}
	// 会话表回填给 Hub：连接级复核（dispatch.go 的 loadConnState）与定时巡检
	// 需要按 sid 查撤销状态。Hub 在 main 中先于 router 构造，注入点只能在这里。
	hub.setSessions(sessionStore)
	// 握手频率限制放在鉴权之前：每次握手都会查库，宁可让伪造 token 的洪水
	// 在解析/查库之前就被挡掉。
	// 连接并发配额需要 uid，放在鉴权之后（见下）。
	ip := quotaIP(r.RemoteAddr)
	if !hub.allowHandshake(ip) {
		slog.Warn("ws handshake rate limited", "remote", r.RemoteAddr)
		http.Error(w, `{"error":"操作过于频繁，请稍后再试"}`, http.StatusTooManyRequests)
		return
	}
	// token 优先取 query（兼容现有前端），没有时回退到登录下发的同源 cookie。
	// 走 cookie 可避免 token 出现在反向代理 / 访问日志的 URL 里；
	// query 方式继续保留，不改变现有握手契约。
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(middleware.AuthCookieName); err == nil {
			token = ck.Value
		}
	}
	if token == "" {
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	cl, err := middleware.ParseToken(token, secret)
	if err != nil {
		slog.Warn("ws handshake rejected: bad token", "remote", r.RemoteAddr)
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	u, err := users.ByID(cl.UID)
	if err != nil || u == nil || u.TokenVersion != cl.TV {
		slog.Warn("ws handshake rejected: stale token", "uid", cl.UID, "remote", r.RemoteAddr)
		http.Error(w, `{"error":"登录已失效，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	if u.IsBanned() {
		slog.Warn("ws handshake rejected: banned", "uid", u.ID, "remote", r.RemoteAddr)
		http.Error(w, `{"error":"账号已被封禁"}`, http.StatusForbidden)
		return
	}
	// 会话校验与 lazy 登记：与 REST Auth 同一套口径。
	// 二维码登录常见的顺序是「先拿到 token、再带 platform 参数握手」，
	// 因此客户端标识同时接受 query 与请求头。
	sid := cl.SID
	// 与 REST Auth 同口径：启用会话表后无 sid 的老 token 一律拒绝，
	// 否则单设备撤销（只改 revoked_at）踢不到该连接。
	if sessionStore != nil && sid == "" {
		http.Error(w, `{"error":"登录态需要更新，请重新登录"}`, http.StatusUnauthorized)
		return
	}
	if sessionStore != nil {
		q := r.URL.Query()
		platform := middleware.ClientPlatform(firstNonEmpty(q.Get("platform"), r.Header.Get("X-Client-Platform")))
		clientType := middleware.SanitizeHeader(firstNonEmpty(q.Get("clientType"), r.Header.Get("X-Client-Type")))
		appVersion := middleware.SanitizeHeader(firstNonEmpty(q.Get("appVersion"), r.Header.Get("X-Client-Version")))
		// 与 REST Auth 同口径：先 lazy 登记，再校验未撤销状态。
		// 新登录 token 首次 WS 握手不应被会话表空行误判为"已退出登录"。
		if _, err := sessionStore.Touch(u.ID, sid, platform, clientType, appVersion, r.RemoteAddr); err != nil {
			slog.Warn("ws touch session failed", "err", err.Error())
		}
		sess, err := sessionStore.ActiveBySID(sid)
		if err != nil {
			slog.Error("ws load session", "err", err.Error())
			http.Error(w, `{"error":"服务暂时不可用"}`, http.StatusServiceUnavailable)
			return
		}
		if sess == nil {
			http.Error(w, `{"error":"该设备已退出登录，请重新登录"}`, http.StatusUnauthorized)
			return
		}
	}
	// 维护模式下非管理员不许建连：REST 已经被中间件挡住，
	// 这里不挡的话页面虽然显示维护页，已有连接仍能继续收发消息
	if settings != nil && settings.Ops().MaintenanceMode && u.Role != "admin" {
		http.Error(w, `{"error":"站点维护中","maintenance":true}`, http.StatusServiceUnavailable)
		return
	}

	// 连接准入：按账号与来源 IP 限制并发连接数。
	// 此前同一账号可无限建连，认证用户即可用 goroutine / 发送积压把内存打满。
	if n := hub.ConnCount(u.ID); n >= maxConnsPerUser {
		slog.Warn("ws conn limit per user", "uid", u.ID, "conns", n)
		http.Error(w, `{"error":"连接数过多，请稍后再试"}`, http.StatusServiceUnavailable)
		return
	}
	if !hub.acquireIPSlot(ip) {
		slog.Warn("ws conn limit per ip", "remote", r.RemoteAddr)
		http.Error(w, `{"error":"连接数过多，请稍后再试"}`, http.StatusServiceUnavailable)
		return
	}

	// cookie 回授：已登录的旧客户端（localStorage 有 token、浏览器还没有 mc_auth）
	// 在重连时即可拿到 cookie，此后 <img>/<video> 加载 /uploads 不再 401。
	middleware.SetAuthCookie(w, r, token)

	up := upgraderFor(allowedOrigins)
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		hub.releaseIPSlot(ip) // 升级失败：名额必须归还，否则配额会被慢慢耗尽
		return
	}
	client := &Client{
		UserID: u.ID,
		tv:     cl.TV, // 缓存握手时的 token_version，供 handleChat 连接级复核
		sid:    sid,   // 会话标识，供 KickSession 单设备踢线
		ip:     ip,
		Conn:   conn,
		send:   make(chan []byte, sendBuffer),
		kickCh: make(chan struct{}),
		hub:    hub,
		// 非聊天上行帧限流桶（F2）：20 帧/秒、突发 40，见 dispatch.go 阈值说明
		ctrl: rate.NewLimiter(ctrlFrameRate, ctrlFrameBurst),
	}
	client.lastInbound.Store(time.Now().UnixNano())
	hub.Register(client)
	// 握手校验（上方）与本次注册之间没有互斥：KickSession / KickUser 可能恰好
	// 在两段之间完成、扫不到这条尚未注册的连接。注册后立即跑一次连接级复核，
	// 把竞态窗口收敛为「要么注册前已复核通过、要么复核时已撤销」——后者直接
	// 拒绝该连接，不再启动读写循环与 presence 广播。
	if !hub.checkConnState(client, 0) {
		hub.Unregister(client)
		_ = conn.Close()
		return
	}
	slog.Info("ws connected", "uid", u.ID)

	// 上线 presence（好友 + 同会话成员）
	go hub.notifyPresence(u, true)

	go writePump(client)
	readPump(client)
}

// writePump 单连接写循环：send channel + 协议层 ping + 踢线信号；写满即踢（慢消费者）。
//
// 修复缺陷：收到 kickCh 信号（KickUser 下发 kicked 帧后触发）时，先把
// send 队列里残留的帧冲刷掉（保证 kicked 帧送达），再发 WS CloseMessage
// 并退出；c.send 的关闭仍由 readPump defer 里的 Unregister 独占。
func writePump(c *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()
	for {
		select {
		case data, ok := <-c.send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			w, err := c.Conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			if err := w.Close(); err != nil {
				return
			}
		case <-c.kickCh:
			// KickUser 场景（kickFlush=true）先冲刷队列，保证 kicked 帧送达；
			// 慢消费者场景（kickFlush=false）直接退出，避免为已判定超载的连接
			// 继续占用写带宽。
			if c.kickFlush.Load() {
				for i := len(c.send); i > 0; i-- {
					data, ok := <-c.send
					if !ok {
						break
					}
					_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
					if err := c.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
						break
					}
				}
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			_ = c.Conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "kicked"))
			return
		case <-ticker.C:
			// 只收不发的连接不会经过 Dispatch，连接级复核也就永远不会触发；
			// 静默超过 maxSilentLifetime 主动断开，迫使客户端重连重新鉴权。
			if time.Since(time.Unix(0, c.lastInbound.Load())) > maxSilentLifetime {
				slog.Info("ws silent conn closed for reauth", "uid", c.UserID)
				_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = c.Conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "reauth required"))
				return
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// readPump 单连接读循环：每帧刷新读超时，帧交给 Hub 分发。
func readPump(c *Client) {
	defer func() {
		// 修复缺陷：先把连接从注册表摘除，再判断是否还有其它连接在线。
		// 原实现无条件广播 offline，导致「手机 + 网页」多端登录时，任一端
		// 断开就让好友看到该用户离线（误报）；现在仅当该用户已无任何在线
		// 连接时才广播 offline，单端在线期间保持 online 状态。
		c.hub.Unregister(c)
		if !c.hub.Online(c.UserID) {
			go c.hub.notifyPresence(&model.User{ID: c.UserID}, false)
		} else {
			slog.Debug("ws conn closed, user still online", "uid", c.UserID)
		}
		_ = c.Conn.Close()
		slog.Info("ws disconnected", "uid", c.UserID)
	}()
	// 线路字节上限：只挡未压缩的超长帧；压缩帧的解压后体积由 readFrame 限制。
	c.Conn.SetReadLimit(maxMsgSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		return c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		data, err := readFrame(c.Conn)
		if err != nil {
			if errors.Is(err, errMsgTooBig) {
				slog.Warn("ws frame rejected: decompressed payload > limit", "uid", c.UserID)
			}
			return
		}
		c.lastInbound.Store(time.Now().UnixNano())
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		c.hub.Dispatch(c, data)
	}
}

// readFrame 读取一条完整上行消息，并按「解压后」的字节数执行上限。
//
// 修复缺陷：gorilla 的 SetReadLimit 只累计压缩后的线路字节（conn.go 的
// advanceFrame 把 readRemaining 累加进 readLength），permessage-deflate 下
// 一条 64KB 线路帧可解压出数十 MB，而 ReadMessage 直接 io.ReadAll(解压流)，
// 限流触发前内存已经分配完毕。这里改用 NextReader 拿到（可能的）解压流后
// 只消费 maxMsgSize+1 字节，超出即判超限并断开，内存上界固定为 maxMsgSize；
// SetReadLimit 仍保留，作为压缩前线路字节的廉价第一道闸。
func readFrame(conn *websocket.Conn) ([]byte, error) {
	_, r, err := conn.NextReader()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(&io.LimitedReader{R: r, N: maxMsgSize + 1})
	if err != nil {
		return nil, err
	}
	if len(data) > maxMsgSize {
		return nil, errMsgTooBig
	}
	return data, nil
}
