package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DbPoolOpen = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "app_db_pool_open",
		Help: "Current open DB connections",
	})
	DbPoolIdle         = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_db_pool_idle"})
	DbPoolInuse        = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_db_pool_inuse"})
	DbPoolWaitCount    = promauto.NewCounter(prometheus.CounterOpts{Name: "app_db_pool_wait_count"})
	DbPoolWaitDuration = promauto.NewCounter(prometheus.CounterOpts{Name: "app_db_pool_wait_seconds"})

	RedisPoolOpen         = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_redis_pool_open"})
	RedisPoolIdle         = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_redis_pool_idle"})
	RedisPoolInuse        = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_redis_pool_inuse"})
	RedisPoolWaitCount    = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_redis_pool_wait_count"})
	RedisPoolWaitDuration = promauto.NewGauge(prometheus.GaugeOpts{Name: "app_redis_pool_wait_seconds"})

	DbQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "app_db_query_duration_seconds",
		Help:    "DB query latency",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15), // 1ms ~ 16s
	}, []string{"query", "status"})

	RedisCmdDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "app_redis_cmd_duration_seconds",
		Help:    "Redis command latency",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 15),
	}, []string{"cmd", "status"})
	RedisErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "app_redis_errors_total",
		Help: "Redis errors",
	}, []string{"cmd", "code"})
)
