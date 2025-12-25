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
	"time"
)

const hdrSize = 4

const (
	tPing byte = 1
	tPong byte = 2
	tData byte = 3
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "server addr")
	mode := flag.String("mode", "normal", "fast|normal|slow|idle")
	// slow：收到 PING 后延迟多久再回 PONG（用来测试 miss 边界）
	slowDelay := flag.Duration("slow_delay", 3*time.Second, "delay before replying PONG in slow mode")
	// fast：主动发 DATA 的频率（帮助你看到 server miss 永远归零）
	fastData := flag.Duration("fast_data", 500*time.Millisecond, "send DATA interval in fast mode (0=disable)")
	flag.Parse()

	c, err := net.DialTimeout("tcp", *addr, 2*time.Second)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer c.Close()

	log.Printf("[cli] connected %s mode=%s", *addr, *mode)

	// idle：不读不写（hb 下必然会触发 miss -> close；blackhole 下也不触发读）
	if *mode == "idle" {
		for {
			time.Sleep(time.Hour)
		}
	}

	r := bufio.NewReaderSize(c, 32*1024)
	w := bufio.NewWriterSize(c, 32*1024)

	// fast：主动周期性发 DATA（让 server 的 readFull 能读到字节）
	if *mode == "fast" && *fastData > 0 {
		go func() {
			tk := time.NewTicker(*fastData)
			defer tk.Stop()
			for range tk.C {
				_ = c.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
				payload := []byte(fmt.Sprintf("hello ts=%d", time.Now().UnixNano()))
				if err := writeFrame(w, tData, payload); err != nil {
					log.Printf("[cli] send DATA err: %v", err)
					return
				}
				if err := w.Flush(); err != nil {
					log.Printf("[cli] flush err: %v", err)
					return
				}
			}
		}()
	}

	// 读循环：收到 server 的 PING 就回 PONG（normal/fast/slow 都会回）
	for {
		_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))

		msg, err := readFull(r, 4<<20)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				log.Printf("[cli] read timeout")
				continue
			}
			if err == io.EOF {
				log.Printf("[cli] EOF")
			} else {
				log.Printf("[cli] read err: %v", err)
			}
			return
		}
		if len(msg) == 0 {
			continue
		}

		typ := msg[0]
		payload := msg[1:]

		switch typ {
		case tPing:
			log.Printf("[cli] <- PING %q", safeASCII(payload))

			if *mode == "slow" && *slowDelay > 0 {
				time.Sleep(*slowDelay)
			}

			_ = c.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
			if err := writeFrame(w, tPong, []byte("pong")); err != nil {
				log.Printf("[cli] write PONG err: %v", err)
				return
			}
			if err := w.Flush(); err != nil {
				log.Printf("[cli] flush err: %v", err)
				return
			}
			log.Printf("[cli] -> PONG")

		case tPong:
			log.Printf("[cli] <- PONG %q", safeASCII(payload))

		case tData:
			head := payload
			if len(head) > 32 {
				head = head[:32]
			}
			log.Printf("[cli] <- DATA len=%d head=%q", len(payload), safeASCII(head))

		default:
			log.Printf("[cli] <- UNKNOWN typ=%d len=%d", typ, len(payload))
		}
	}
}

// ===== framing =====

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

func writeFrame(w *bufio.Writer, typ byte, payload []byte) error {
	n := 1 + len(payload)
	var hdr [hdrSize]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(n))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if err := w.WriteByte(typ); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
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
