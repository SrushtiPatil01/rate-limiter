package server

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/SrushtiPatil01/rate-limiter/pkg/limiter"
	"github.com/SrushtiPatil01/rate-limiter/pkg/metrics"
	pb "github.com/SrushtiPatil01/rate-limiter/proto/ratelimitpb"
)

// RateLimitServer implements the gRPC RateLimitService.
type RateLimitServer struct {
	pb.UnimplementedRateLimitServiceServer
	limiter *limiter.TokenBucket
}

// NewRateLimitServer creates a new server backed by the given limiter.
func NewRateLimitServer(l *limiter.TokenBucket) *RateLimitServer {
	return &RateLimitServer{limiter: l}
}

func (s *RateLimitServer) Allow(ctx context.Context, req *pb.AllowRequest) (*pb.AllowResponse, error) {
	start := time.Now()
	defer func() {
		metrics.RequestDuration.WithLabelValues("Allow").Observe(time.Since(start).Seconds())
	}()

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	res, err := s.limiter.Allow(ctx, req.Key, req.Tokens, req.Burst, req.Rate)
	if err != nil {
		metrics.InternalErrors.WithLabelValues("Allow", "redis").Inc()
		return nil, status.Errorf(codes.Internal, "rate limit check failed: %v", err)
	}

	prefix := metrics.KeyPrefix(req.Key)
	if res.Allowed {
		metrics.RequestsTotal.WithLabelValues(prefix, "allowed").Inc()
	} else {
		metrics.RequestsTotal.WithLabelValues(prefix, "denied").Inc()
	}
	metrics.TokensRemaining.WithLabelValues(prefix).Set(float64(res.Remaining))

	return &pb.AllowResponse{
		Allowed:    res.Allowed,
		Remaining:  res.Remaining,
		Limit:      res.Limit,
		ResetAt:    res.ResetAt,
		RetryAfter: res.RetryAfter,
	}, nil
}

func (s *RateLimitServer) Peek(ctx context.Context, req *pb.PeekRequest) (*pb.PeekResponse, error) {
	start := time.Now()
	defer func() {
		metrics.RequestDuration.WithLabelValues("Peek").Observe(time.Since(start).Seconds())
	}()

	if req.Key == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	res, err := s.limiter.Peek(ctx, req.Key, 0, 0)
	if err != nil {
		metrics.InternalErrors.WithLabelValues("Peek", "redis").Inc()
		return nil, status.Errorf(codes.Internal, "peek failed: %v", err)
	}

	return &pb.PeekResponse{
		Remaining: res.Remaining,
		Limit:     res.Limit,
		ResetAt:   res.ResetAt,
	}, nil
}

func (s *RateLimitServer) HealthCheck(ctx context.Context, _ *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	resp := &pb.HealthCheckResponse{Status: pb.HealthCheckResponse_SERVING}

	if err := s.limiter.Ping(ctx); err != nil {
		resp.Status = pb.HealthCheckResponse_NOT_SERVING
		resp.RedisStatus = err.Error()
		return resp, nil
	}

	resp.RedisStatus = "ok"
	return resp, nil
}