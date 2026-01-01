package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

/*
Demo 01 - Sharded Hub (only)

Goal: reduce lock contention in "topic -> subscribers" by sharding.
Still uses:
- topic string keys
- per-conn send channel (no conflation)
- encode per publish (json.Marshal for each publish call)
*/

type ClientMsg struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

type Hub struct {
	shards []hubShard
}

type hubShard struct {
	mu   sync.RWMutex
	subs map[string]map[*Conn]struct{} // topic -> set(conn)
}

func NewHub(shards int) *Hub {
	if shards <= 0 {
		shards = 8
	}
	h := &Hub{shards: make([]hubShard, shards)}
	for i := range h.shards {
		h.shards[i].subs = make(map[string]map[*Conn]struct{}, 256)
	}
	return h
}

func (h *Hub) shard(topic string) *hubShard {
	var x uint32
	for i := 0; i < len(topic); i++ {
		x = x*131 + uint32(topic[i])
	}
	return &h.shards[int(x)%len(h.shards)]
}

func (h *Hub) Subscribe(c *Conn, topics []string) {
	for _, t := range topics {
		sh := h.shard(t)
		sh.mu.Lock()
		set := sh.subs[t]
		if set == nil {
			set = make(map[*Conn]struct{}, 16)
			sh.subs[t] = set
		}
		set[c] = struct{}{}
		sh.mu.Unlock()
	}
}

func (h *Hub) Unsubscribe(c *Conn, topics []string) {
	for _, t := range topics {
		sh := h.shard(t)
		sh.mu.Lock()
		if set := sh.subs[t]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(sh.subs, t)
			}
		}
		sh.mu.Unlock()
	}
}

func (h *Hub) RemoveConn(c *Conn) {
	for i := range h.shards {
		sh := &h.shards[i]
		sh.mu.Lock()
		for topic, set := range sh.subs {
			delete(set, c)
			if len(set) == 0 {
				delete(sh.subs, topic)
			}
		}
		sh.mu.Unlock()
	}
}

func (h *Hub) Publish(topic string, payload []byte) {
	sh := h.shard(topic)
	sh.mu.RLock()
	set := sh.subs[topic]
	if len(set) == 0 {
		sh.mu.RUnlock()
		return
	}
	conns := make([]*Conn, 0, len(set))
	for c := range set {
		conns = append(conns, c)
	}
	sh.mu.RUnlock()

	for _, c := range conns {
		select {
		case c.send <- payload:
		default:
			// drop when full (still no "latest-only")
		}
	}
}

type Conn struct {
	ws   *websocket.Conn
	hub  *Hub
	send chan []byte
}

type Server struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

func NewServer(hub *Hub) *Server {
	return &Server{
		hub: hub,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade err: %v", err)
		return
	}
	c := &Conn{ws: ws, hub: s.hub, send: make(chan []byte, 64)}
	go s.readPump(c)
	go s.writePump(c)
}

func (s *Server) readPump(c *Conn) {
	defer func() {
		c.hub.RemoveConn(c)
		_ = c.ws.Close()
	}()
	for {
		_, b, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var msg ClientMsg
		if json.Unmarshal(b, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "sub":
			c.hub.Subscribe(c, msg.Topics)
		case "unsub":
			c.hub.Unsubscribe(c, msg.Topics)
		}
	}
}

func (s *Server) writePump(c *Conn) {
	defer func() { _ = c.ws.Close() }()
	for p := range c.send {
		_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.ws.WriteMessage(websocket.TextMessage, p); err != nil {
			return
		}
	}
}

func main() {
	ctx := context.Background()
	hub := NewHub(8)
	srv := NewServer(hub)

	http.HandleFunc("/ws", srv.ServeWS)
	go runProducer(ctx, hub)

	log.Println("demo01 listening :8080")
	log.Println(`sub: {"type":"sub","topics":["kline:1s:BTC-USD"]}`)
	if err := http.ListenAndServe(":8080", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runProducer(ctx context.Context, hub *Hub) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	t := time.NewTicker(200 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			ev := map[string]any{
				"symbol":   "BTC-USD",
				"tf":       "1s",
				"start_ms": now.Add(-time.Second).UnixMilli(),
				"end_ms":   now.UnixMilli(),
				"c":        100_000_000 + r.Intn(10_000),
			}
			b, _ := json.Marshal(ev)
			hub.Publish(fmt.Sprintf("kline:%s:%s", "1s", "BTC-USD"), b)
		}
	}
}
