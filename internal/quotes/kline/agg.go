package kline

import (
	"fmt"
	"strings"
	"time"
)

// Scale：定点数的缩放倍率（1e8 = 8位小数）
// 为什么不用 float64：
// - K 线需要大量比较(max/min)与累加(volume)，浮点误差会累积污染结果。
// - 8位精度对主流 crypto price/qty 一般足够，v0 先用固定 scale。
// 后续：可以根据每个 symbol 的 tickSize/stepSize 动态选择 scale（更严谨）。
const Scale = int64(100_000_000)

// Trade：聚合器的输入（来自你统一后的成交模型）
// 注意：这里 price/size 仍然用 string，聚合器内部再转成定点数 int64。
// 这样 Trade 模型保持“无损”，而数值计算用定点数保证准确性。
//type Trade struct {
//	Symbol   string // e.g. "BTC-USD" or "BTC-USDT"
//	PriceStr string // decimal string
//	SizeStr  string // decimal string
//	TsUnixMs int64  // trade time in ms（统一毫秒时间戳）
//	TradeID  string // optional: 用于调试/去重（v0不做去重）
//}

// Bar：K 线（OHLCV）
// - StartMs/EndMs 表示这个 bar 覆盖的时间窗：[Start, End)
// - Open/High/Low/Close/Volume 都是定点数（Scale=1e8）
// - Count：TradeAgg里表示 trade 数；RollupAgg里表示合并了多少个子 bar
type Bar struct {
	Symbol   string        `json:"symbol"`
	Interval time.Duration `json:"interval"`
	TF       string        `json:"tf"`
	StartMs  int64         `json:"start_ms"`
	EndMs    int64         `json:"end_ms"`

	Open  int64 `json:"open"`
	High  int64 `json:"high"`
	Low   int64 `json:"low"`
	Close int64 `json:"close"`

	Volume int64 `json:"volume"`
	Count  int64 `json:"count"`
}

// String：仅用于打印/调试（不要在热路径频繁用，字符串拼接有成本）
func (b Bar) String() string {
	return fmt.Sprintf("%s %s [%d,%d) O=%s H=%s L=%s C=%s V=%s n=%d",
		b.Symbol, b.Interval,
		b.StartMs, b.EndMs,
		FormatFixed(b.Open), FormatFixed(b.High), FormatFixed(b.Low), FormatFixed(b.Close),
		FormatFixed(b.Volume), b.Count,
	)
}

// TradeAgg 维护“每个 symbol 当前正在构建的那一根 bar”。
// 收到 trade 时：
// - 计算它属于哪个时间桶
// - 如果是新桶：先 emit 旧 bar，再创建新 bar
// - 如果是同桶：更新 OHLCV
// - 如果是旧桶（乱序）：v0 选择丢弃（后续可以加 reorder window）
type TradeAgg struct {
	intervalMs      int64 // bar 周期（毫秒）
	offsetMs        int64 // 桶对齐偏移（毫秒），用于时区对齐；0 表示 UTC
	reorderWindowMs int64 // 允许乱序窗口，0 表示不允许（等价旧版本）

	// cur：每个 symbol 一根“正在构建”的当前 bar
	sym map[string]*symState

	// emit：当一个 bar 关闭时，怎么把它输出给下游（回调函数）
	emit func(Bar)

	// lateDrops：v0 用于简单统计“丢了多少乱序的 trade”（后续会做成指标）
	lateDrops int64
}
type symState struct {
	latestTsMs int64
	// bars: key = bucketStartMs
	bars map[int64]*Bar

	// 为了保证 emit 顺序，我们记录已 emit 的最大 start（仅用于防重复）
	lastEmittedStartMs int64
	hasEmitted         bool
}

// NewTradeAgg：创建底层 trade 聚合器（比如 interval=1s）
// tzOffset：用于日线等按时区对齐；v0 默认传 0（UTC）
func NewTradeAgg(interval time.Duration, tzOffset time.Duration, emit func(Bar)) *TradeAgg {
	return NewTradeAggReorder(interval, tzOffset, 0, emit)
}

// NewTradeAggReorder：支持乱序窗口的构造函数
func NewTradeAggReorder(interval time.Duration, tzOffset time.Duration, reorderWindow time.Duration, emit func(Bar)) *TradeAgg {
	return &TradeAgg{
		intervalMs:      int64(interval / time.Millisecond),
		offsetMs:        int64(tzOffset / time.Millisecond),
		reorderWindowMs: int64(reorderWindow / time.Millisecond),
		sym:             make(map[string]*symState, 256),
		emit:            emit,
	}
}

// OfferTrade：喂入一笔 trade（上游可能一次消息包含多笔 trade，调用方循环喂即可）
//
// v0 的乱序策略：
//   - 如果 trade 属于比当前 bar 更早的桶（bs < cur.StartMs），直接丢弃。
//     真实生产通常会加一个“乱序窗口”（例如允许延迟 1~3 秒），v0-4.1 我们会加。
func (a *TradeAgg) OfferTrade(t Trade) {
	price, ok := ParseFixed(t.PriceStr)
	if !ok {
		return
	}
	size, ok := ParseFixed(t.SizeStr)
	if !ok {
		return
	}

	st := a.sym[t.Symbol]
	if st == nil {
		st = &symState{
			bars: make(map[int64]*Bar, 8), // 2s窗口 + 1s粒度 -> 通常只有3~4根
		}
		a.sym[t.Symbol] = st
	}

	// 更新 latestTsMs（按事件时间推进 watermark）
	if t.TsUnixMs > st.latestTsMs {
		st.latestTsMs = t.TsUnixMs
	}

	// 计算 watermark
	watermark := st.latestTsMs - a.reorderWindowMs

	// 超过窗口的乱序：丢弃
	if a.reorderWindowMs > 0 && t.TsUnixMs < watermark {
		a.lateDrops++
		// 仍然尝试 emit（因为 watermark 可能推进了）
		a.emitReady(st, watermark)
		return
	}

	bs := bucketStartMs(t.TsUnixMs, a.intervalMs, a.offsetMs)
	be := bs + a.intervalMs

	// 取该 bucket 的 bar（可能是历史桶，但在窗口内，允许更新）
	b := st.bars[bs]
	if b == nil {
		b = &Bar{
			Symbol:   t.Symbol,
			Interval: time.Duration(a.intervalMs) * time.Millisecond,
			StartMs:  bs,
			EndMs:    be,
			Open:     price,
			High:     price,
			Low:      price,
			Close:    price,
			Volume:   size,
			Count:    1,
		}
		st.bars[bs] = b
	} else {
		// 合并 OHLCV
		if price > b.High {
			b.High = price
		}
		if price < b.Low {
			b.Low = price
		}
		b.Close = price
		b.Volume += size
		b.Count++
	}

	// 每次 offer 后都尝试 emit 已经“水位线安全”的 bars
	a.emitReady(st, watermark)
}

// emitReady：把 EndMs <= watermark 的 bar 按时间顺序 emit 掉
func (a *TradeAgg) emitReady(st *symState, watermarkMs int64) {
	// 收集所有可输出的 start（窗口很小，排序开销可忽略）
	ready := make([]int64, 0, 8)
	for start, b := range st.bars {
		// 已经 emit 过的不要重复
		if st.hasEmitted && start <= st.lastEmittedStartMs {
			continue
		}
		if b.EndMs <= watermarkMs {
			ready = append(ready, start)
		}
	}
	if len(ready) == 0 {
		return
	}

	// 小规模排序（手写插入排序，避免引入 sort 包也行；这里用 sort 更清楚）
	// 你也可以直接 import "sort"
	for i := 1; i < len(ready); i++ {
		x := ready[i]
		j := i - 1
		for j >= 0 && ready[j] > x {
			ready[j+1] = ready[j]
			j--
		}
		ready[j+1] = x
	}

	for _, start := range ready {
		b := st.bars[start]
		if b == nil {
			continue
		}
		a.emit(*b)
		delete(st.bars, start)
		st.lastEmittedStartMs = start
		st.hasEmitted = true
	}
}

// Flush：输出所有 remaining bars（按 start 排序），用于退出/测试
func (a *TradeAgg) Flush() {
	for _, st := range a.sym {
		// 收集全部 start
		keys := make([]int64, 0, len(st.bars))
		for start := range st.bars {
			// 防止重复输出历史 emit（一般 flush 在退出时调用，不会重复）
			if st.hasEmitted && start <= st.lastEmittedStartMs {
				continue
			}
			keys = append(keys, start)
		}
		// 排序（同上：插入排序）
		for i := 1; i < len(keys); i++ {
			x := keys[i]
			j := i - 1
			for j >= 0 && keys[j] > x {
				keys[j+1] = keys[j]
				j--
			}
			keys[j+1] = x
		}
		for _, start := range keys {
			if b := st.bars[start]; b != nil && b.Count > 0 {
				a.emit(*b)
				st.lastEmittedStartMs = start
				st.hasEmitted = true
			}
		}
		// flush 后清空
		st.bars = make(map[int64]*Bar, 8)
	}
}

// ==============================
// 2) RollupAgg：低周期 Bar -> 高周期 Bar
// ==============================
//
// RollupAgg 用于：
// - 1s bar 合成 1m bar
// - 1m bar 合成 1h bar
// - 1h bar 合成 1d bar
//
// 逻辑和 TradeAgg 类似，只是输入是 child Bar：
// - Open = 第一个 child 的 Open
// - Close = 最后一个 child 的 Close
// - High/Low = 子 bar 的 max/min
// - Volume = 子 bar Volume 累加
type RollupAgg struct {
	intervalMs int64
	offsetMs   int64
	cur        map[string]*Bar
	emit       func(Bar)

	// ---- gap fill ----
	fillGaps  bool
	lastClose map[string]int64
	hasClose  map[string]bool
}

func NewRollupAgg(interval time.Duration, tzOffset time.Duration, emit func(Bar)) *RollupAgg {
	return NewRollupAggFill(interval, tzOffset, false, emit)
}

// fillGaps=true 时启用补空K
func NewRollupAggFill(interval time.Duration, tzOffset time.Duration, fillGaps bool, emit func(Bar)) *RollupAgg {
	return &RollupAgg{
		intervalMs: int64(interval / time.Millisecond),
		offsetMs:   int64(tzOffset / time.Millisecond),
		cur:        make(map[string]*Bar, 256),
		emit:       emit,
		fillGaps:   fillGaps,
		lastClose:  make(map[string]int64, 256),
		hasClose:   make(map[string]bool, 256),
	}
}

func (a *RollupAgg) OfferBar(child Bar) {
	bs := bucketStartMs(child.StartMs, a.intervalMs, a.offsetMs)
	be := bs + a.intervalMs

	cb := a.cur[child.Symbol]
	if cb == nil {
		a.cur[child.Symbol] = &Bar{
			Symbol:   child.Symbol,
			Interval: time.Duration(a.intervalMs) * time.Millisecond,
			StartMs:  bs,
			EndMs:    be,
			Open:     child.Open,
			High:     child.High,
			Low:      child.Low,
			Close:    child.Close,
			Volume:   child.Volume,
			Count:    1,
		}
		return
	}

	if bs > cb.StartMs {
		// 先输出旧桶
		a.emit(*cb)

		// 记录这个 symbol 的 lastClose（用于补空K）
		a.lastClose[child.Symbol] = cb.Close
		a.hasClose[child.Symbol] = true

		// ---- 补空K：输出 cb 后，如果 bs 跳过了多个桶，中间补空 ----
		if a.fillGaps && a.hasClose[child.Symbol] {
			next := cb.StartMs + a.intervalMs
			for next < bs {
				empty := Bar{
					Symbol:   child.Symbol,
					Interval: time.Duration(a.intervalMs) * time.Millisecond,
					StartMs:  next,
					EndMs:    next + a.intervalMs,
					Open:     a.lastClose[child.Symbol],
					High:     a.lastClose[child.Symbol],
					Low:      a.lastClose[child.Symbol],
					Close:    a.lastClose[child.Symbol],
					Volume:   0,
					Count:    0,
				}
				a.emit(empty)
				// 空K的 close 仍然等于 lastClose（不变）
				next += a.intervalMs
			}
		}

		// 开新桶，用 child 初始化
		*cb = Bar{
			Symbol:   child.Symbol,
			Interval: time.Duration(a.intervalMs) * time.Millisecond,
			StartMs:  bs,
			EndMs:    be,
			Open:     child.Open,
			High:     child.High,
			Low:      child.Low,
			Close:    child.Close,
			Volume:   child.Volume,
			Count:    1,
		}
		return
	}

	if bs < cb.StartMs {
		// 乱序：v0 先丢
		return
	}

	// 同桶合并
	if child.High > cb.High {
		cb.High = child.High
	}
	if child.Low < cb.Low {
		cb.Low = child.Low
	}
	cb.Close = child.Close
	cb.Volume += child.Volume
	cb.Count++
}

func (a *RollupAgg) Flush() {
	for sym, cb := range a.cur {
		if cb != nil && cb.Count > 0 {
			a.emit(*cb)
			a.lastClose[sym] = cb.Close
			a.hasClose[sym] = true
		}
	}
}

//
// ==============================
// helpers：归桶 + 定点数
// ==============================

// bucketStartMs：计算某个时间戳属于哪个桶的开始时间（毫秒）
//
// offsetMs 用于“按时区对齐”桶边界：
// - offsetMs=0：按 UTC 对齐
// - offsetMs=8h：按 UTC+8 对齐（例如日线从台北时间 00:00 开始）
//
// 公式：((ts+off)/interval)*interval - off
func bucketStartMs(tsMs, intervalMs, offsetMs int64) int64 {
	x := tsMs + offsetMs
	return (x/intervalMs)*intervalMs - offsetMs
}

// ParseFixed：把 decimal string 解析为定点 int64（scale=1e8）
//
// 特性/取舍（v0）：
// - 小数最多取 8 位，多余部分直接截断（不是四舍五入）
// - 不做科学计数法处理（例如 1e-8 不支持）
// - 输入必须是标准十进制字符串（交易所给的正是这种）
func ParseFixed(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	}

	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	fracPart := ""
	if len(parts) == 2 {
		fracPart = parts[1]
	}

	// 解析整数部分（手写逐位解析，避免 float）
	var ip int64
	for i := 0; i < len(intPart); i++ {
		c := intPart[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		ip = ip*10 + int64(c-'0')
	}

	// 解析小数部分：最多 8 位，不足补 0
	fp := int64(0)
	digits := 0
	for i := 0; i < len(fracPart) && digits < 8; i++ {
		c := fracPart[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		fp = fp*10 + int64(c-'0')
		digits++
	}
	for digits < 8 {
		fp *= 10
		digits++
	}

	val := ip*Scale + fp
	if neg {
		val = -val
	}
	return val, true
}

// FormatFixed：把定点 int64 转回字符串，用于打印/debug
func FormatFixed(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	ip := v / Scale
	fp := v % Scale
	s := fmt.Sprintf("%d.%08d", ip, fp)
	if neg {
		return "-" + s
	}
	return s
}
