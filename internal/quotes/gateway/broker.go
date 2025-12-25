package gateway

import "context"

type Message struct {
	Topic   string
	Payload []byte
}

type Broker interface {
	// publish
	Publish(ctx context.Context, topic string, payload []byte) error
	// 订阅
	Subscribe(ctx context.Context, topics []string) (<-chan Message, error)
	// 关闭
	Close() error
}
