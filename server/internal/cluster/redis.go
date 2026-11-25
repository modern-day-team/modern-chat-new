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
	client    *redis.Client
	nodeID    string
	channel   string
	onMessage func(Event)
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

// Subscribe 订阅事件并在 ctx 取消时退出。收到自己发布的事件自动忽略，避免回声。
// Redis 连接中断时 go-redis 的 PubSub 会自动重连并恢复订阅。
func (b *Bus) Subscribe(ctx context.Context, onMessage func(Event)) error {
	if b == nil || b.client == nil {
		return nil
	}
	b.onMessage = onMessage
	pubsub := b.client.Subscribe(ctx, b.channel)
	// 等待订阅真正建立再返回，避免启动瞬间发布的首条事件丢失。
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return err
	}
	go func() {
		defer pubsub.Close()
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
				if ev.Node == b.nodeID {
					continue // 自己发布的事件，本地已处理
				}
				if b.onMessage != nil {
					b.onMessage(ev)
				}
			}
		}
	}()
	return nil
}

func randomNodeID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// 极端兜底：时间戳也足以区分实例（同一纳秒启动两实例概率可忽略）。
		return "t" + hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf)
}
