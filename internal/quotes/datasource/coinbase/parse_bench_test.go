package coinbase

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// ---------- 基准用的最小结构（只为 Unmarshal） ----------
type cbMarketTradesMsgTest struct {
	Channel     string `json:"channel"`
	ClientID    string `json:"client_id"`
	Timestamp   string `json:"timestamp"`
	SequenceNum int64  `json:"sequence_num"`
	Events      []struct {
		Type   string `json:"type"` // snapshot / update
		Trades []struct {
			TradeID   string `json:"trade_id"`
			ProductID string `json:"product_id"`
			Price     string `json:"price"`
			Size      string `json:"size"`
			Side      string `json:"side"` // BUY / SELL
			Time      string `json:"time"`
		} `json:"trades"`
	} `json:"events"`
}

var (
	payload1  = []byte(`{"channel":"market_trades","client_id":"","timestamp":"2023-02-09T20:19:35.39625135Z","sequence_num":0,"events":[{"type":"snapshot","trades":[{"trade_id":"000000000","product_id":"ETH-USD","price":"1260.01","size":"0.3","side":"BUY","time":"2019-08-14T20:42:27.265Z"}]}]}`)
	payload50 = []byte(makeMarketTradesPayload(50))
)

// 生成一个包含 n 条 trades 的 market_trades 消息（在 init 阶段生成，不计入 benchmark 时间）
func makeMarketTradesPayload(n int) string {
	var b strings.Builder
	b.Grow(256 + n*160)

	b.WriteString(`{"channel":"market_trades","client_id":"","timestamp":"2023-02-09T20:19:35.39625135Z","sequence_num":0,"events":[{"type":"update","trades":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		// 注意：trade_id/price/size/time 都是 string（和 Coinbase 示例一致）
		fmt.Fprintf(&b, `{"trade_id":"%09d","product_id":"ETH-USD","price":"1260.%02d","size":"0.%d","side":"BUY","time":"2019-08-14T20:42:27.265Z"}`, i, i%100, (i%9)+1)
	}
	b.WriteString(`]}]}`)
	return b.String()
}

// ---------- 1) 纯 Unmarshal：定位 encoding/json scanner 成本 ----------
func BenchmarkCoinbase_Unmarshal_1Trade(b *testing.B)   { benchUnmarshal(b, payload1) }
func BenchmarkCoinbase_Unmarshal_50Trades(b *testing.B) { benchUnmarshal(b, payload50) }

func benchUnmarshal(b *testing.B, payload []byte) {
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	var msg cbMarketTradesMsgTest
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg = cbMarketTradesMsgTest{} // 清空，避免 slice 复用影响观测
		if err := json.Unmarshal(payload, &msg); err != nil {
			b.Fatal(err)
		}
		// 防止编译器过度优化（虽然 Unmarshal 已经有副作用，这里更稳）
		if len(msg.Events) == 0 {
			b.Fatal("no events")
		}
	}
}

// ---------- 2) 你的 ParseCoinbaseMarketTrades：测总开销 ----------
// ⚠️ 这里你只需要把返回值的接收方式改成你的真实签名即可。
// pprof 显示它在 parse.go:29 里调用 json.Unmarshal，所以非常适合对比优化前后。
func BenchmarkCoinbase_Parse_1Trade(b *testing.B)   { benchParse(b, payload1) }
func BenchmarkCoinbase_Parse_50Trades(b *testing.B) { benchParse(b, payload50) }

func benchParse(b *testing.B, payload []byte) {
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// TODO: 按你的真实函数签名调整这一行
		_, err := ParseCoinbaseMarketTrades(payload)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// 可选：并行 benchmark（看多核下的伸缩，尤其你后面会 shard）
func BenchmarkCoinbase_Parse_50Trades_Parallel(b *testing.B) {
	b.ReportAllocs()
	payload := payload50
	b.SetBytes(int64(len(payload)))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := ParseCoinbaseMarketTrades(payload)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
