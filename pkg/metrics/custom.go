package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	RateLimitBlockTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gopherex",
			Name:      "ratelimit_block_total",
			Help:      "Total number of rate limit blocks.",
		},
		[]string{"service", "method", "reason"},
	)

	CBRejectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "gopherex",
			Name:      "circuitbreaker_reject_total",
			Help:      "Total number of circuit breaker rejections.",
		},
		[]string{"service", "method", "reason"},
	)

	CBState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "gopherex",
			Name:      "circuitbreaker_state",
			Help:      "Circuit breaker state (0/1).",
		},
		[]string{"service", "method", "state"}, // state: closed/open/half_open
	)
)

func MustRegister() {
	prometheus.MustRegister(RateLimitBlockTotal, CBRejectTotal, CBState)
}
