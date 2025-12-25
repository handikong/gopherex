package wsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"gopherex.com/exec/networks/md"
)

type Server struct {
	Hub          *md.Hub
	WriteTimeout time.Duration
}

func NewServer(h *md.Hub) *Server {
	return &Server{
		Hub:          h,
		WriteTimeout: 2 * time.Second,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ex := strings.ToLower(r.URL.Query().Get("exchange"))
	if ex == "" {
		ex = "coinbase"
	}
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "missing symbol", 400)
		return
	}
	ival := r.URL.Query().Get("interval")
	interval, err := time.ParseDuration(ival)
	if err != nil || (interval != time.Second && interval != time.Minute) {
		http.Error(w, "interval must be 1s or 1m", 400)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost:*", "127.0.0.1:*"},
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	topic := md.Topic{Exchange: ex, Symbol: symbol, Interval: interval}
	sub := s.Hub.Subscribe(topic)
	defer s.Hub.Unsubscribe(topic, sub)

	// 建一个可取消的 ctx：任何一端出错都能停掉两个 loop
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 读循环：主要用于及时感知 close（以及处理控制帧）
	go func() {
		defer cancel()
		for {
			// 客户端通常不会发业务消息；这里读到错误（断开/关闭）就退出
			_, _, err := conn.Read(ctx)
			if err != nil {
				return
			}
		}
	}()

	// 写循环：把 K 线推给客户端
	for {
		select {
		case <-ctx.Done():
			return
		case k, ok := <-sub.Chan():
			if !ok {
				return
			}
			b, _ := json.Marshal(k) // M0 用 JSON；后面换 protobuf
			wctx, wcancel := context.WithTimeout(ctx, s.WriteTimeout)
			err := conn.Write(wctx, websocket.MessageText, b)
			wcancel()
			if err != nil {
				cancel()
				return
			}
		}
	}
}
