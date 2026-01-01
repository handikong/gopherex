package wsmetrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Conns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ws_conns",
		Help: "Active websocket connections",
	})
	ConnOpenTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_conn_open_total",
		Help: "Total we		bsocket connections opened",
	})
	ConnCloseTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_conn_close_total",
		Help: "Total websocket connections closed, partitioned by close code and reason",
	}, []string{"code", "reason"})

	SubOpsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_sub_ops_total",
		Help: "Total subscription operations",
	}, []string{"op"}) // sub/unsub

	MsgsOutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_msgs_out_total",
		Help: "Total websocket messages sent out (logical messages, not frames)",
	})
	BytesOutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_bytes_out_total",
		Help: "Total websocket bytes sent out",
	})
	WriteErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_write_errors_total",
		Help: "Total websocket write errors",
	})
	DroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ws_dropped_total",
		Help: "Total dropped messages",
	}, []string{"why"})

	PingSentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_ping_sent_total",
		Help: "Total ping sent",
	})
	PingErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_ping_errors_total",
		Help: "Total ping send errors",
	})
	PongRecvTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_pong_recv_total",
		Help: "Total pong received",
	})
	PongTimeoutTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ws_pong_timeout_total",
		Help: "Total pong timeouts",
	})

	WriteDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ws_write_duration_seconds",
		Help:    "Duration of a websocket write batch",
		Buckets: prometheus.ExponentialBuckets(0.0005, 2, 14), // 0.5ms -> ~4s
	})
	BatchSize = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ws_batch_size",
		Help:    "Number of messages per flush/batch",
		Buckets: []float64{1, 2, 4, 8, 16, 32, 64, 128, 256},
	})
)

func OnOpen() {
	Conns.Inc()
	ConnOpenTotal.Inc()
}

func OnClose(code int, reason string) {
	Conns.Dec()
	ConnCloseTotal.WithLabelValues(strconv.Itoa(code), reason).Inc()
}

func ObserveWrite(batchN int, bytes int, dur time.Duration, err error) {
	if batchN > 0 {
		MsgsOutTotal.Add(float64(batchN))
		BatchSize.Observe(float64(batchN))
	}
	if bytes > 0 {
		BytesOutTotal.Add(float64(bytes))
	}
	WriteDuration.Observe(dur.Seconds())
	if err != nil {
		WriteErrorsTotal.Inc()
	}
}
