package ws

import (
	"log"
	"sync"
)

type Hub struct {
	mu   sync.RWMutex
	subs map[string]map[*Conn]struct{} // topic -> set(conn)
	last map[string][]byte             // topic -> last payload (snapshot)

}

func NewHub() *Hub {
	return &Hub{
		subs: make(map[string]map[*Conn]struct{}, 1024),
		last: make(map[string][]byte, 1024),
	}
}

func (h *Hub) Subscribe(c *Conn, topics []string) {
	log.Println("subscribe topics=", topics)

	// 1) 记录订阅
	h.mu.Lock()
	for _, t := range topics {
		set := h.subs[t]
		if set == nil {
			set = make(map[*Conn]struct{}, 16)
			h.subs[t] = set
		}
		set[c] = struct{}{}
	}
	// 2) 取快照（同一把锁里取，避免订阅后立刻 publish 却取不到）
	snaps := make([]struct {
		topic string
		data  []byte
	}, 0, len(topics))
	for _, t := range topics {
		if b := h.last[t]; b != nil {
			cp := make([]byte, len(b))
			copy(cp, b)
			snaps = append(snaps, struct {
				topic string
				data  []byte
			}{t, cp})
		}
	}
	h.mu.Unlock()

	// 3) 立即回放最新快照（首包延迟显著下降）
	for _, s := range snaps {
		_ = c.Offer(s.topic, s.data)
	}

}

func (h *Hub) Unsubscribe(c *Conn, topics []string) {
	h.mu.Lock()
	for _, t := range topics {
		if set := h.subs[t]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(h.subs, t)
			}
		}
	}
	h.mu.Unlock()
}

func (h *Hub) RemoveConn(c *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for topic, m := range h.subs {
		delete(m, c)
		if len(m) == 0 {
			delete(h.subs, topic)
		}
	}
}

// Publish：把 payload 广播给 topic 的所有订阅者。
// 关键：对每个 conn 都是非阻塞 Offer；慢客户端不会卡住广播。
func (h *Hub) Publish(topic string, payload []byte) {
	cp := make([]byte, len(payload))
	copy(cp, payload)

	h.mu.RLock()
	set := h.subs[topic]
	h.mu.RUnlock()

	h.mu.Lock()
	h.last[topic] = cp
	h.mu.Unlock()
	// 2) fanout：每连接 LatestOnly
	for c := range set {
		_ = c.Offer(topic, payload)
	}

}
