package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// kickChannel 跨实例踢线事件的 Redis 频道。所有实例订阅同一频道。
const kickChannel = "mc:ws:kick"

// Bus 跨实例事件总线：Publish 广播事件，Subscribe 持续接收本实例以外发布的事件。
// 实现基于 Redis Pub/Sub，连接不可用时 Publish 返回错误由调用方降级（fail-open：
// 本实例仍踢自己的连接，其它实例靠 5s 连接级复核兜底），Subscribe 自动重连。
type Bus struct {
	client  *redis.Client
	nodeID  string
	channel string
}

// NewBus 创建总线。nodeID 随机生成，用于过滤自己发布的事件；channel 为空时用默认频道。
func NewBus(client *redis.Client, channel string) *Bus {
	if channel == "" {
		channel = kickChannel
	}
	return &Bus{client: client, nodeID: randomNodeID(), channel: channel}
}

// NodeID 实例标识（供测试/日志）。
func (b *Bus) NodeID() string { return b.nodeID }

// Publish 向集群广播事件。Redis 不可用时返回错误，调用方应降级为仅本地处理。
func (b *Bus) Publish(ev Event) error {
	if b == nil || b.client == nil {
		return nil
	}
	ev.Node = b.nodeID
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return b.client.Publish(ctx, b.channel, payload).Err()
}

const (
	subscribeRetryMin = 200 * time.Millisecond
	subscribeRetryMax = 30 * time.Second
)

// Subscribe 订阅事件并在 ctx 取消时退出。收到自己发布的事件自动忽略，避免回声。
// 首次建连同步确认后再返回（调用方可据此判断订阅是否可用）；此后连接断开会
// 指数退避自动重连，避免 Redis 短暂抖动导致本实例永久失去跨实例踢线能力。
// onMessage 由本 goroutine 闭包捕获，多次调用 Subscribe 互不干扰。
func (b *Bus) Subscribe(ctx context.Context, onMessage func(Event)) error {
	if b == nil || b.client == nil {
		return nil
	}
	ready := make(chan error, 1)
	go b.subscribeLoop(ctx, onMessage, ready)
	// 等待首次订阅结果：成功返回 nil，失败返回错误（调用方告警后仍由循环重试）。
	return <-ready
}

// subscribeLoop 持有订阅生命周期：建连 → 消费 → 断线退避重连，直到 ctx 取消。
func (b *Bus) subscribeLoop(ctx context.Context, onMessage func(Event), ready chan<- error) {
	backoff := subscribeRetryMin
	first := true
	for {
		if ctx.Err() != nil {
			if first {
				ready <- ctx.Err()
			}
			return
		}
		pubsub := b.client.Subscribe(ctx, b.channel)
		// 等待订阅确认（SUBSCRIBE 的确认帧），避免订阅建立前发布的事件丢失。
		if _, err := pubsub.Receive(ctx); err != nil {
			_ = pubsub.Close()
			if first {
				ready <- err
				first = false
			}
			if !sleepBackoff(ctx, backoff) {
				return
			}
			if backoff < subscribeRetryMax {
				backoff *= 2
			}
			continue
		}
		if first {
			ready <- nil
			first = false
		}
		consumePubSub(ctx, pubsub, b.nodeID, onMessage)
		_ = pubsub.Close()
		// 连接中断或 ctx 取消：只有 ctx 仍在才退避重连。
		if !sleepBackoff(ctx, backoff) {
			return
		}
		if backoff < subscribeRetryMax {
			backoff *= 2
		}
	}
}

// consumePubSub 消费单个连接的消息；channel 关闭或 ctx 取消时返回。
func consumePubSub(ctx context.Context, pubsub *redis.PubSub, nodeID string, onMessage func(Event)) {
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				slog.Warn("cluster event malformed", "err", err.Error())
				continue
			}
			if ev.Node == nodeID {
				continue // 自己发布的事件，本地已处理
			}
			if onMessage != nil {
				onMessage(ev)
			}
		}
	}
}

// sleepBackoff 可取消的退避等待；ctx 已取消返回 false。
func sleepBackoff(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func randomNodeID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// 极端兜底：时间戳也足以区分实例（同一纳秒启动两实例概率可忽略）。
		return "t" + hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf)
}
