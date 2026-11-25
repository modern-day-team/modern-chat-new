// Package cluster 提供跨实例事件总线的中立数据类型与 Redis 实现。
//
// 本包不依赖 ws / handler 等上层包：事件结构自包含，ws 只依赖自己定义的
// 窄接口（Publish + Events），具体总线由组合根 main 装配。未配置 Redis 时
// 上层不注入总线，行为退化为纯单实例（与历史一致），组件仍可独立运行。
package cluster

// 事件类型：目前只承载踢线，后续 presence / 消息广播可按同一机制扩展。
const (
	KindKickUser    = "kick_user"
	KindKickSession = "kick_session"
)

// Event 跨实例事件。Node 为发布实例 ID，接收端据此跳过自己发布的事件，
// 避免「本机踢线 → 广播 → 本机再踢一次」的回声。
type Event struct {
	Node   string `json:"node"`
	Kind   string `json:"kind"`
	UID    uint   `json:"uid,omitempty"`
	SID    string `json:"sid,omitempty"`
	Reason string `json:"reason"`
}
