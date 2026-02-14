package limiter

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These are integration tests that require a running Redis instance.
// Run: REDIS_ADDR=localhost:6379 go test -v ./pkg/limiter/...

func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // use a test DB
	})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	t.Cleanup(func() {
		rdb.FlushDB(ctx)
		rdb.Close()
	})
	return rdb
}

func TestAllow_BasicFlow(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 5, 1.0) // burst=5, rate=1/s
	ctx := context.Background()

	// First 5 requests should be allowed
	for i := 0; i < 5; i++ {
		res, err := tb.Allow(ctx, "test:basic", 1, 0, 0)
		require.NoError(t, err)
		assert.True(t, res.Allowed, "request %d should be allowed", i)
		assert.Equal(t, int64(4-i), res.Remaining)
		assert.Equal(t, int64(5), res.Limit)
	}

	// 6th request should be denied
	res, err := tb.Allow(ctx, "test:basic", 1, 0, 0)
	require.NoError(t, err)
	assert.False(t, res.Allowed)
	assert.True(t, res.RetryAfter > 0)
}

func TestAllow_Refill(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 2, 10.0) // burst=2, rate=10/s (fast refill for test)
	ctx := context.Background()

	// Consume all tokens
	for i := 0; i < 2; i++ {
		res, err := tb.Allow(ctx, "test:refill", 1, 0, 0)
		require.NoError(t, err)
		assert.True(t, res.Allowed)
	}

	// Should be denied now
	res, err := tb.Allow(ctx, "test:refill", 1, 0, 0)
	require.NoError(t, err)
	assert.False(t, res.Allowed)

	// Wait for refill (need 1 token, rate=10/s → 100ms)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	res, err = tb.Allow(ctx, "test:refill", 1, 0, 0)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
}

func TestAllow_MultipleTokens(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 10, 1.0)
	ctx := context.Background()

	// Request 7 tokens at once
	res, err := tb.Allow(ctx, "test:multi", 7, 0, 0)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(3), res.Remaining)

	// Request 5 more - should be denied (only 3 remaining)
	res, err = tb.Allow(ctx, "test:multi", 5, 0, 0)
	require.NoError(t, err)
	assert.False(t, res.Allowed)
}

func TestAllow_PerRequestOverride(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 100, 10.0) // defaults
	ctx := context.Background()

	// Override to burst=2
	for i := 0; i < 2; i++ {
		res, err := tb.Allow(ctx, "test:override", 1, 2, 1.0)
		require.NoError(t, err)
		assert.True(t, res.Allowed)
	}

	res, err := tb.Allow(ctx, "test:override", 1, 2, 1.0)
	require.NoError(t, err)
	assert.False(t, res.Allowed)
}

func TestAllow_Concurrent(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 100, 0) // burst=100, rate=0 (no refill)
	ctx := context.Background()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
		denied  int
	)

	// Fire 200 concurrent requests for a bucket of 100
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res, err := tb.Allow(ctx, "test:concurrent", 1, 100, 0)
			if err != nil {
				t.Errorf("request %d error: %v", n, err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if res.Allowed {
				allowed++
			} else {
				denied++
			}
		}(i)
	}

	wg.Wait()

	// Exactly 100 should be allowed thanks to atomic Lua script
	assert.Equal(t, 100, allowed, "expected exactly 100 allowed")
	assert.Equal(t, 100, denied, "expected exactly 100 denied")
}

func TestAllow_IsolatedKeys(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 5, 1.0)
	ctx := context.Background()

	// Exhaust key A
	for i := 0; i < 5; i++ {
		tb.Allow(ctx, "test:keyA", 1, 0, 0)
	}

	// Key B should still have full quota
	res, err := tb.Allow(ctx, "test:keyB", 1, 0, 0)
	require.NoError(t, err)
	assert.True(t, res.Allowed)
	assert.Equal(t, int64(4), res.Remaining)
}

func TestPeek(t *testing.T) {
	rdb := testRedis(t)
	tb := New(rdb, 10, 1.0)
	ctx := context.Background()

	// Consume 3 tokens
	for i := 0; i < 3; i++ {
		tb.Allow(ctx, "test:peek", 1, 0, 0)
	}

	// Peek should show 7 remaining without consuming
	res, err := tb.Peek(ctx, "test:peek", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), res.Remaining)

	// Peek again - still 7
	res, err = tb.Peek(ctx, "test:peek", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(7), res.Remaining)
}

func BenchmarkAllow(b *testing.B) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 15})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		b.Skipf("Redis not available: %v", err)
	}
	defer rdb.FlushDB(ctx)
	defer rdb.Close()

	tb := New(rdb, 1000000, 1000000) // large bucket so we don't get denied
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("bench:%d", i%1000)
			tb.Allow(ctx, key, 1, 0, 0)
			i++
		}
	})
}