package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// RequestsTotal tracks total rate limit checks partitioned by result.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "ratelimiter",
		Name:      "requests_total",
		Help:      "Total rate limit requests by key_prefix and decision.",
	}, []string{"key_prefix", "decision"}) // decision: "allowed" | "denied"

	// RequestDuration records the latency of the Allow RPC (seconds).
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "ratelimiter",
		Name:      "request_duration_seconds",
		Help:      "Histogram of Allow RPC latencies.",
		Buckets:   []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
	}, []string{"method"})

	// RedisLatency records Redis round-trip time.
	RedisLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "ratelimiter",
		Name:      "redis_latency_seconds",
		Help:      "Histogram of Redis command latencies.",
		Buckets:   []float64{0.00005, 0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
	}, []string{"command"})

	// RedisErrors counts Redis errors.
	RedisErrors = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "ratelimiter",
		Name:      "redis_errors_total",
		Help:      "Total Redis errors.",
	})

	// TokensRemaining provides a gauge snapshot per key prefix.
	TokensRemaining = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ratelimiter",
		Name:      "tokens_remaining",
		Help:      "Last observed remaining tokens (sampled).",
	}, []string{"key_prefix"})

	// ActiveConnections tracks active gRPC connections.
	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "ratelimiter",
		Name:      "active_connections",
		Help:      "Number of active gRPC connections.",
	})

	// ErrorRate tracks error responses (non-rate-limit errors).
	InternalErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "ratelimiter",
		Name:      "internal_errors_total",
		Help:      "Internal (non-rate-limit) errors.",
	}, []string{"method", "error_type"})
)

// Handler returns an HTTP handler for the /metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// KeyPrefix extracts a prefix from a rate limit key for metric labeling.
// e.g. "user:123" → "user", "ip:10.0.0.1" → "ip"
func KeyPrefix(key string) string {
	for i, c := range key {
		if c == ':' {
			return key[:i]
		}
	}
	return key
}