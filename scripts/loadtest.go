// Load test client for the rate limiter service.
// Usage: go run scripts/loadtest.go -addr localhost:50051 -rps 5000 -duration 30s -keys 100
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/SrushtiPatil01/rate-limiter/proto/ratelimitpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "gRPC server address")
	rps := flag.Int("rps", 5000, "target requests per second")
	dur := flag.Duration("duration", 30*time.Second, "test duration")
	numKeys := flag.Int("keys", 100, "number of unique keys")
	conns := flag.Int("conns", 10, "number of gRPC connections")
	workers := flag.Int("workers", 50, "number of concurrent workers")
	flag.Parse()

	log.Printf("Load Test Configuration:")
	log.Printf("  Target: %s", *addr)
	log.Printf("  RPS: %d, Duration: %s, Keys: %d", *rps, *dur, *numKeys)
	log.Printf("  Connections: %d, Workers: %d", *conns, *workers)

	// Create connection pool
	clients := make([]pb.RateLimitServiceClient, *conns)
	for i := 0; i < *conns; i++ {
		conn, err := grpc.Dial(*addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
		)
		if err != nil {
			log.Fatalf("failed to connect: %v", err)
		}
		defer conn.Close()
		clients[i] = pb.NewRateLimitServiceClient(conn)
	}

	// Stats
	var (
		totalReqs  atomic.Int64
		allowed    atomic.Int64
		denied     atomic.Int64
		errors     atomic.Int64
		latencies  sync.Map // stores []time.Duration per worker
	)

	// Rate control
	interval := time.Second / time.Duration(*rps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	// Request channel
	reqCh := make(chan struct{}, *rps*2)

	// Workers
	var wg sync.WaitGroup
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := clients[id%len(clients)]
			var lats []time.Duration

			for {
				select {
				case <-ctx.Done():
					latencies.Store(id, lats)
					return
				case <-reqCh:
					key := fmt.Sprintf("user:%d", rand.Intn(*numKeys))
					start := time.Now()

					callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
					resp, err := client.Allow(callCtx, &pb.AllowRequest{
						Key:    key,
						Tokens: 1,
					})
					callCancel()

					lat := time.Since(start)
					lats = append(lats, lat)
					totalReqs.Add(1)

					if err != nil {
						errors.Add(1)
					} else if resp.Allowed {
						allowed.Add(1)
					} else {
						denied.Add(1)
					}
				}
			}
		}(w)
	}

	// Ticker goroutine dispatches requests at target RPS
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case reqCh <- struct{}{}:
				default: // drop if workers can't keep up
				}
			}
		}
	}()

	// Progress reporter
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				log.Printf("[progress] total=%d allowed=%d denied=%d errors=%d",
					totalReqs.Load(), allowed.Load(), denied.Load(), errors.Load())
			}
		}
	}()

	wg.Wait()

	// Aggregate latencies
	var allLats []time.Duration
	latencies.Range(func(_, v interface{}) bool {
		allLats = append(allLats, v.([]time.Duration)...)
		return true
	})
	sort.Slice(allLats, func(i, j int) bool { return allLats[i] < allLats[j] })

	total := totalReqs.Load()
	actualDur := *dur

	fmt.Fprintf(os.Stdout, "\n")
	fmt.Fprintf(os.Stdout, "═══════════════════════════════════════════\n")
	fmt.Fprintf(os.Stdout, "  LOAD TEST RESULTS\n")
	fmt.Fprintf(os.Stdout, "═══════════════════════════════════════════\n")
	fmt.Fprintf(os.Stdout, "  Total Requests : %d\n", total)
	fmt.Fprintf(os.Stdout, "  Actual RPS     : %.0f\n", float64(total)/actualDur.Seconds())
	fmt.Fprintf(os.Stdout, "  Allowed        : %d (%.1f%%)\n", allowed.Load(), pct(allowed.Load(), total))
	fmt.Fprintf(os.Stdout, "  Denied         : %d (%.1f%%)\n", denied.Load(), pct(denied.Load(), total))
	fmt.Fprintf(os.Stdout, "  Errors         : %d (%.1f%%)\n", errors.Load(), pct(errors.Load(), total))
	fmt.Fprintf(os.Stdout, "───────────────────────────────────────────\n")

	if len(allLats) > 0 {
		fmt.Fprintf(os.Stdout, "  Latency (ms):\n")
		fmt.Fprintf(os.Stdout, "    p50  = %.2f\n", pN(allLats, 50).Seconds()*1000)
		fmt.Fprintf(os.Stdout, "    p90  = %.2f\n", pN(allLats, 90).Seconds()*1000)
		fmt.Fprintf(os.Stdout, "    p95  = %.2f\n", pN(allLats, 95).Seconds()*1000)
		fmt.Fprintf(os.Stdout, "    p99  = %.2f\n", pN(allLats, 99).Seconds()*1000)
		fmt.Fprintf(os.Stdout, "    max  = %.2f\n", allLats[len(allLats)-1].Seconds()*1000)
	}
	fmt.Fprintf(os.Stdout, "═══════════════════════════════════════════\n")
}

func pN(sorted []time.Duration, p float64) time.Duration {
	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

func pct(n, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}