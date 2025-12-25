package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Conn struct {
	id string

	ws     *websocket.Conn
	hub    *Hub
	send   chan []byte
	mu     sync.Mutex
	latest map[string][]byte // LatestOnly：topic -> last payload
	notify chan struct{}     // 缓冲 1：合并唤醒
	closed atomic.Bool

	lastPongUnix  atomic.Int64 // time.Now().UnixNano()
	lastWriteUnix atomic.Int64
	pendingBytes  atomic.Int64 // latest 中累计字节（粗略即可）

}

func NewConn(h *Hub, ws *websocket.Conn) *Conn {
	return &Conn{
		ws:     ws,
		hub:    h,
		latest: make(map[string][]byte, 64),
		notify: make(chan struct{}, 1),
	}
}

func (c *Conn) Offer(topic string, payload []byte) bool {
	//select {
	//case c.send <- b:
	//default:
	//	// send 队列满：直接丢（慢客户端自己承担）
	//}

	if c.closed.Load() {
		return false
	}
	cp := make([]byte, len(payload))
	copy(cp, payload)

	c.mu.Lock()
	c.latest[topic] = cp
	c.mu.Unlock()

	select {
	case c.notify <- struct{}{}:
	default:
	}
	return true

}

func (c *Conn) flushLatest(max int) [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.latest) == 0 {
		return nil
	}
	out := make([][]byte, 0, min(len(c.latest), max))
	i := 0
	for k, v := range c.latest {
		out = append(out, v)
		delete(c.latest, k)
		i++
		if i >= max {
			break
		}
	}
	return out
}

type Server struct {
	Hub      *Hub
	Upgrader websocket.Upgrader
	ctx      context.Context
	SendBuf  int // per-conn send chan size
	// 超时参数（v0 给默认值）
	PongWait   time.Duration
	PingPeriod time.Duration
	PingJitter time.Duration // e.g. 3s
	WriteWait  time.Duration
	ReadLimit  int64
}

func NewServer(ctx context.Context, h *Hub) *Server {
	return &Server{
		Hub: h,
		ctx: ctx,
		Upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     func(r *http.Request) bool { return true }, // v0：学习用；生产要校验 Origin
		},
		SendBuf:    1024,
		PongWait:   60 * time.Second,
		PingPeriod: 30 * time.Second,
		PingJitter: 100 * time.Millisecond,
		WriteWait:  5 * time.Second,
		ReadLimit:  1 << 10,
	}
}
func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	wsConn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := NewConn(s.Hub, wsConn)
	go s.writePump(c)
	go s.readPump(c)
}

func (s *Server) readPump(c *Conn) {
	defer func() {

		c.hub.RemoveConn(c)
		_ = c.ws.Close()
	}()

	c.ws.SetReadLimit(s.ReadLimit)
	//  处理poing链接
	c.lastPongUnix.Store(time.Now().UnixNano())
	_ = c.ws.SetReadDeadline(time.Now().Add(s.PongWait))
	c.ws.SetPongHandler(func(string) error {
		c.lastPongUnix.Store(time.Now().UnixNano())
		_ = c.ws.SetReadDeadline(time.Now().Add(s.PongWait))
		return nil
	})
	// 处理关闭连接
	c.ws.SetCloseHandler(func(code int, text string) error {
		_ = c.ws.SetReadDeadline(time.Now()) // 让 ReadMessage 立刻返回
		return nil
	})

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}
		_, b, err := c.ws.ReadMessage()
		if err != nil {
			// 看是不是 i/o timeout
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				log.Printf("read error is timeout : %v", err)
			} else {
				log.Printf("read error : %v", err)
				// log: other close/error
			}

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

const (
	writeWait = 5 * time.Second
	maxFlush  = 256 // 单次最多写多少条，防止订阅 topic 极多时一次写爆
)

func (s *Server) writePump(c *Conn) {
	// 相当于有个随机数等待
	if s.PingJitter > 0 {
		d := time.Duration(rand.Int63n(int64(s.PingJitter)))
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
		case <-s.ctx.Done():
			return
		}
	}

	ticker := time.NewTicker(s.PingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()

	for {
		select {
		case <-c.notify:
			batch := c.flushLatest(maxFlush)

			if len(batch) == 0 {
				continue
			}

			//  批量写：一次 NextWriter 写完本批（减少 syscall）
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			w, err := c.ws.NextWriter(websocket.TextMessage)
			if err != nil {
				log.Printf("NextWriter err: %v", err)
				return
			}

			for i, payload := range batch {
				if i > 0 {
					// 用换行分隔多条 JSON
					if _, err := w.Write([]byte("\n")); err != nil {
						log.Printf("writer close err: %v", err)

						_ = w.Close()
						return
					}
				}
				fmt.Printf("send receive is %+v\n", string(payload))
				if _, err := w.Write(payload); err != nil {
					_ = w.Close()
					log.Printf("writer2 close err: %v", err)
					return
				}
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(s.WriteWait))
			if err := c.ws.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(s.WriteWait)); err != nil {
				log.Printf("WriteControl ping err: %v", err)
				return
			}
		case <-s.ctx.Done():
			return
		}
	}
}
