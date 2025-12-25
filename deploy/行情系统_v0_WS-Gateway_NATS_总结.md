# 行情系统 v0（Market Data）——可扩展 WS Gateway + NATS Pub/Sub 总结（面试可讲）

> 时间：2025-12-25（Asia/Taipei）  
> 范围：**实时行情接入（Coinbase/Binance）→ Trade 统一模型 → 多周期 K 线聚合 → NATS 广播 → WS Gateway 推送**；历史链路（InfluxDB）作为可选扩展。

---

## 1. 背景与目标

- **目标**：做一个接近生产级的行情系统原型，具备“能跑、可观测、可扩展”的架构骨架；以**10K 连接压测**为起点，架构上支持**100K 在线**（通过 WS Gateway 水平扩展，而非单进程硬顶）。
- **约束**：v0 优先跑通链路与可扩展形态；性能优化“可逐步推进”，不追求一次到位。

---

## 2. 理论基础（面试讲得清的关键点）

### 2.1 为什么要拆成 WS Gateway（连接层）+ Broker（消息层）
- **WS 是长连接**：连接维护、慢客户端治理、心跳、订阅关系、快照回放等都属于“连接层”职责。
- **行情生产（聚合/编码）应与连接承载解耦**：把消息通过 Broker 广播给多个 Gateway，各 Gateway 只负责推送自己节点的连接，从而**线性扩展连接数**（加机器即可扩容）。  
- **Pub/Sub 一对多 fan-out**：发布者向某个 subject 发送消息，所有活跃订阅者都能收到，是典型的分发模型。citeturn0search1turn0search4

### 2.2 为什么 WS 单连接只允许“1读+1写”
gorilla/websocket 明确：每条连接只支持**一个并发 reader**与**一个并发 writer**；但 `Close` 与 `WriteControl` 可以与其他方法并发调用。  
这决定了常见模型必须是 `readPump` + `writePump` 两个 goroutine（每连接），并且写侧要避免多处并发写。citeturn0search2turn0search10

### 2.3 为什么 NATS 适合做 v0 的行情广播
- NATS Core 是轻量高吞吐的 Pub/Sub，总线模型适合“实时行情 fan-out”。citeturn0search1turn0search13  
- subject 是 **`.` 分隔的层级命名**，支持 `*`（单 token）与 `>`（全匹配）通配订阅：例如订阅 `kline.>` 接收全部 K 线。citeturn0search4turn0search8turn0search16  
- v0 默认 **at-most-once**（在线订阅才收到），足够支撑实时推送；若要“持久化/回放/至少一次”，再升级 JetStream（后续 TODO）。citeturn0search5

### 2.4 为什么 Nginx/负载均衡建议 least_conn
WebSocket 是长连接，连接持续时间长，按“最少连接”分配可以更均匀地分摊在线连接数。Nginx 官方文档给出 `least_conn` upstream 负载均衡方式。citeturn0search3turn0search7

---

## 3. 架构总览

### 3.1 数据流（实时）
1) **数据源接入**：Coinbase / Binance WebSocket → Trade 统一模型  
2) **聚合层**：ShardedAggregator（按 symbol 分片）  
   - 乱序吸收：`reorderWindow=2s`  
   - 多周期：1s/1m/1h/1d  
   - 缺口补齐（fill gaps）  
3) **编码与广播**：`EncodeBar -> topic + JSON payload` → NATS Publish  
4) **WS Gateway（可多实例）**：NATS Subscribe（如 `kline.>`）→ 本地 Hub（快照+订阅表）→ WebSocket 推送给前端

### 3.2 可选：历史链路
- 聚合后的 Bar 写入时序库（InfluxDB）用于历史查询；
- 前端页面加载：HTTP 拉历史 + WS 订阅实时更新（经典做法）。

---

## 4. 核心数据模型

### 4.1 K 线 Bar（你当前实现）
```go
type Bar struct {
    Symbol   string
    Interval time.Duration

    StartMs int64 // inclusive
    EndMs   int64 // exclusive

    Open  int64
    High  int64
    Low   int64
    Close int64

    Volume int64 // sum(size) scaled
    Count  int64
}
```

### 4.2 Topic/Subject 约定
- Topic（系统内部/WS）：`kline:<tf>:<symbol>`  
  - 例：`kline:1s:BTC-USD`
- NATS subject：把 `:` 替换为 `.`  
  - 例：`kline.1s.BTC-USD`
- 订阅所有 K 线：`kline.>`（通配符订阅）。citeturn0search4turn0search16

---

## 5. 关键实现（代码骨架）

> 下面是“面试可讲”的实现点：**为什么这么做**比完整代码更重要。

### 5.1 ShardedAggregator：多生产者 → 分片 inbox（channel）→ 单线程聚合
- `OfferTrade` 仅做 **channel send**，不需要锁（channel 并发安全，且 shards slice 运行期只读）。
- `DropWhenFull=true`：v0 防止全链路阻塞（可观测性与稳定性优先）。

### 5.2 WS Hub/Conn：LatestOnly + notify 合并唤醒 + 批量写
- `latest map[topic][]byte`：每 topic 只保留最新消息（conflation），避免慢客户端导致队列无限增长；
- `notify chan struct{}`（缓冲 1）：合并多次唤醒，减少 writePump 争用；
- `NextWriter` 批量写 + `\n` 分隔多条 JSON：减少 syscall，提升吞吐；
- `Hub.last[topic]`：保存最新快照，订阅时可立即回放，降低首包延迟。

### 5.3 WS 并发模型遵循 gorilla 约束
- 每连接 `readPump` 负责订阅控制消息（sub/unsub）；
- 每连接 `writePump` 负责写数据与 Ping；
- 只允许一个写 goroutine 调用 `NextWriter/WriteMessage/SetWriteDeadline` 等写方法。citeturn0search2turn0search10

### 5.4 Broker 抽象 + NATS 实现（核心“可扩展形态”）
- 抽象 `Broker` 接口：`Publish(topic,payload)` + `Subscribe(subjects)`；
- 单机用 in-memory 实现也能跑；
- 多机用 NATS 实现即可把消息广播到多个 Gateway。

---

## 6. 运行方式（本地）

### 6.1 启动 NATS
```bash
docker run -d --name nats \
  -p 4222:4222 -p 8222:8222 \
  nats:latest
```

### 6.2 观察 NATS 是否在收发（订阅打印）
用 nats-box（容器自带 nats CLI）订阅全部 K 线：
```bash
docker run --rm -it natsio/nats-box:latest sh -lc \
'nats sub -s nats://127.0.0.1:4222 "kline.>"'
```

### 6.3 启动两个进程
- `cmd/marketdata`：接入交易所数据 → 聚合 bar → 发布到 NATS  
- `cmd/ws-gateway`：订阅 NATS → Hub → 对外提供 `/ws`

---

## 7. 压测结论（v0 结论口径）
- 本机 Mac + k6 在 10K 连接下已跑通端到端推送；
- 主要瓶颈更偏向 **压测端与本机内核资源上限**（连接建立/心跳尖峰/FD 等），而不是业务逻辑本身；
- v0 已做的优化：WS 写侧批量写、LatestOnly conflation、订阅快照回放、Hub 并发安全修复等。

---

## 8. 尚未优化点（TODO 清单，面试可说“下一步”）

### 8.1 连接层（WS Gateway）
- [ ] **Ping jitter**：分散心跳，避免 30s 周期性尖峰（thundering herd）
- [ ] **慢客户端硬治理**：pendingBytes / writeLag 超阈值主动踢；避免拖垮整体
- [ ] **topicID 化 + sharded hub**：减少 string/map 开销与全局锁竞争
- [ ] **CloseOnce**：read/write 双方关闭协同，避免 “use of closed network connection” 日志风暴
- [ ] **鉴权/限流**：WS 握手鉴权（JWT/签名）、连接数/订阅数限制、IP 级限流

### 8.2 Broker 层（NATS）
- [ ] **JetStream**：需要持久化/回放/至少一次投递时引入（行情补齐、断线续传）
- [ ] **主题治理**：subject 分层规划（env/region/market/tf/symbol），便于权限与订阅隔离
- [ ] **多生产者策略**：按 symbol 分片生产（owner）或选主，避免重复 publish

### 8.3 数据层（历史）
- [ ] InfluxDB 写入：批量、压缩、重试、幂等覆盖（同一根 bar key）
- [ ] 历史查询 API：分页、时间范围、对齐、冷热分层（更大规模时）

### 8.4 可观测性
- [ ] Prometheus 指标：ws_conns、drops、close_code、write_lag、pong_age、broker_pub/sub 等
- [ ] 分布式追踪：marketdata → broker → gateway（最少 trace id 打通）
- [ ] 统一结构化日志：带 conn_id、topic、close_reason、lag 等字段

### 8.5 更极致的单机连接数（可选研究）
- [ ] 学习 gnet（event-loop / epoll/kqueue）：减少 goroutine-per-conn 的内存与调度开销（但工程复杂度更高）

---

## 9. 面试讲解提纲（1 分钟版本）
- 我把行情系统拆成两层：**行情生产（聚合/编码）** 和 **连接分发（WS Gateway）**，中间用 **NATS Pub/Sub** 解耦。
- 行情生产端把 bar 编码成 `topic + payload` 发到 NATS；多个 Gateway 订阅后各自 fanout 给连接，实现**水平扩展 100K**。
- WS 侧遵循 gorilla 约束（1读1写），用 **LatestOnly conflation + batch write + snapshot replay** 保障吞吐与首包体验。
- v0 已经在本机 10K 跑通；下一步优化是 ping jitter、慢客户端治理、topicID/sharded hub，以及需要可靠回放时引入 JetStream。

---

## 参考资料
- NATS Core Pub/Sub：fan-out 分发模型 citeturn0search1  
- NATS subjects 与通配符（`.` 分层、`*`、`>`） citeturn0search4turn0search8turn0search16  
- gorilla/websocket 并发约束（1 reader + 1 writer；Close/WriteControl 可并发） citeturn0search2turn0search10  
- Nginx least_conn 负载均衡（适合长连接） citeturn0search3turn0search7  
- nats.go（Go client，含 Drain 等能力；JetStream 支持） citeturn0search9turn0search5
