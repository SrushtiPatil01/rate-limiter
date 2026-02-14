package limiter

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/SrushtiPatil01/rate-limiter/pkg/metrics"
)

//go:embed ../../scripts/lua/token_bucket.lua
var tokenBucketScript string

// Result represents the outcome of a rate limit check.
type Result struct {
	Allowed    bool
	Remaining  int64
	Limit      int64
	ResetAt    int64
	RetryAfter float64
}

// TokenBucket implements a distributed token bucket backed by Redis.
type TokenBucket struct {
	rdb    *redis.Client
	script *redis.Script

	defaultBurst int64
	defaultRate  float64
}

// New creates a new TokenBucket limiter.
func New(rdb *redis.Client, defaultBurst int64, defaultRate float64) *TokenBucket {
	return &TokenBucket{
		rdb:          rdb,
		script:       redis.NewScript(tokenBucketScript),
		defaultBurst: defaultBurst,
		defaultRate:  defaultRate,
	}
}

// Allow checks whether a request identified by key should be permitted.
// burst and rate are optional overrides (pass 0 to use defaults).
func (tb *TokenBucket) Allow(ctx context.Context, key string, tokens int64, burst int64, rate float64) (*Result, error) {
	if tokens <= 0 {
		tokens = 1
	}
	if burst <= 0 {
		burst = tb.defaultBurst
	}
	if rate <= 0 {
		rate = tb.defaultRate
	}

	redisKey := fmt.Sprintf("rl:%s", key)
	now := float64(time.Now().UnixNano()) / 1e9 // high-precision timestamp

	start := time.Now()
	raw, err := tb.script.Run(ctx, tb.rdb, []string{redisKey},
		burst,
		rate,
		now,
		tokens,
	).Result()
	elapsed := time.Since(start).Seconds()

	metrics.RedisLatency.WithLabelValues("eval_token_bucket").Observe(elapsed)

	if err != nil {
		metrics.RedisErrors.Inc()
		return nil, fmt.Errorf("redis eval: %w", err)
	}

	vals, ok := raw.([]interface{})
	if !ok || len(vals) < 5 {
		return nil, fmt.Errorf("unexpected lua response: %v", raw)
	}

	allowed, _ := vals[0].(int64)
	remaining, _ := vals[1].(int64)
	limit, _ := vals[2].(int64)
	resetAt, _ := vals[3].(int64)

	var retryAfter float64
	switch v := vals[4].(type) {
	case string:
		retryAfter, _ = strconv.ParseFloat(v, 64)
	case int64:
		retryAfter = float64(v)
	}

	return &Result{
		Allowed:    allowed == 1,
		Remaining:  remaining,
		Limit:      limit,
		ResetAt:    resetAt,
		RetryAfter: retryAfter,
	}, nil
}

// Peek returns the current bucket state without consuming tokens.
func (tb *TokenBucket) Peek(ctx context.Context, key string, burst int64, rate float64) (*Result, error) {
	if burst <= 0 {
		burst = tb.defaultBurst
	}
	if rate <= 0 {
		rate = tb.defaultRate
	}

	// Use Allow with 0 tokens to peek without consuming
	return tb.Allow(ctx, key, 0, burst, rate)
}

// Ping checks Redis connectivity.
func (tb *TokenBucket) Ping(ctx context.Context) error {
	return tb.rdb.Ping(ctx).Err()
}