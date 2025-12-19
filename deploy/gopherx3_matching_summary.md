# GopherX3 撮合系统阶段性归档（Step5.2–Step5.3）

> 归档范围：撮合核心（订单簿/撮合）+ Actor 撮合引擎 + cmd.wal/outbox/publisher 串联 + E2E 测试与 Benchmark/pprof 性能优化闭环。  
> 备注：行情系统与 K8s HA（主备/切主）已记录，等统一 K8s 部署再做。

---

## 目录
1. 背景与目标
2. 总体架构与数据流
3. 模块拆解
4. 测试体系（E2E）
5. 性能压测体系（Benchmark）
6. pprof 定位与关键优化
7. 当前完成度与边界
8. 参考资料
9. 代码示例

---

## 1. 背景与目标

### 1.1 我们要解决的问题
- **低延迟撮合**：订单进来后在内存中撮合并推进状态，尽量减少堆分配/锁竞争。
- **可恢复**：关键输入/输出落盘，支持重启恢复。
- **可优化**：用 `Benchmark + pprof` 建立“定位 → 改动 → 验证”的性能闭环。

### 1.2 关键工程原则（已落地）
- **单交易对单写（single-writer）**：同一 symbol 的 order book 必须全序执行（价格优先/时间优先）。
- **热路径 0 alloc 优先**：撮合核心尽量做到每次撮合/入队不产生堆分配，并用 bench/pprof 验证。
- **Outbox 语义**：对外事件（成交/订单状态变化）通过 outbox 记录与发布，允许 at-least-once（可能重复但不丢）。

## 2. 总体架构与数据流

### 2.1 组件分层
- **Matching / OrderBook（撮合核心）**：LevelOrderBook（价位桶 FIFO、byID O(1)）、best price 维护（heap + lazy deletion）、pool/buffer 复用。
- **Engine / Actor（撮合引擎）**：`TrySubmit` 路由到 `SymbolActor`，`SymbolActor.Run` 单线程批处理 mailbox；`seq` 表示处理进度。
- **Persistence（持久化）**：`cmd.wal`（输入命令日志）、`outbox(ev.wal)`（输出事件日志）、`cursor`（publisher 位点）。
- **Publisher / Bus（发布）**：publisher 从 outbox+cursor 读事件并 `bus.Publish`；`CmdEnd` 作为命令边界，cursor 推进到安全点。

### 2.2 数据流（文字版）
1) Client/Test/Bench 调用 `TrySubmit(cmd)`
2) Engine 找到/创建 `SymbolActor(symbol)`，把 cmd 入 mailbox
3) Actor 串行处理：落 `cmd.wal` → 撮合/emit → 事件写 outbox → 写 `CmdEnd`
4) Publisher tail outbox：普通事件 publish 成功才推进 offset；`CmdEnd` 不发布只落 cursor

## 3. 模块拆解

### 3.1 订单簿/撮合（OrderBook）
你已完成并优化的核心点（按你这几天的实现归纳）：
- 价位桶（Level）维护 FIFO
- byID O(1) 撤单
- best price 维护（min/max heap + lazy deletion）
- nodePool / buffer 复用，撮合热路径尽量 0 alloc（并用 bench 验证）

典型输出事件（对外语义）：
- `Accepted` / `Added`
- `Trade`
- `Canceled` / `Rejected`

### 3.2 Actor 撮合引擎（Engine + SymbolActor）
关键点：
- 单 symbol 单 actor 单写，保证全序
- `BatchMax` 批处理降低调度成本
- `seq` 用于“处理进度”观测（测试/bench 等待）

我们实际踩到的坑（直接导致 benchmark 超时）：
- 如果 `TrySubmit` 走的是 **TryEnqueue（mailbox 满返回 error）**，bench 忽略 error 会导致“实际入队远小于预期”，最终 `seq` 等待超时。
- 修复：bench 统计成功提交数（submitted），并用“批次提交 + 等待追上”的方式测量。

### 3.3 WAL / Outbox / Publisher（可恢复 + 可发布）
目标：
- `cmd.wal`：输入命令日志（重启回放恢复订单簿）
- `outbox(ev.wal)`：输出事件日志（publisher 消费并发布）
- `cursor`：publisher 消费位点（断点续推）

publisher 关键语义（你现在的实现要点）：
- 普通事件：`Publish` 成功后才推进 offset；失败则回滚到 committedOff 重试（不丢）
- `CmdEnd`：不发布，只推进 cursor 到“命令边界安全点”

---

## 4. 测试体系（E2E）
目标：验证最小闭环
- Submit 两笔对手单 → 撮合 Trade → 事件写 outbox → publisher/bus 能工作

实践建议：
- E2E 使用 **JSON codec**（方便读 wal/outbox 排障）
- 性能压测使用 **binary codec**（避免 json.Marshal 造成大量分配）

## 5. 性能压测体系（Benchmark）

### 5.1 为什么不能只靠 Test
- `Test` 关注正确性
- `Benchmark` 才能稳定输出：`ns/op, B/op, allocs/op`
- 你的撮合是异步 actor：如果只测 `TrySubmit` 入队，会得到“虚假的超低 ns/op”（真正工作在后台 goroutine）。

### 5.2 可信 bench 的两条铁律
1) 把**完成条件**纳入统计：例如等待 `actor.seq` 达到目标。
2) 处理**背压**：mailbox 满会导致 TrySubmit/Enqueue 失败，bench 必须统计成功提交数，并分批等待追上。

---

## 6. pprof 定位与关键优化（真实踩坑记录）

### 6.1 你实际看到的典型现象
- CPU Top 常见 `pthread_cond_wait/signal`：多数是“等待 actor 追上”的同步噪声（不是撮合计算的热点）。
- alloc_space：
  - JSON 阶段：`encoding/json.Marshal`、JSON codec、outbox append 等占大头
  - binary 阶段：热点收敛到 outbox/wal/初始化等更真实成本

### 6.2 优化 A：JSON → 二进制
结论：JSON 编解码导致明显 `B/op` 与 `allocs/op`，切到 binary 后 profile 更干净，更能反映撮合+写盘本身。

### 6.3 优化 B：修复 outbox 中“栈 buffer 逃逸到堆”
你贴出的 pprof `list` 显示 alloc_space 打在 outbox 的 `var buf [evRecordLen]byte` 这一行。
原因（本质）：slice 指向栈数组并跨函数边界传递，编译器无法证明生命周期安全 → 触发逃逸 → 每次 Append 分配。
修复（高收益、改动小）：把 scratch buffer 放到 `EventOutbox` 结构字段里（堆上一份，多次复用）。

## 7. 当前完成度与边界

### 7.1 已完成
- 订单簿/撮合：数据结构优化 + bench/pprof 验证
- actor 引擎：TrySubmit → actor mailbox → 串行 apply
- Step5.2：cmd.wal 回放恢复订单簿
- Step5.3：outbox + publisher + cursor 断点续推
- E2E 测试 + 基准压测（bench）+ pprof 定位优化闭环

### 7.2 已记录暂缓（不污染当前项目，后续统一补齐）
- snapshot/快恢复（避免回放“几天 WAL”）
- 主备 HA / 切主 / fencing / 通知去重（统一 K8s 部署时落地）
- 撮合后的资金/结算/用户回报/行情等“交易闭环”模块

## 8. 参考资料（理论与工具）

```text
LMAX Architecture（单写状态机 + 事件驱动交易架构的经典公开资料）
https://martinfowler.com/articles/lmax.html

Transactional Outbox Pattern
https://microservices.io/patterns/data/transactional-outbox.html

Go pprof
https://pkg.go.dev/net/http/pprof
https://github.com/google/pprof/blob/master/doc/README.md
```

## 9. 代码示例

### 9.1 最小 E2E：Submit → Trade（骨架）

```go
func TestE2E_Publisher_SubmitToTradeFlow(t *testing.T) {
    const sym = "BTCUSDT"
    dir := t.TempDir()

    bus := NewChanBus(1 << 16)

    cfg := EngineConfig{
        WALDir:          dir,
        EnableCmdWAL:    true,
        EnableOutbox:    true,
        EnablePublisher: true,
        PublisherPoll:   10 * time.Millisecond,
        bus:             bus,

        // 测试用 JSON：方便读 ev/cmd.wal，也确保 publisher decode 能过
        CmdCodec: JSONCmdCodec{Version: 1},
        EvCodec:  JSONEvCodec{Version: 1},

        ActorCfg:     ActorConfig{MailboxSize: 4096, BatchMax: 256},
        BookFactory: func(symbol string) (OrderBook, error) {
            // 这里替换为你当前可编译的 adapter
            return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
        },
    }

    eng := NewEngine(cfg)

    // 两笔对手单 -> 触发成交
    require.NoError(t, eng.TrySubmit(sym, Command{
        Type: CmdSubmitLimit, ReqID: 1, OrderID: 1001, UserID: 2001,
        Side: Buy, Price: 100, Qty: 10,
    }))
    require.NoError(t, eng.TrySubmit(sym, Command{
        Type: CmdSubmitLimit, ReqID: 2, OrderID: 1002, UserID: 2002,
        Side: Sell, Price: 100, Qty: 10,
    }))

    // 断言方式二选一：
    // 1) 订阅 bus 等到 Trade
    // 2) 或扫描 outbox 文件验证出现 Trade 事件
}
```

### 9.2 可信 Benchmark：批次提交 + 等 actor.seq（避免“只测入队”的错觉）

```go
func BenchmarkE2E_SubmitToTradeFlow(b *testing.B) {
    const sym = "BTCUSDT"
    dir := b.TempDir()

    cfg := EngineConfig{
        WALDir:       dir,
        EnableCmdWAL: true,
        EnableOutbox: true,
        EnablePublisher: false, // bench 先测撮合+写盘，不含发布
        bus: nil,

        CmdCodec: BinCmdCodec{},
        EvCodec:  BinEvCodec{},
        ActorCfg: ActorConfig{MailboxSize: 4096, BatchMax: 256},
        BookFactory: func(symbol string) (OrderBook, error) {
            return &HeapBookAdapter{B: matching.NewLevelOrderHeapBook()}, nil
        },
    }
    eng := NewEngine(cfg)

    // warmup：确保 actor 创建
    _ = eng.TrySubmit(sym, Command{Type: CmdSubmitLimit, ReqID: 1, OrderID: 1, Side: Buy, Price: 100, Qty: 1})
    a := eng.mustGetActor(sym) // 同包测试可直接拿 actor

    runtime.GC()
    b.ReportAllocs()
    b.ResetTimer()

    const batchPairs = 512
    submitted := 0
    startSeq := a.seq

    i := 0
    for i < b.N {
        end := i + batchPairs
        if end > b.N {
            end = b.N
        }

        for ; i < end; i++ {
            base := uint64(i) * 2
            if err := eng.TrySubmit(sym, Command{Type: CmdSubmitLimit, ReqID: 100 + base, OrderID: 1_000_000 + base, Side: Buy, Price: 100, Qty: 10}); err == nil {
                submitted++
            }
            if err := eng.TrySubmit(sym, Command{Type: CmdSubmitLimit, ReqID: 101 + base, OrderID: 1_000_001 + base, Side: Sell, Price: 100, Qty: 10}); err == nil {
                submitted++
            }
        }

        // ✅ 等待 actor 处理追上（完成条件）
        target := startSeq + uint64(submitted)
        waitSeq(b, a, target, 3*time.Second)
    }

    target := startSeq + uint64(submitted)
    waitSeq(b, a, target, 5*time.Second)
}

func waitSeq(b *testing.B, a *SymbolActor, target uint64, timeout time.Duration) {
    deadline := time.Now().Add(timeout)
    for a.seq < target {
        if time.Now().After(deadline) {
            b.Fatalf("timeout waiting actor seq=%d target=%d", a.seq, target)
        }
        runtime.Gosched()
    }
}
```

### 9.3 Outbox 逃逸修复：栈 buf → 结构字段复用（减少 alloc_space 大头）

```go
type EventOutbox struct {
    w     *wal.Writer
    codec EvCodec

    // 堆上一份，多次复用（避免每次 Append 都为 buf 分配）
    binBuf  []byte // cap >= evRecordLen
    jsonBuf []byte // cap >= 256
}

func NewEventOutbox(w *wal.Writer, codec EvCodec) *EventOutbox {
    return &EventOutbox{
        w: w,
        codec: codec,
        binBuf:  make([]byte, 0, evRecordLen),
        jsonBuf: make([]byte, 0, 256),
    }
}

func (o *EventOutbox) Append(ev Event) error {
    var dst []byte
    switch o.codec.(type) {
    case JSONEvCodec:
        dst = o.jsonBuf[:0]
    default:
        dst = o.binBuf[:0]
    }
    payload, _ := o.codec.Encode(dst, ev)
    return o.w.Append(payload)
}
```

### 9.4 WAL Writer.Append（你当前实现的二进制记录写入）

```go
func (w *Writer) Append(payload []byte) error {
    var hrd [headerSize]byte
    binary.LittleEndian.PutUint32(hrd[:4], uint32(len(payload)))
    binary.LittleEndian.PutUint32(hrd[4:], crc32.ChecksumIEEE(payload))

    if _, err := w.bw.Write(hrd[:]); err != nil {
        return ErrCorruptHeader
    }
    if _, err := w.bw.Write(payload); err != nil {
        return ErrCorruptPayload
    }

    w.off += int64(headerSize + len(payload))
    return nil
}
```

### 9.5 pprof 常用命令（你这几天实际用到的套路）

```bash
# 跑基准并打印 alloc 指标
go test ./internal/engine -run '^$' -bench BenchmarkE2E_SubmitToTradeFlow -benchmem -count 5

# 生成 profile
go test ./internal/engine -run '^$' -bench BenchmarkE2E_SubmitToTradeFlow -benchmem \
  -cpuprofile /tmp/e2e.cpu.pprof -memprofile /tmp/e2e.mem.pprof

# CPU 热点
go tool pprof -top /tmp/e2e.cpu.pprof | head -n 30

# 内存分配热点（alloc_space）
go tool pprof -top -alloc_space /tmp/e2e.mem.pprof | head -n 30

# 精确定位到某个函数/行
go tool pprof -alloc_space /tmp/e2e.mem.pprof
# (pprof) list gopherex.com/internal/engine.(*EventOutbox).Append
```
