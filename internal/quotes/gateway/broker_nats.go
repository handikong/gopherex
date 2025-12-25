// internal/quotes/gateway/broker_nats.go
package gateway

import (
	"context"
	"strings"

	"github.com/nats-io/nats.go"
)

type NatsBroker struct {
	nc *nats.Conn
}

func NewNatsBroker(url string, opts ...nats.Option) (*NatsBroker, error) {
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, err
	}
	return &NatsBroker{nc: nc}, nil
}

func (b *NatsBroker) Publish(ctx context.Context, topic string, payload []byte) error {
	subj := topicToSubject(topic)
	return b.nc.Publish(subj, payload)
}

func (b *NatsBroker) Subscribe(ctx context.Context, subjects []string) (<-chan Message, error) {
	out := make(chan Message, 8192)

	// 保存订阅，退出时取消
	subs := make([]*nats.Subscription, 0, len(subjects))

	for _, s := range subjects {
		subj := topicToSubject(s) // 允许传入带通配符的 topic 形式
		sub, err := b.nc.Subscribe(subj, func(m *nats.Msg) {
			msg := Message{
				Topic:   subjectToTopic(m.Subject),
				Payload: m.Data,
			}
			// at-most-once：慢消费者直接丢，避免把 NATS 回调卡死
			select {
			case out <- msg:
			default:
			}
		})
		if err != nil {
			for _, ss := range subs {
				_ = ss.Unsubscribe()
			}
			return nil, err
		}
		subs = append(subs, sub)
	}

	// 监听 ctx.Done 清理
	go func() {
		<-ctx.Done()
		for _, s := range subs {
			_ = s.Unsubscribe()
		}
		close(out)
	}()

	return out, nil
}

func (b *NatsBroker) Close() error {
	if b.nc != nil {
		b.nc.Drain()
		b.nc.Close()
	}
	return nil
}

func topicToSubject(topic string) string { return strings.ReplaceAll(topic, ":", ".") }
func subjectToTopic(subj string) string  { return strings.ReplaceAll(subj, ".", ":") }
