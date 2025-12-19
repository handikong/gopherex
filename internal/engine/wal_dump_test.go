package engine

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"gopherex.com/pkg/wal"
)

/***************
 * JSON wrappers
 ***************/
type cmdJSONTest struct {
	V   uint8   `json:"v"`
	Seq uint64  `json:"seq"`
	Cmd Command `json:"cmd"`
}

type evJSONTest struct {
	V  uint8 `json:"v"`
	Ev Event `json:"ev"`
}

/***********************
 * Type name translators
 ***********************/
func evTypeName(t uint8) string {
	// 你日志里：1 Accepted, 3 Added, 5 Trade, 250 CmdEnd
	switch t {
	case 1:
		return "Accepted"
	case 2:
		return "Rejected"
	case 3:
		return "Added"
	case 4:
		return "Cancelled"
	case 5:
		return "Trade"
	case 250:
		return "CmdEnd"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

func cmdTypeName(t uint8) string {
	// 这里按你们 Command.Type 的常见枚举写个默认映射；
	// 如果你们的值不同，改这里即可。
	switch t {
	case 1:
		return "SubmitLimit"
	case 2:
		return "Cancel"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

func sideName(s uint8) string {
	switch s {
	case Buy:
		return "Buy"
	case Sell:
		return "Sell"
	default:
		return fmt.Sprintf("Side(%d)", s)
	}
}

/***********************
 * Pretty printers (core)
 ***********************/
func DumpCmdWALPretty(t *testing.T, path string) {
	t.Helper()

	r, err := wal.OpenReader(path, 0, wal.ReaderOptions{AllowTruncatedTail: true})
	if err != nil {
		t.Fatalf("open cmd wal reader: %v (path=%s)", err, path)
	}
	defer r.Close()

	t.Logf("========== CMD WAL DUMP (%s) ==========", path)

	i := 0
	for {
		p, nextOff, e := r.Next()
		if e != nil {
			if errors.Is(e, io.EOF) {
				break
			}
			t.Fatalf("read cmd wal: %v", e)
		}

		line := renderCmdPayload(p)
		t.Logf("[%03d] off=%d  %s", i, nextOff, line)
		i++
	}

	if r.TruncatedTail() {
		t.Logf("[cmd] truncatedTail=true lastGoodOffset=%d", r.LastGoodOffset())
	}
}

func DumpEvWALPretty(t *testing.T, path string) {
	t.Helper()

	r, err := wal.OpenReader(path, 0, wal.ReaderOptions{AllowTruncatedTail: true})
	if err != nil {
		t.Fatalf("open ev wal reader: %v (path=%s)", err, path)
	}
	defer r.Close()

	t.Logf("========== EV WAL DUMP (%s) ==========", path)

	i := 0
	for {
		p, nextOff, e := r.Next()
		if e != nil {
			if errors.Is(e, io.EOF) {
				break
			}
			t.Fatalf("read ev wal: %v", e)
		}

		line := renderEvPayload(p)
		t.Logf("[%03d] off=%d  %s", i, nextOff, line)
		i++
	}

	if r.TruncatedTail() {
		t.Logf("[ev] truncatedTail=true lastGoodOffset=%d", r.LastGoodOffset())
	}
}

/***********************
 * Payload rendering
 ***********************/
func renderCmdPayload(p []byte) string {
	// JSON 优先（你现在测试就是 JSON）
	if looksLikeJSON(p) {
		var rec cmdJSONTest
		if err := json.Unmarshal(p, &rec); err == nil {
			c := rec.Cmd
			// 这里按你们 Command 常用字段打印（没有的字段编译会报错，你删掉对应字段即可）
			// 目标：一眼看懂 “seq + type + req + 核心参数”
			switch c.Type {
			case CmdSubmitLimit:
				return fmt.Sprintf(
					"Seq:%d  Type:%d(%s)  ReqID:%d  OrderID:%d  UserID:%d  Side:%s  Price:%d  Qty:%d",
					rec.Seq, c.Type, cmdTypeName(uint8(c.Type)), c.ReqID, c.OrderID, c.UserID, sideName(c.Side), c.Price, c.Qty,
				)
			case CmdCancel:
				return fmt.Sprintf(
					"Seq:%d  Type:%d(%s)  ReqID:%d  CancelOrderID:%d",
					rec.Seq, c.Type, cmdTypeName(uint8(c.Type)), c.ReqID, c.CancelOrderID,
				)
			default:
				return fmt.Sprintf(
					"Seq:%d  Type:%d(%s)  ReqID:%d  (raw cmd=%s)",
					rec.Seq, c.Type, cmdTypeName(uint8(c.Type)), c.ReqID, compactJSON(p),
				)
			}
		}
	}

	// fallback：不是 JSON 就原样打印（你后续想支持二进制 decode，再补这里）
	return "payload=" + safeText(p)
}

func renderEvPayload(p []byte) string {
	if looksLikeJSON(p) {
		var rec evJSONTest
		if err := json.Unmarshal(p, &rec); err == nil {
			ev := rec.Ev
			name := evTypeName(uint8(ev.Type))

			switch ev.Type {
			case 1: // Accepted
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d  OrderID:%d  UserID:%d → %s",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, ev.OrderID, ev.UserID, name,
				)
			case 3: // Added
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d  OrderID:%d  UserID:%d → %s",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, ev.OrderID, ev.UserID, name,
				)
			case 4: // Cancelled
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d  OrderID:%d → %s",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, ev.OrderID, name,
				)
			case 2: // Rejected
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d  OrderID:%d  UserID:%d  Reason:%q → %s",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, ev.OrderID, ev.UserID, ev.Reason, name,
				)
			case 5: // Trade
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d  MakerOrderID:%d  TakerOrderID:%d  Price:%d  Qty:%d → %s",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, ev.MakerOrderID, ev.TakerOrderID, ev.Price, ev.Qty, name,
				)
			case 250: // CmdEnd
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d → CmdEnd(%d)",
					ev.Type, name, ev.Seq, ev.Seq,
				)
			default:
				return fmt.Sprintf(
					"Type:%d %s  Seq:%d  ReqID:%d  Idx:%d (raw ev=%s)",
					ev.Type, name, ev.Seq, ev.ReqID, ev.Idx, compactJSON(p),
				)
			}
		}
	}

	return "payload=" + safeText(p)
}

func DumpCursorPretty(t *testing.T, cursorPath, evPath string, previewN int) {
	t.Helper()

	t.Logf("========== CURSOR DUMP ==========")
	t.Logf("cursor: %s", cursorPath)
	t.Logf("ev.wal:  %s", evPath)

	// 1) cursor 文件信息
	b, err := os.ReadFile(cursorPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("cursor: <not exists> (treat as offset=0)")
		} else {
			t.Logf("cursor: read error: %v", err)
		}
	} else {
		t.Logf("cursor: bytes=%d", len(b))
		if len(b) < 8 {
			t.Logf("cursor: <invalid> len<8 (treat as offset=0)")
		} else {
			off := int64(binary.LittleEndian.Uint64(b[:8]))
			t.Logf("cursor offset: %d", off)
		}
	}

	off := loadCursor(cursorPath) // 你已有的 helper（不存在返回 0）
	t.Logf("cursor offset (loadCursor): %d", off)

	// 2) ev.wal 文件大小
	if st, e := os.Stat(evPath); e != nil {
		if os.IsNotExist(e) {
			t.Logf("ev.wal: <not exists>")
			return
		}
		t.Logf("ev.wal: stat error: %v", e)
		return
	} else {
		t.Logf("ev.wal size: %d", st.Size())
		if off > st.Size() {
			t.Logf("WARNING: cursor offset > ev.wal size (cursor=%d size=%d)", off, st.Size())
		}
	}

	// 3) 预览：从 cursor 开始读 N 条 record 的 payload（不 decode，直接打印长度/前缀）
	if previewN <= 0 {
		return
	}

	r, err := wal.OpenReader(evPath, off, wal.ReaderOptions{AllowTruncatedTail: true})
	if err != nil {
		t.Logf("preview: OpenReader error: %v", err)
		return
	}
	defer r.Close()

	t.Logf("preview from cursor (N=%d):", previewN)
	for i := 0; i < previewN; i++ {
		p, nextOff, e := r.Next()
		if e != nil {
			if errors.Is(e, io.EOF) {
				t.Logf("  [%d] EOF", i)
				return
			}
			t.Logf("  [%d] Next error: %v", i, e)
			return
		}
		s := safeText(p)
		t.Logf("  [%d] nextOff=%d payloadLen=%d payload=%s", i, nextOff, len(p), s)
	}

	if r.TruncatedTail() {
		t.Logf("preview: truncatedTail=true lastGoodOffset=%d", r.LastGoodOffset())
	}
}

/***********************
 * Small helpers
 ***********************/
func looksLikeJSON(p []byte) bool {
	s := strings.TrimSpace(string(p))
	return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")
}

func compactJSON(p []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, p); err != nil {
		return safeText(p)
	}
	return buf.String()
}

func safeText(p []byte) string {
	// 防止 payload 里有不可见字符把日志刷爆
	s := string(p)
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	if len(s) > 240 {
		return s[:240] + "…"
	}
	return s
}
