### 多层级构建
FROM golang:1.25-alpine as builder

### 设置环境变量
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /build

# 1. 缓存依赖层 (如果 go.mod 没变，这一步会直接用缓存)
COPY go.mod go.sum ./
RUN go mod download

# 2. 编译代码
COPY . .

RUN go build -ldflags="-s -w" -o gopherex ./apps/wallet/cmd/main.go

# ============================
# 阶段 2: 运行层 (Runner)
# ============================
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# 复制二进制文件
COPY --from=builder /build/gopherex .

# 修正这里：确保路径是连在一起的，指向 builder 里的完整路径
COPY --from=builder /build/apps/wallet/etc/wallet.yaml .

# 启动命令
CMD [ "./gopherex", "-f", "./wallet.yaml" ]





# 第一阶段：多链资产系统 —— 攻克“并发与数据一致性” (Day 18 - Day 32)
# 核心课题：金融系统的基石是不丢币、不错账。我们在开发钱包时，同步学习MySQL锁、Redis分布式锁、Docker编排。

# Day 18 (Today): BTC UTXO 模型 [已完成]。

# Day 19 (ETH开发 + 并发模型):

# 业务: 开发 ETH 提现功能。

# 架构融合: 为什么 ETH 不能像 BTC 那样并发？深入理解 Nonce 设计。如何用 Redis 管理 Nonce 实现伪并发？

# Day 20 (ERC20 + ABI):

# 业务: 开发 USDT 提现。

# 架构融合: 学习 Go 的反射与 ABI 编码原理。

# Day 21 (Docker 容器化实战):

# 场景: 我们不能一直依靠本地环境。

# 架构融合: 编写 Dockerfile。为什么要分阶段构建？ 如何减小镜像体积？用 docker-compose 编排 App, MySQL, Redis, Geth。

# Day 22 (幂等性与防重):

# 场景: 用户手抖连点两次“提现”。

# 架构融合: 设计接口幂等性。在代码中集成“业务流水号 + 唯一索引”机制。

# Day 23-24 (分布式锁深度实战):

# 场景: 两个协程同时操作同一个用户的余额。

# 架构融合: 抛弃 sync.Mutex。手写 Redis Lua 脚本实现分布式锁。理解 Redlock 原理。在转账代码中直接应用它。

# Day 25 (数据库设计与索引优化):

# 场景: 钱包表数据量大了查得慢。

# 架构融合: Schema 设计。为什么金额要用 Decimal？索引实战：覆盖索引、最左前缀。在建表时就考虑到未来的查询性能。

# Day 26 (资金安全与对账):

# 场景: 数据库被黑或者代码有 Bug。

# 架构融合: 设计 T+1 对账系统。实现一个独立进程（Watcher），监控链上余额与 DB 余额的差异。

# Day 27-28 (资金归集逻辑):

# 场景: 交易所要把用户的钱汇聚起来。

# 架构融合: 设计一个后台 Sweeper 服务。如何处理 ETH 的 Gas 费预充值？这是典型的任务调度场景。

# Day 29-32 (微服务拆分初探):

# 场景: 钱包模块太重了，要独立出来。

# 架构融合: 定义 gRPC 接口。将 Wallet 拆分为独立服务。

# 🔵 第二阶段：高性能撮合引擎 —— 攻克“内存计算与数据结构” (Day 33 - Day 48)
# 核心课题：交易所的核心是速度。我们在写撮合时，同步学习高级数据结构、Go内存优化、定序算法。

# Day 33 (架构选型理论):

# 思考: 为什么要用内存撮合？数据库撮合为什么慢？LMAX 架构是什么？

# 决策: 确定基于内存定单簿 (OrderBook) 的架构。

# Day 34 (核心数据结构设计):

# 场景: OrderBook 需要极其快速的插入、删除和排序。

# 架构融合: 学习并实现 跳表 (SkipList)。对比它与红黑树的优劣。直接用跳表实现买卖队列。

# Day 35 (定序与分布式ID):

# 场景: 高并发下如何保证定单先来后到？

# 架构融合: 实现 Snowflake (雪花算法)。理解时钟回拨问题。

# Day 36-37 (撮合核心逻辑):

# 业务: 实现限价单 (Limit) 和市价单 (Market) 的匹配。

# 架构融合: 对象池 (sync.Pool) 优化。定单对象频繁创建销毁，如何减少 GC 压力？在代码中集成 sync.Pool。

# Day 38 (持久化与 WAL):

# 场景: 机器断电了，内存里的定单怎么办？

# 架构融合: 设计 WAL (Write Ahead Log)。先写日志，再修改内存。模拟 Redis 的 AOF 机制。

# Day 39-40 (高性能流水线):

# 场景: 锁竞争太严重，拖慢撮合速度。

# 架构融合: 使用 Go Channel 设计无锁流水线。利用 GMP 模型，实现“单协程串行撮合”。

# Day 41-42 (与钱包的交互 - 最终一致性):

# 场景: 撮合成功了，要异步通知钱包扣款。

# 架构融合: 引入 Kafka/RocketMQ。设计消息结构。理解“削峰填谷”。在撮合输出端直接对接 MQ。

# Day 43-48 (撮合压测与调优):

# 实战: 编写 Benchmark。使用 pprof 分析 CPU 和 内存。优化热点代码。

# 🟠 第三阶段：行情推送系统 —— 攻克“高并发网络编程” (Day 49 - Day 60)
# 核心课题：如何维持百万长连接？我们在写推送时，同步学习WebSocket、Epoll、Redis PubSub。

# Day 49 (数据流设计):

# 思考: 撮合产生的数据怎么变成 K 线？

# 架构融合: 设计发布/订阅模式。撮合(Pub) -> Redis -> 网关(Sub)。

# Day 50-51 (WebSocket 网关实现):

# 业务: 建立 WS 连接，推送数据。

# 架构融合: 理解 Gorilla WebSocket 源码。如何管理百万个 Connection 对象？

# Day 52 (K线计算服务):

# 业务: 1分钟、1小时 K 线生成。

# 架构融合: 时间窗口算法。如何处理乱序数据？

# Day 53 (深度图增量推送):

# 场景: 每次推全量深度太浪费带宽。

# 架构融合: Diff 算法。计算 OrderBook 的变动量，只推差异。优化网络流量。

# Day 54-55 (网络模型优化):

# 场景: Go 原生 Net 模型即使很强，但在海量连接下也有瓶颈。

# 架构融合: 了解 Netpoller 和 Epoll。了解 gnet 等高性能网络库的设计理念。

# Day 56-60 (全链路联调):

# 实战: 机器人下单 -> 撮合匹配 -> MQ -> 清算 -> 推送行情 -> 前端收到。打通任督二脉。

# 🔴 第四阶段：微服务治理与云原生 —— 攻克“稳定性与运维” (Day 61 - Day 70)
# 核心课题：系统写好了，怎么稳健地跑在生产环境？同步学习gRPC、K8s、监控。

# Day 61-62 (API 网关与鉴权):

# 场景: 这么多微服务，前端怎么调？

# 架构融合: 搭建 API Gateway。集成 JWT 鉴权。实现 令牌桶限流 防止被攻击。

# Day 63 (服务发现与配置中心):

# 场景: 服务 IP 变了怎么办？

# 架构融合: 引入 Etcd。实现服务的自动注册与发现。实现配置热更新。

# Day 64 (分布式事务 - 兜底):

# 场景: 极端情况下，MQ 消息丢了怎么办？

# 架构融合: 实现 本地消息表 模式。确保钱包和撮合的数据最终绝对一致。

# Day 65 (K8s 部署 - 够用版):

# 实战: 编写 Deployment 和 Service 的 yaml。把微服务跑在本地 K8s 集群里。

# Day 66 (可观测性 - 监控):

# 架构融合: 集成 Prometheus。在代码里埋点（Metrics），监控 QPS、延迟、Goroutine 数量。

# Day 67 (可观测性 - 链路追踪):

# 架构融合: 集成 Jaeger/OpenTelemetry。看清一个请求在微服务间是怎么跳跃的。

# Day 68-69 (混沌工程与故障演练):

# 实战: 故意杀掉 Redis，故意断网。验证系统的容灾能力。

# Day 70 (毕业):

# 总结: 整理架构图，梳理技术难点，模拟面试。