package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

const hdrSize = 4

const (
	tPing byte = 1
	tPong byte = 2
	tData byte = 3
)

type connState struct {
	c       net.Conn
	r       *bufio.Reader
	send    chan []byte   // 写路径隔离 所有出栈队列都走这个
	done    chan struct{} // 关闭通道
	close1  sync.Once     //  这个不知道哦啊是干嘛的
	writeTO time.Duration // 写的时间
}

func newConnState(c net.Conn, sendCap int, writeTO time.Duration) *connState {
	return &connState{
		c:       c,
		r:       bufio.NewReaderSize(c, 32*1024),
		send:    make(chan []byte, sendCap),
		done:    make(chan struct{}),
		writeTO: writeTO,
	}
}

func (s *connState) close() {
	s.close1.Do(func() {
		s.c.Close()
		close(s.done)
	})
}

// 非阻塞发送
func (s *connState) trySend(b []byte) bool {
	//  超过数量就丢弃
	select {
	case s.send <- b:
		return true
	case <-s.done:
		return false
	default:
		return false
	}
}

// ========================
// writer loop：唯一写者
// ========================
func writerLoop(s *connState) {
	ra := s.c.RemoteAddr().String()
	for {
		select {
		case <-s.done:
			return
		case b := <-s.send:
			_ = s.c.SetWriteDeadline(time.Now().Add(s.writeTO))
			if err := writeAll(s.c, b); err != nil {
				log.Printf("[writer] write err %s: %v -> close", ra, err)
				s.close()
				return
			}
		}
	}
}

func main() {
	mode := flag.String("mode", "hb", "hb=heartbeat (single-thread)")
	addr := flag.String("addr", ":8080", "listen addr")
	recvBuf := flag.Int("rbuf", 32*1024, "TCP read buffer bytes (blackhole)")
	readTO := flag.Duration("read_to", 2*time.Second, "read timeout (hb/echo)")
	missMax := flag.Int("miss", 3, "max missed heartbeats before close (hb)")
	writeTO := flag.Duration("write_to", 500*time.Millisecond, "write timeout")
	sendCap := flag.Int("send_cap", 32, "per-conn send queue capacity")

	pushEvery := flag.Duration("push_every", 20*time.Millisecond, "push interval (push)")
	pushSize := flag.Int("push_size", 4096, "payload bytes per DATA frame (push)")
	pushLogEvery := flag.Int("push_log_every", 200, "log once per N enqueued frames (push)")

	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	log.Printf("[srv] listen %s (NO goroutine) mode=%s", *addr, *mode)

	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("[srv] accept err: %v", err)
			continue
		}
		log.Printf("[srv] accepted %s -> handling (blocking accept loop)", c.RemoteAddr())
		go func(c net.Conn) {
			ra := c.RemoteAddr().String()
			log.Printf("[srv] +conn %s", ra)
			defer func() {
				_ = c.Close()
				log.Printf("[srv] -conn %s", ra)
			}()
			switch *mode {
			case "blackhole":
				runBlackholeServer(c, *recvBuf)
				return
			case "hb":
				st := newConnState(c, *sendCap, *writeTO)
				go writerLoop(st)
				runHeartbeatServer(st, *readTO, *missMax)
				st.close()
				log.Printf("[srv] -conn %s", ra)
				return
			case "echo":
				st := newConnState(c, *sendCap, *writeTO)
				go writerLoop(st)
				runEchoServer(st, *readTO)
				st.close()
				return
			case "push":
				st := newConnState(c, *sendCap, *writeTO)
				go writerLoop(st)
				runPushServer(st, *pushEvery, *pushSize, *pushLogEvery)
				st.close()
				return

			default:
				_ = c.Close()
				log.Printf("[srv] unknown mode=%s", *mode)
				return
			}
		}(c)

	}
}

func writeAll(c net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := c.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}
func runBlackholeServer(c net.Conn, recvBuf int) {
	ra := c.RemoteAddr().String()
	log.Printf("[bh] open %s (never read, rbuf=%d)", ra, recvBuf)

	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(recvBuf)
	}
	for {
		time.Sleep(10 * time.Second)
		log.Printf("[bh] still alive %s (not reading)", ra)
	}
}

func runHeartbeatServer(s *connState, readTO time.Duration, missMax int) {
	ra := s.c.RemoteAddr().String()
	log.Printf("[hb] open %s read_to=%s miss=%d", ra, readTO, missMax)

	miss := 0
	for {
		_ = s.c.SetReadDeadline(time.Now().Add(readTO))
		msg, err := readFull(s.r, 4<<20)

		if err != nil {
			// 读超时：对端没发任何字节
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				miss++
				log.Printf("[hb] read timeout %s miss=%d/%d -> enqueue PING", ra, miss, missMax)

				// ✅ 不直接写：只投递到 send 队列
				if ok := s.trySend(makeFrame(tPing, []byte("ping"))); !ok {
					log.Printf("[hb] send queue full -> close %s", ra)
					return
				}

				if miss >= missMax {
					log.Printf("[hb] miss limit reached -> close %s", ra)
					return
				}
				continue
			}

			if err == io.EOF {
				log.Printf("[hb] EOF %s", ra)
			} else {
				log.Printf("[hb] read err %s: %v", ra, err)
			}
			return
		}

		if len(msg) == 0 {
			continue
		}

		typ := msg[0]
		payload := msg[1:]
		miss = 0 // 收到任何帧都认为活跃

		switch typ {
		case tPing:
			log.Printf("[hb] <- PING %s", ra)
			if ok := s.trySend(makeFrame(tPong, []byte("pong"))); !ok {
				log.Printf("[hb] send queue full -> close %s", ra)
				return
			}
		case tPong:
			log.Printf("[hb] <- PONG %s", ra)
		case tData:
			head := payload
			if len(head) > 32 {
				head = head[:32]
			}
			log.Printf("[hb] <- DATA %s len=%d head=%q", ra, len(payload), safeASCII(head))
		default:
			log.Printf("[hb] <- UNKNOWN typ=%d %s", typ, ra)
		}
	}
}

func runEchoServer(s *connState, readTO time.Duration) {
	ra := s.c.RemoteAddr().String()
	log.Printf("[echo] open %s", ra)

	for {
		_ = s.c.SetReadDeadline(time.Now().Add(readTO))
		msg, err := readFull(s.r, 4<<20)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				log.Printf("[echo] read timeout %s", ra)
				continue
			}
			if err == io.EOF {
				log.Printf("[echo] EOF %s", ra)
			} else {
				log.Printf("[echo] read err %s: %v", ra, err)
			}
			return
		}

		// echo 回原 payload（不带 typ）
		if ok := s.trySend(makeFull(msg)); !ok {
			log.Printf("[echo] send queue full -> close %s", ra)
			return
		}
	}
}

func runPushServer(s *connState, every time.Duration, payloadSize int, logEvery int) {
	ra := s.c.RemoteAddr().String()
	if every <= 0 {
		every = 1 * time.Millisecond
	}
	if payloadSize < 0 {
		payloadSize = 0
	}
	if logEvery <= 0 {
		logEvery = 200
	}

	log.Printf("[push] open %s every=%s payload=%dB", ra, every, payloadSize)

	// ✅ 为了更清楚观察背压，payload 固定，frame 也固定复用（0 alloc 的思路）
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = 'A'
	}
	frame := makeFrame(tData, payload)

	tk := time.NewTicker(every)
	defer tk.Stop()

	var n uint64
	for {
		select {
		case <-s.done:
			return
		case <-tk.C:
			if ok := s.trySend(frame); !ok {
				log.Printf("[push] send queue full/closed -> close %s (client too slow / writer blocked)", ra)
				return
			}
			n++
			if n%uint64(logEvery) == 0 {
				log.Printf("[push] enqueued %d frames to %s", n, ra)
			}
		}
	}
}

func readFull(r *bufio.Reader, maxFrame int) ([]byte, error) {
	var hdr [hdrSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > uint32(maxFrame) {
		return nil, fmt.Errorf("frame too large: %d > %d", n, maxFrame)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// makeFrame: 造 [4B len][1B typ][payload...]
func makeFrame(typ byte, payload []byte) []byte {
	n := 1 + len(payload)
	b := make([]byte, 4+n)
	binary.BigEndian.PutUint32(b[:4], uint32(n))
	b[4] = typ
	copy(b[5:], payload)
	return b
}

// makeFull: 造 [4B len][payload...]
func makeFull(payload []byte) []byte {
	b := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(b[:4], uint32(len(payload)))
	copy(b[4:], payload)
	return b
}

func safeASCII(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, x := range b {
		if x >= 32 && x <= 126 {
			out = append(out, x)
		} else {
			out = append(out, '.')
		}
	}
	return string(out)
}

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}
