package ratelimit

import (
	"sync"
	"time"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Rule struct {
	// Half-Open 状态允许通过的探测请求数（MaxRequests=0 时库会当作 1） :contentReference[oaicite:1]{index=1}
	MaxRequests uint32

	// Closed 状态计数窗口
	Interval time.Duration

	// Rolling window 每个 bucket 周期（>0 则启用 rolling window；<=0 用 fixed window） :contentReference[oaicite:2]{index=2}
	BucketPeriod time.Duration

	// Open 状态持续时间，到期进入 Half-Open :contentReference[oaicite:3]{index=3}
	Timeout time.Duration

	// 触发熔断条件（两种之一即可）
	TripConsecutiveFailures uint32  // 连续失败阈值（建议 10~50）
	TripFailureRate         float64 // 失败率阈值（0~1），比如 0.5
	TripMinRequests         uint32  // 失败率计算的最小样本数，比如 20
}

type Manager struct {
	mu sync.RWMutex
	m  map[string]*gobreaker.CircuitBreaker[struct{}]

	defaultRule Rule
	rules       map[string]Rule
}

func NewManager(defaultRule Rule, perMethod map[string]Rule) *Manager {

	if defaultRule.MaxRequests == 0 {
		defaultRule.MaxRequests = 5
	}
	if defaultRule.Timeout <= 0 {
		defaultRule.Timeout = 3 * time.Second
	}
	if defaultRule.Interval <= 0 {
		defaultRule.Interval = 10 * time.Second
	}
	if defaultRule.TripConsecutiveFailures == 0 && defaultRule.TripFailureRate == 0 {
		defaultRule.TripConsecutiveFailures = 10
	}
	if defaultRule.TripMinRequests == 0 {
		defaultRule.TripMinRequests = 20
	}

	return &Manager{
		m:           make(map[string]*gobreaker.CircuitBreaker[struct{}], 64),
		defaultRule: defaultRule,
		rules:       perMethod,
	}
}

func (m *Manager) Get(method string) *gobreaker.CircuitBreaker[struct{}] {
	// 快路径：读锁
	m.mu.RLock()
	cb := m.m[method]
	m.mu.RUnlock()
	if cb != nil {
		return cb
	}

	// 慢路径：创建
	m.mu.Lock()
	defer m.mu.Unlock()

	if cb = m.m[method]; cb != nil {
		return cb
	}

	rule, ok := m.rules[method]
	if !ok {
		rule = m.defaultRule
	}
	st := gobreaker.Settings{
		Name:         method,
		MaxRequests:  rule.MaxRequests,
		Interval:     rule.Interval,
		BucketPeriod: rule.BucketPeriod,
		Timeout:      rule.Timeout,

		ReadyToTrip: func(c gobreaker.Counts) bool {
			// 1) 连续失败阈值优先（最直观）
			if rule.TripConsecutiveFailures > 0 && c.ConsecutiveFailures >= rule.TripConsecutiveFailures {
				return true
			}
			// 2) 失败率阈值（适合波动流量）
			if rule.TripFailureRate > 0 && c.Requests >= rule.TripMinRequests {
				failRate := float64(c.TotalFailures) / float64(c.Requests)
				return failRate >= rule.TripFailureRate
			}
			return false
		},

		// IsSuccessful 决定“哪些错误计入熔断失败”。Settings.IsSuccessful 在 v2.3.0 存在。 :contentReference[oaicite:4]{index=4}
		IsSuccessful: func(err error) bool {
			return isSuccessfulForBreaker(err)
		},
	}

	cb = gobreaker.NewCircuitBreaker[struct{}](st)
	m.m[method] = cb
	return cb
}

func isSuccessfulForBreaker(err error) bool {
	if err == nil {
		return true
	}

	st, ok := status.FromError(err)
	if !ok {
		// 非 gRPC status 的 error：按失败计入（更保守）
		return false
	}

	switch st.Code() {
	// ✅ “业务可预期/不代表依赖不健康” -> 不计入熔断失败
	case codes.InvalidArgument,
		codes.NotFound,
		codes.PermissionDenied,
		codes.Unauthenticated,
		codes.AlreadyExists,
		codes.FailedPrecondition,
		codes.OutOfRange,
		codes.Canceled:
		return true

	// ❌ 这些通常代表依赖不健康/网络/超时/过载 -> 计入熔断失败
	case codes.Unavailable,
		codes.DeadlineExceeded,
		codes.Internal,
		codes.Unknown,
		codes.ResourceExhausted, // 下游限流/资源耗尽：建议计入，让调用方降压
		codes.Aborted,
		codes.DataLoss:
		return false

	default:
		return false
	}
}
