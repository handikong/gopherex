package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

func wsEcho(w http.ResponseWriter, r *http.Request) {
	// 定义所有的地址都链接上
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"localhost"},
	})
	if err != nil {
		return
	}
	// 链接进行断开
	defer c.Close(websocket.StatusNormalClosure, "bye")
	ctx := r.Context()
	for {
		//  设置每个连接的读写
		rCtx, rCancelFunc := context.WithTimeout(ctx, time.Second*30)
		read, msg, err := c.Read(rCtx)
		rCancelFunc()
		if err != nil {
			return
		}
		// 设置写的超时
		wCtx, wCancelFunc := context.WithTimeout(ctx, time.Second*5)
		err = c.Write(wCtx, read, msg)
		wCancelFunc()
		if err != nil {
			return
		}
	}
}

// 开发一个echo服务器

func main() {
	http.HandleFunc("/ws", wsEcho)
	log.Println("listen :8080  (ws://127.0.0.1:8080/ws)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
