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
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

/*
Demo 05 - Slow client backpressure (only)

Goal: prove and handle "slow consumer":
- Latest-only mailbox per topicID (conflation) to avoid unbounded queues
- Bounded pending bytes per conn: > threshold => kick
- Ping/pong and write-lag monitoring
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
		c.addSub(id)
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
			_ = c.Enqueue(id, snap)
		}
	}
}

func (h *Hub) RemoveConn(c *Conn) {
	ids := c.takeSubsSnapshot()
	for _, id := range ids {
		sh := h.shard(id)
		sh.mu.Lock()
		if set := sh.subs[id]; set != nil {
			delete(set, c)
			if len(set) == 0 {
				delete(sh.subs, id)
			}
		}
		sh.mu.Unlock()
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
		_ = c.Enqueue(id, payload)
	}
}

// ---- Conn latest-only mailbox ----

const (
	writeWait       = 5 * time.Second
	pingPeriod      = 30 * time.Second
	pongWait        = 40 * time.Second
	maxWriteLag     = 10 * time.Second
	maxPendingBytes = 256 * 1024
	maxFlush        = 256
)

var nextConnID atomic.Uint64

var bufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}
var wsWriteBufPool = sync.Pool{New: func() any { b := make([]byte, 0, 16*1024); return &b }}

type Conn struct {
	id  uint64
	ws  *websocket.Conn
	hub *Hub

	closeCh chan struct{}
	closed  atomic.Bool

	notify chan struct{} // buffered-1 coalescing

	mu       sync.Mutex
	latest   map[uint32][]byte
	dirty    []uint32
	dirtySet map[uint32]struct{}
	pending  int64

	subMu sync.Mutex
	subs  map[uint32]struct{}

	lastWrite atomic.Int64
	lastPong  atomic.Int64
}

func NewConn(ws *websocket.Conn, hub *Hub) *Conn {
	now := time.Now().UnixNano()
	c := &Conn{
		id:       nextConnID.Add(1),
		ws:       ws,
		hub:      hub,
		closeCh:  make(chan struct{}),
		notify:   make(chan struct{}, 1),
		latest:   make(map[uint32][]byte, 64),
		dirty:    make([]uint32, 0, 128),
		dirtySet: make(map[uint32]struct{}, 128),
		subs:     make(map[uint32]struct{}, 16),
	}
	c.lastWrite.Store(now)
	c.lastPong.Store(now)
	return c
}

func (c *Conn) addSub(id uint32) {
	c.subMu.Lock()
	c.subs[id] = struct{}{}
	c.subMu.Unlock()
}

func (c *Conn) takeSubsSnapshot() []uint32 {
	c.subMu.Lock()
	defer c.subMu.Unlock()
	out := make([]uint32, 0, len(c.subs))
	for id := range c.subs {
		out = append(out, id)
	}
	c.subs = make(map[uint32]struct{}, 0)
	return out
}

func (c *Conn) Close(reason string) {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	close(c.closeCh)
	_ = c.ws.Close()
	c.hub.RemoveConn(c)
	log.Printf("conn closed id=%d reason=%s", c.id, reason)
}

func (c *Conn) Enqueue(id uint32, payload []byte) bool {
	if c.closed.Load() {
		return false
	}

	c.mu.Lock()
	old := c.latest[id]
	c.latest[id] = payload

	if _, ok := c.dirtySet[id]; !ok {
		c.dirtySet[id] = struct{}{}
		c.dirty = append(c.dirty, id)
	}

	delta := int64(len(payload))
	if old != nil {
		delta -= int64(len(old))
	}
	c.pending += delta
	p := c.pending
	c.mu.Unlock()

	if p > maxPendingBytes {
		c.Close("pending_bytes_limit")
		return false
	}

	select {
	case c.notify <- struct{}{}:
	default:
	}
	return true
}

func (c *Conn) flush(max int) (batch [][]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for len(c.dirty) > 0 && len(batch) < max {
		id := c.dirty[0]
		c.dirty = c.dirty[1:]
		delete(c.dirtySet, id)

		p := c.latest[id]
		delete(c.latest, id)
		if p == nil {
			continue
		}
		batch = append(batch, p)
		c.pending -= int64(len(p))
	}
	return batch
}

// ---- Server ----

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
			WriteBufferPool: &wsWriteBufPool,
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
	c := NewConn(ws, s.hub)
	go s.readPump(c)
	go s.writePump(c)
}

func (s *Server) readPump(c *Conn) {
	defer c.Close("read_exit")

	_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error {
		c.lastPong.Store(time.Now().UnixNano())
		_ = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

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
			s.hub.Subscribe(c, msg.Topics)
		}
	}
}

func (s *Server) writePump(c *Conn) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Close("write_exit")
	}()

	for {
		select {
		case <-c.closeCh:
			return

		case <-c.notify:
			batch := c.flush(maxFlush)
			if len(batch) == 0 {
				continue
			}

			buf := bufPool.Get().(*bytes.Buffer)
			buf.Reset()
			for i, p := range batch {
				if i > 0 {
					_ = buf.WriteByte('\n')
				}
				_, _ = buf.Write(p)
			}

			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			err := c.ws.WriteMessage(websocket.TextMessage, buf.Bytes())

			buf.Reset()
			bufPool.Put(buf)

			c.lastWrite.Store(time.Now().UnixNano())

			if err != nil {
				c.Close("write_err")
				return
			}

		case <-ticker.C:
			now := time.Now()
			if now.Sub(time.Unix(0, c.lastPong.Load())) > pongWait {
				c.Close("pong_timeout")
				return
			}
			if now.Sub(time.Unix(0, c.lastWrite.Load())) > maxWriteLag {
				c.Close("write_lag")
				return
			}

			_ = c.ws.SetWriteDeadline(now.Add(writeWait))
			if err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), now.Add(writeWait)); err != nil {
				c.Close("ping_err")
				return
			}
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

	log.Println("demo05 listening :8080")
	log.Println(`sub: {"type":"sub","topics":["kline:1s:BTC-USD"]}`)
	if err := http.ListenAndServe(":8080", nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func runProducer(ctx context.Context, hub *Hub) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	t := time.NewTicker(20 * time.Millisecond)
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
			b, _ := json.Marshal(ev) // encode once
			hub.Publish(fmt.Sprintf("kline:%s:%s", "1s", "BTC-USD"), b)
		}
	}
}
