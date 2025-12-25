// internal/quotes/gateway/gateway.go
package gateway

import (
	"context"
	"log"

	"gopherex.com/internal/quotes/ws"
)

type Gateway struct {
	hub    *ws.Hub
	server *ws.Server
	broker Broker
}

func NewGateway(hub *ws.Hub, server *ws.Server, broker Broker) *Gateway {
	return &Gateway{hub: hub, server: server, broker: broker}
}

// Run：订阅 broker 消息 -> Bridge 到本地 hub
func (g *Gateway) Run(ctx context.Context, topics []string) error {
	ch, err := g.broker.Subscribe(ctx, topics)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m, ok := <-ch:
			if !ok {
				return nil
			}
			// 关键点：payload 你可以直接当成 ws.Bridge 的输入（如果 Bridge 接收的是 topic+[]byte）
			// 若你现在 Bridge 接收的是“事件结构”，就把这里改成解包/再编码。
			ws.BridgeRaw(g.hub, m.Topic, m.Payload) // 你实现一个 BridgeRaw：直接发布 payload
		}
	}
}

// Publish：生产者（agg）调用它，把消息发到 broker（单机=内存，多机=跨节点）
func (g *Gateway) Publish(ctx context.Context, topic string, payload []byte) {
	if err := g.broker.Publish(ctx, topic, payload); err != nil {
		log.Printf("broker publish err: %v", err)
	}
}
