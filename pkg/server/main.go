package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	grpcprom "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/SrushtiPatil01/rate-limiter/pkg/config"
	"github.com/SrushtiPatil01/rate-limiter/pkg/limiter"
	"github.com/SrushtiPatil01/rate-limiter/pkg/metrics"
	"github.com/SrushtiPatil01/rate-limiter/pkg/server"
	pb "github.com/SrushtiPatil01/rate-limiter/proto/ratelimitpb"
)

func main() {
	cfg := config.Load()

	// ── Redis ────────────────────────────────────────────────
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		PoolSize:     cfg.RedisPoolSize,
		DialTimeout:  cfg.RedisDialTimeout,
		ReadTimeout:  cfg.RedisReadTimeout,
		WriteTimeout: cfg.RedisWriteTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("failed to connect to Redis at %s: %v", cfg.RedisAddr, err)
	}
	log.Printf("connected to Redis at %s", cfg.RedisAddr)

	// ── Limiter ──────────────────────────────────────────────
	tb := limiter.New(rdb, cfg.DefaultBurst, cfg.DefaultRate)

	// ── Prometheus metrics server ────────────────────────────
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := rdb.Ping(r.Context()).Err(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "redis: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	metricsSrv := &http.Server{
		Addr:    ":" + cfg.MetricsPort,
		Handler: mux,
	}
	go func() {
		log.Printf("metrics server listening on :%s", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("metrics server error: %v", err)
		}
	}()

	// ── gRPC server ──────────────────────────────────────────
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.MaxConcurrentStreams(uint32(cfg.MaxConcurrent)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              1 * time.Minute,
			Timeout:           20 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ChainUnaryInterceptor(
			grpcprom.UnaryServerInterceptor,
			unaryLogInterceptor,
		),
	)

	// Register gRPC Prometheus metrics
	grpcprom.Register(grpcServer)

	rlServer := server.NewRateLimitServer(tb)
	pb.RegisterRateLimitServiceServer(grpcServer, rlServer)
	reflection.Register(grpcServer) // for grpcurl/debugging

	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		log.Fatalf("failed to listen on :%s: %v", cfg.GRPCPort, err)
	}

	go func() {
		log.Printf("gRPC server listening on :%s", cfg.GRPCPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %v, shutting down...", sig)

	grpcServer.GracefulStop()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	metricsSrv.Shutdown(shutdownCtx)
	rdb.Close()

	log.Println("server stopped")
}

// unaryLogInterceptor logs slow requests (>50ms).
func unaryLogInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	dur := time.Since(start)
	if dur > 50*time.Millisecond {
		log.Printf("SLOW %s took %v", info.FullMethod, dur)
	}
	return resp, err
}