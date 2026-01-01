package main

import (
	"bytes"
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
Demo 04 - Batch write + buffer pooling (only)

Goal:
- Reduce syscalls/frame overhead by sending multiple messages in one write
- Reuse bytes.Buffer via sync.Pool
- Use gorilla Upgrader.WriteBufferPool to reduce retained per-conn write buffers
*/

type ClientMsg struct {
	Type   string   `json:"type"`
	Topics []string `json:"topics"`
}

type TopicRegistry struct {
	mu     sync.RWMutex
	byName map[string]uint32
}

func NewTopicRegistry() *TopicRegistry {
	return &TopicRegistry{byName: make(map[string]uint32, 256)}
}

func (r *TopicRegistry) ID(topic string) uint32 {
	r.mu.RLock()
	if id, ok := r.byName[topic]; ok {
		r.mu.RUnlock()
		return id
	}
	r.mu.RUnlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.byName[topic]; ok {
		return id
	}
	id := uint32(len(r.byName) + 1)
	r.byName[topic] = id
	return id
}

type Hub struct {
	reg    *TopicRegistry
	shards []hubShard
}

type hubShard struct {
	mu   sync.RWMutex
	subs map[uint32]map[*Conn]struct{}
	last map[uint32][]byte
}

func NewHub(reg *TopicRegistry, shards int) *Hub {
	if shards <= 0 {
		shards = 8
	}
	h := &Hub{reg: reg, shards: make([]hubShard, shards)}
	for i := range h.shards {
		h.shards[i].subs = make(map[uint32]map[*Conn]struct{}, 256)
		h.shards[i].last = make(map[uint32][]byte, 256)
	}
	return h
}

func (h *Hub) shard(id uint32) *hubShard { return &h.shards[int(id)%len(h.shards)] }

func (h *Hub) Subscribe(c *Conn, topics []string) {
	for _, t := range topics {
		id := h.reg.ID(t)
		sh := h.shard(id)

		sh.mu.Lock()
		set := sh.subs[id]
		if set == nil {
			set = make(map[*Conn]struct{}, 16)
			sh.subs[id] = set
		}
		set[c] = struct{}{}
		snap := sh.last[id]
		sh.mu.Unlock()

		if len(snap) > 0 {
			select {
			case c.send <- snap:
			default:
			}
		}
	}
}

func (h *Hub) Publish(topic string, payload []byte) {
	id := h.reg.ID(topic)
	sh := h.shard(id)

	sh.mu.Lock()
	sh.last[id] = payload
	set := sh.subs[id]
	conns := make([]*Conn, 0, len(set))
	for c := range set {
		conns = append(conns, c)
	}
	sh.mu.Unlock()

	for _, c := range conns {
		select {
		case c.send <- payload:
		default:
		}
	}
}

func (h *Hub) RemoveConn(c *Conn) {
	for i := range h.shards {
		sh := &h.shards[i]
		sh.mu.Lock()
		for id, set := range sh.subs {
			delete(set, c)
			if len(set) == 0 {
				delete(sh.subs, id)
				delete(sh.last, id)
			}
		}
		sh.mu.Unlock()
	}
}

type Conn struct {
	ws   *websocket.Conn
	hub  *Hub
	send chan []byte
}

const maxBatch = 128

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// gorilla write buffer pool
var wsWriteBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 16*1024)
		return &b
	},
}

type Server struct {
	hub      *Hub
	upgrader websocket.Upgrader
}

func NewServer(hub *Hub) *Server {
	return &Server{
		hub: hub,
		upgrader: websocket.Upgrader{
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			WriteBufferPool:  &wsWriteBufPool,
			CheckOrigin:      func(r *http.Request) bool { return true },
			EnableCompression: false,
		},
	}
}

func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade err: %v", err)
		return
	}
	c := &Conn{ws: ws, hub: s.hub, send: make(chan []byte, 256)}
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
		if msg.Type == "sub" {
			c.hub.Subscribe(c, msg.Topics)
		}
	}
}

func (s *Server) writePump(c *Conn) {
	defer func() { _ = c.ws.Close() }()
	for {
		p, ok := <-c.send
		if !ok {
			return
		}

		buf := bufPool.Get().(*bytes.Buffer)
		buf.Reset()
		_, _ = buf.Write(p)

		for i := 1; i < maxBatch; i++ {
			select {
			case p2 := <-c.send:
				_ = buf.WriteByte('\n')
				_, _ = buf.Write(p2)
			default:
				i = maxBatch
			}
		}

		_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err := c.ws.WriteMessage(websocket.TextMessage, buf.Bytes())

		buf.Reset()
		bufPool.Put(buf)

		if err != nil {
			return
		}
	}
}

func main() {
	ctx := context.Background()
	reg := NewTopicRegistry()
	hub := NewHub(reg, 8)
	srv := NewServer(hub)

	http.HandleFunc("/ws", srv.ServeWS)
	go runProducer(ctx, hub)

	log.Println("demo04 listening :8080")
	log.Println(`sub: {"type":"sub","topics":["kline:1s:BTC-USD"]}`)
	if err := http.ListenAndServe(":8080", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runProducer(ctx context.Context, hub *Hub) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			ev := struct {
				Symbol  string `json:"symbol"`
				TF      string `json:"tf"`
				StartMs int64  `json:"start_ms"`
				EndMs   int64  `json:"end_ms"`
				C       int64  `json:"c"`
			}{
				Symbol:  "BTC-USD",
				TF:      "1s",
				StartMs: now.Add(-time.Second).UnixMilli(),
				EndMs:   now.UnixMilli(),
				C:       100_000_000 + int64(r.Intn(10_000)),
			}
			b, _ := json.Marshal(ev)
			hub.Publish(fmt.Sprintf("kline:%s:%s", "1s", "BTC-USD"), b)
		}
	}
}
